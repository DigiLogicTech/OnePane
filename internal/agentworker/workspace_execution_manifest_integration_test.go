//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestWorkspaceExecutionManifestListsOnlyTaskOwnedOCIApplications(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/tool-inventory.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Game",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 world,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:project.ID,Name:"World",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 story,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:project.ID,Name:"Story",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 worldRun,err:=svc.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{ProjectID:project.ID,ProjectWorkspaceID:&world.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 storyRun,err:=svc.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{ProjectID:project.ID,ProjectWorkspaceID:&story.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 image:="registry.example/tool@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
 appWorld,err:=svc.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{RuntimeID:worldRun.ID,Name:"Godot",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 appStory,err:=svc.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{RuntimeID:storyRun.ID,Name:"Private Story Tool",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 owned:=task.Task{WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&world.ID}
 raw,err:=workspaceExecutionManifest(ctx,db.SQL(),owned)
 if err!=nil{t.Fatal(err)}
 var manifest struct{
  ProjectWorkspaceID string `json:"project_workspace_id"`
  RuntimeID string `json:"runtime_id"`
  ResourceRef string `json:"resource_ref"`
  Applications []struct{ID string `json:"application_id"`} `json:"applications"`
  CommandTool struct{ID string `json:"tool_id"`;Version string `json:"tool_version"`} `json:"command_tool"`
  GitInspectTool struct{ID string `json:"tool_id"`;Version string `json:"tool_version"`;Actions []string `json:"actions"`} `json:"git_inspect_tool"`
  FileInspectTool struct{ID string `json:"tool_id"`;Version string `json:"tool_version"`;Actions []string `json:"actions"`} `json:"file_inspect_tool"`
  FileEditTool struct{ID string `json:"tool_id"`;Version string `json:"tool_version"`;Actions []string `json:"actions"`} `json:"file_edit_tool"`
  GitMutationTool struct{ID string `json:"tool_id"`;Version string `json:"tool_version"`;Actions []string `json:"actions"`} `json:"git_mutation_tool"`
 }
 if err:=json.Unmarshal(raw,&manifest);err!=nil{t.Fatal(err)}
 if manifest.ProjectWorkspaceID!=world.ID||manifest.RuntimeID!=worldRun.ID||
  manifest.ResourceRef!="project_runtime:"+worldRun.ID||
  manifest.CommandTool.ID!="project.app.exec"||manifest.CommandTool.Version!="1"||
  manifest.GitInspectTool.ID!="project.app.git.inspect"||manifest.GitInspectTool.Version!="1"||len(manifest.GitInspectTool.Actions)!=4||
  manifest.FileInspectTool.ID!="project.app.files.inspect"||manifest.FileInspectTool.Version!="1"||len(manifest.FileInspectTool.Actions)!=2||
  manifest.FileEditTool.ID!="project.app.files.edit"||manifest.FileEditTool.Version!="1"||len(manifest.FileEditTool.Actions)!=3||
  manifest.GitMutationTool.ID!="project.app.git.mutate"||manifest.GitMutationTool.Version!="1"||len(manifest.GitMutationTool.Actions)!=3||
  len(manifest.Applications)!=1||manifest.Applications[0].ID!=appWorld.ID {
  t.Fatalf("incorrect Task-owned Workspace execution manifest: %s",raw)
 }
 if strings.Contains(string(raw),storyRun.ID)||strings.Contains(string(raw),appStory.ID)||
  strings.Contains(string(raw),"environment_bindings")||strings.Contains(string(raw),"secret_ref"){
  t.Fatalf("foreign Workspace identity or secret scope leaked: %s",raw)
 }
 absent:=task.Task{WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:ptr("not-a-workspace")}
 missing,err:=workspaceExecutionManifest(ctx,db.SQL(),absent)
 if err!=nil||!strings.Contains(string(missing),"not_provisioned")||
  strings.Contains(string(missing),worldRun.ID){
  t.Fatalf("invalid Workspace must never receive another Workspace inventory: %s %v",missing,err)
 }
 legacy:=task.Task{WorkspaceID:"tenant",ProjectID:&project.ID}
 noManifest,err:=workspaceExecutionManifest(ctx,db.SQL(),legacy)
 if err!=nil||noManifest!=nil{
  t.Fatalf("legacy Project Tasks must not implicitly inherit named Workspace runtimes: %s %v",noManifest,err)
 }
 // One Workspace belonging to another tenant cannot be enumerated simply by
 // knowing a Project and Workspace ID.
 forged:=task.Task{WorkspaceID:"other",ProjectID:&project.ID,ProjectWorkspaceID:&world.ID}
 foreign,err:=workspaceExecutionManifest(ctx,db.SQL(),forged)
 if err!=nil||strings.Contains(string(foreign),appWorld.ID){
  t.Fatalf("cross-tenant manifest leaked: %s %v",foreign,err)
 }
}
