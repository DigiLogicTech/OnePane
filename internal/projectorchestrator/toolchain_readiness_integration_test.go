//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

type unavailableOrchestratorReasoning struct{}
func (unavailableOrchestratorReasoning) Route(context.Context,scheduler.RouteRequest)(scheduler.Decision,error){
 return scheduler.Decision{},errors.New("no registered reasoning model")
}

func TestProjectOrchestratorUsesCanonicalWorkspaceToolchainReadinessAndPinnedTasks(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/orchestrator-toolchain.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"General Software",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 build,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Build",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 qa,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"QA",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&build.ID,CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 image:="registry.example/go-toolchain@sha256:"+strings.Repeat("a",64)
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Compiler",SourceKind:projectworkspace.AppOCIImage,
  SourceRef:image,CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 noApproval,err:=inspectWorkspaceToolchain(ctx,db.SQL(),p.ID,build.ID)
 if err!=nil||noApproval.Status!="not_approved"||noApproval.ManifestSHA256!=""{
  t.Fatalf("absent approval incorrectly qualified: %+v %v",noApproval,err)
 }
 approved,err:=projects.ApproveWorkspaceToolchainManifest(ctx,projectworkspace.ApproveToolchainManifestCommand{
  ProjectID:p.ID,ProjectWorkspaceID:build.ID,ApplicationID:app.ID,ActorPrincipalID:"human",
  Requirements:[]projectworkspace.ToolchainRequirement{{Executable:"go",VersionConstraint:">=1.23"}},
 })
 if err!=nil{t.Fatal(err)}
 waiting,err:=inspectWorkspaceToolchain(ctx,db.SQL(),p.ID,build.ID)
 if err!=nil||waiting.Status!="waiting_resources"||waiting.ManifestSHA256!=approved.ManifestSHA256||
  waiting.ApplicationID!=app.ID||waiting.RuntimeID!=runtime.ID||
  waiting.RuntimeStatus=="running"{
  t.Fatalf("stopped OCI falsely declared ready: %+v %v",waiting,err)
 }
 missingQA,err:=inspectWorkspaceToolchain(ctx,db.SQL(),p.ID,qa.ID)
 if err!=nil||missingQA.Status!="not_approved"||missingQA.ManifestSHA256!=""{
  t.Fatalf("QA inherited Build approval: %+v %v",missingQA,err)
 }
 if _,err:=inspectWorkspaceToolchain(ctx,db.SQL(),"other-project",build.ID);err==nil{
  t.Fatal("foreign Project was able to inspect private Workspace")
 }
 orch:=NewService(db.SQL(),db,clock.Real{},unavailableOrchestratorReasoning{},nil,nil,
  task.NewService(db.SQL(),db,clock.Real{}),nil)
 snapshot,selected,_,_,err:=orch.contextSnapshot(ctx,p.ID,build.ID)
 if err!=nil||selected!=build.ID{t.Fatalf("orchestrator context failed: %v",err)}
 var doc struct{
  Readiness workspaceToolchainReadiness `json:"toolchain_readiness"`
 }
 if err=json.Unmarshal(snapshot,&doc);err!=nil||doc.Readiness.Status!="waiting_resources"||
  doc.Readiness.ManifestSHA256!=approved.ManifestSHA256{
  t.Fatalf("orchestrator hid unavailable toolchain: %s %v",snapshot,err)
 }
 // Even if the LLM backend is unavailable, an explicitly user-forced Task
 // must retain the canonical workspace FK and its exact manifest digest.
 forced,err:=orch.Turn(ctx,TurnCommand{
  ProjectID:p.ID,ProjectWorkspaceID:build.ID,ActorPrincipalID:"human",
  Objective:"Compile this program",ForceTask:true,
 })
 if err!=nil||forced.TaskID==nil{t.Fatalf("forced Task failed: %+v %v",forced,err)}
 persisted,err:=task.NewService(db.SQL(),db,clock.Real{}).Get(ctx,*forced.TaskID)
 if err!=nil||persisted.ProjectWorkspaceID==nil||*persisted.ProjectWorkspaceID!=build.ID{
  t.Fatalf("Project Orchestrator Task escaped Workspace ownership: %+v %v",persisted,err)
 }
 var metadata struct{Digest string `json:"toolchain_manifest_sha256"`}
 if err=json.Unmarshal(persisted.Completion,&metadata);err!=nil||
  metadata.Digest!=approved.ManifestSHA256{
  t.Fatalf("Task failed to pin approved software: %s %v",persisted.Completion,err)
 }
 if !strings.Contains(forced.Turn.Content,"waiting_resources"){
  t.Fatalf("Orchestrator did not report resource wait in Task turn: %s",forced.Turn.Content)
 }
 // Reconciliation may move the approved app/runtime to running. This is
 // only an OCI reported-state preflight admission, not a version test.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_runtimes
 SET desired_state='running',status='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_applications
 SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 ready,err:=inspectWorkspaceToolchain(ctx,db.SQL(),p.ID,build.ID)
 if err!=nil||ready.Status!="ready_for_preflight"||len(ready.Requirements)!=1{
  t.Fatalf("approved running OCI not eligible for live prereq check: %+v %v",ready,err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_applications
 SET revision=revision+1 WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 stale,err:=inspectWorkspaceToolchain(ctx,db.SQL(),p.ID,build.ID)
 if err!=nil||stale.Status!="waiting_approval"{
  t.Fatalf("changed OCI silently adopted as approved: %+v %v",stale,err)
 }
}
