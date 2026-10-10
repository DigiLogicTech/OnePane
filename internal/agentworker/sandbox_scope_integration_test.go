//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestAgentWorkerSandboxToolOwnershipIsBoundToTaskWorkspace(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/sandbox-scope.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 seed:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }
 for _,q:=range seed{if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}}
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
 oldRun,err:=svc.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{ProjectID:project.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 image:="registry.example/devtool@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
 app,err:=svc.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{RuntimeID:worldRun.ID,Name:"Godot",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 storyApp,err:=svc.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{RuntimeID:storyRun.ID,Name:"StoryTool",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 taskWorld:=task.Task{ID:"task-world",WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&world.ID,Completion:json.RawMessage(`{"type":"operator_review"}`)}
 ref:="project_runtime:"+worldRun.ID
 execInput:=json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","command":["sh","-c","printf world"]}`)
 imageInput:=json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","image":"`+image+`"}`)
 runtimeInput:=json.RawMessage(`{"runtime_id":"`+worldRun.ID+`"}`)
 // Human-approved software cannot be silently bypassed by an Agent:
 // the Tool proposal must use the exact registered Workspace application
 // and carry ALL operator-declared executables to live OCI preflight.
 unchanged,err:=applyApprovedWorkspaceToolchain(ctx,db.SQL(),taskWorld,execInput)
 if err!=nil||string(unchanged)!=string(execInput){t.Fatalf("legacy Workspace build altered: %v",err)}
 approved,err:=svc.ApproveWorkspaceToolchainManifest(ctx,projectworkspace.ApproveToolchainManifestCommand{
  ProjectID:project.ID,ProjectWorkspaceID:world.ID,ApplicationID:app.ID,
  ActorPrincipalID:"operator",Requirements:[]projectworkspace.ToolchainRequirement{
   {Executable:"python3",VersionConstraint:">=3.12"},
   {Executable:"go",VersionConstraint:">=1.23"},
  },
 })
 if err!=nil{t.Fatal(err)}
 enriched,err:=applyApprovedWorkspaceToolchain(ctx,db.SQL(),taskWorld,execInput)
 if err!=nil{t.Fatalf("approved Workspace task denied before live OCI check: %v",err)}
 var execution struct{Required []string `json:"required_executables"`;Application string `json:"application_id"`}
 if err=json.Unmarshal(enriched,&execution);err!=nil||
  len(execution.Required)!=2||execution.Required[0]!="go"||
  execution.Required[1]!="python3"||execution.Application!=app.ID{
  t.Fatalf("approved requirements not injected into Agent Tool call: %+v %v",execution,err)
 }
 pinnedTask:=taskWorld
 pinnedTask.Completion=json.RawMessage(`{"toolchain_manifest_sha256":"`+approved.ManifestSHA256+`"}`)
 if _,err=applyApprovedWorkspaceToolchain(ctx,db.SQL(),pinnedTask,execInput);err!=nil{t.Fatal(err)}
 for _,bad:=range []string{
  `{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","command":["go","version"],"required_executables":["go"]}`,
  `{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","command":["go","version"],"required_executables":[]}`,
  `{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","command":["go","version"]}`,
 }{
  if _,err=applyApprovedWorkspaceToolchain(ctx,db.SQL(),taskWorld,json.RawMessage(bad));err==nil{
   t.Fatalf("Agent bypassed or substituted approved toolchain: %s",bad)
  }
 }
 wrongDigest:=taskWorld
 wrongDigest.Completion=json.RawMessage(`{"toolchain_manifest_sha256":"`+strings.Repeat("0",64)+`"}`)
 if _,err=applyApprovedWorkspaceToolchain(ctx,db.SQL(),wrongDigest,execInput);err==nil{
  t.Fatal("Orchestrator Task reused a changed toolchain approval")
 }
 otherWorkspace:=taskWorld
 otherWorkspace.ProjectWorkspaceID=&story.ID
 if _,err=applyApprovedWorkspaceToolchain(ctx,db.SQL(),otherWorkspace,execInput);err!=nil{
  t.Fatalf("unconfigured neighbour should not inherit approval: %v",err)
 }
 for _,tc:=range []struct{name,tool,ref string;input json.RawMessage}{
  {"exec",sandboxrunner.ToolAppExec,ref,execInput},
  {"git_mutate",sandboxrunner.ToolAppGitMutate,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","action":"init"}`)},
  {"file_publish",sandboxrunner.ToolAppFilePublish,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","action":"publish","path":"world.bin"}`)},
  {"file_edit",sandboxrunner.ToolAppFileEdit,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","action":"create","path":"story.txt","content_base64":"dGVzdA=="}`)},
  {"file_inspect",sandboxrunner.ToolAppFileInspect,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","action":"list"}`)},
  {"git_inspect",sandboxrunner.ToolAppGitInspect,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","action":"status"}`)},
  {"pull",sandboxrunner.ToolAppPull,ref,imageInput},
  {"image_inspect",sandboxrunner.ToolImageInspect,ref,imageInput},
  {"runtime_inspect",sandboxrunner.ToolRuntimeInspect,ref,runtimeInput},
  {"runtime_ensure",sandboxrunner.ToolRuntimeEnsure,ref,runtimeInput},
 }{
  t.Run("allowed_"+tc.name,func(t *testing.T){
   if err:=enforceSandboxToolOwnership(ctx,db.SQL(),taskWorld,tc.tool,tc.ref,tc.input);err!=nil{
    t.Fatalf("valid tool denied: %v",err)
   }
  })
 }
 var denied=[]struct{name string;task task.Task;tool,ref string;input json.RawMessage}{
  {"foreign_workspace_runtime",taskWorld,sandboxrunner.ToolRuntimeInspect,"project_runtime:"+storyRun.ID,
   json.RawMessage(`{"runtime_id":"`+storyRun.ID+`"}`)},
  {"foreign_git_write",taskWorld,sandboxrunner.ToolAppGitMutate,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","action":"stage_all"}`)},
  {"foreign_file_publish",taskWorld,sandboxrunner.ToolAppFilePublish,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","action":"publish","path":"story.bin"}`)},
  {"foreign_file_edit",taskWorld,sandboxrunner.ToolAppFileEdit,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","action":"create","path":"story.txt","content_base64":"dGVzdA=="}`)},
  {"foreign_file_app",taskWorld,sandboxrunner.ToolAppFileInspect,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","action":"list"}`)},
  {"foreign_git_app",taskWorld,sandboxrunner.ToolAppGitInspect,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`","action":"status"}`)},
  {"foreign_workspace_app",taskWorld,sandboxrunner.ToolAppExec,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+storyApp.ID+`"}`)},
  {"resource_input_mismatch",taskWorld,sandboxrunner.ToolAppExec,"project_runtime:"+storyRun.ID,execInput},
  {"image_substitution",taskWorld,sandboxrunner.ToolAppPull,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`","application_id":"`+app.ID+`","image":"rogue/app:latest"}`)},
  {"missing_image",taskWorld,sandboxrunner.ToolImageInspect,ref,execInput},
  {"missing_app",taskWorld,sandboxrunner.ToolAppExec,ref,
   json.RawMessage(`{"runtime_id":"`+worldRun.ID+`"}`)},
  {"other_project",task.Task{WorkspaceID:"tenant",ProjectID:ptr("not-this-project"),ProjectWorkspaceID:&world.ID},sandboxrunner.ToolAppExec,ref,execInput},
  {"unbound_project_task",task.Task{WorkspaceID:"tenant",ProjectID:&project.ID},sandboxrunner.ToolAppExec,ref,execInput},
  {"no_project",task.Task{WorkspaceID:"tenant"},sandboxrunner.ToolAppExec,ref,execInput},
  {"legacy_runtime_from_scoped_task",taskWorld,sandboxrunner.ToolRuntimeInspect,
   "project_runtime:"+oldRun.ID,json.RawMessage(`{"runtime_id":"`+oldRun.ID+`"}`)},
  {"spoof_routing",task.Task{WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&world.ID,
    Completion:json.RawMessage(`{"onepane_routing":{"project_workspace_id":"`+story.ID+`"}}`)},
   sandboxrunner.ToolAppExec,ref,execInput},
 }
 for _,tc:=range denied{
  t.Run("denied_"+tc.name,func(t *testing.T){
   if err:=enforceSandboxToolOwnership(ctx,db.SQL(),tc.task,tc.tool,tc.ref,tc.input);err==nil{
    t.Fatal("cross-Workspace or mismatched sandbox tool accepted")
   }
  })
 }
 // Backward compatibility: old Project Tasks may only run their own
 // explicitly legacy shared Project runtime, never a new Workspace runtime.
 legacyTask:=task.Task{WorkspaceID:"tenant",ProjectID:&project.ID}
 if err:=enforceSandboxToolOwnership(ctx,db.SQL(),legacyTask,sandboxrunner.ToolRuntimeInspect,
  "project_runtime:"+oldRun.ID,json.RawMessage(`{"runtime_id":"`+oldRun.ID+`"}`));err!=nil{
  t.Fatalf("Project legacy runtime access regression: %v",err)
 }
 if err:=enforceSandboxToolOwnership(ctx,db.SQL(),taskWorld,"files.read","file:///workspace/notes",json.RawMessage(`{}`));err!=nil{
  t.Fatalf("unrelated tool policy must remain separate: %v",err)
 }
}
func ptr(s string)*string{return &s}
