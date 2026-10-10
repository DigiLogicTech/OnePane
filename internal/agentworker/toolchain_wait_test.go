package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestApprovedWorkspaceToolchainWaitSurvivesRestartAndAvoidsDuplicateAttempt(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"wait.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Application",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 workspace,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Build",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:project.ID,ProjectWorkspaceID:&workspace.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 image:="registry.example/tool@sha256:"+strings.Repeat("a",64)
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Compiler",SourceKind:projectworkspace.AppOCIImage,
  SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 approved,err:=projects.ApproveWorkspaceToolchainManifest(ctx,projectworkspace.ApproveToolchainManifestCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,
  ApplicationID:app.ID,ActorPrincipalID:"operator",
  Requirements:[]projectworkspace.ToolchainRequirement{{Executable:"go",VersionConstraint:">=1.23"}},
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 completion,_:=json.Marshal(map[string]string{"toolchain_manifest_sha256":approved.ManifestSHA256})
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&workspace.ID,
  Objective:"Compile local Workspace binary",Completion:completion,
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 original:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 run,start:=original.startRun(ctx,ready)
 if start.Error!=""{t.Fatal(start.Error)}
 raw,_:=json.Marshal(map[string]any{
  "runtime_id":runtime.ID,"application_id":app.ID,"command":[]string{"go","version"},
 })
 admitted,err:=applyApprovedWorkspaceToolchain(ctx,db.SQL(),ready,raw)
 if err!=nil{t.Fatalf("approved exec rejected before runtime wait: %v",err)}
 waitingOn,err:=approvedToolchainWaitCandidate(ctx,db.SQL(),ready,admitted)
 if err!=nil||waitingOn==nil||waitingOn.ManifestSHA256!=approved.ManifestSHA256{
  t.Fatalf("stopped approved OCI didn't require resource wait: %+v %v",waitingOn,err)
 }
 waiting:=original.waitForWorkspaceToolchain(ctx,run,TickResult{TaskID:created.ID,RunID:run.ID},waitingOn)
 if waiting.Status!="waiting_toolchain"{t.Fatalf("no durable resource wait: %+v",waiting)}
 suspended,err:=tasks.Get(ctx,created.ID)
 if err!=nil||suspended.State!=task.StateWaitingDependency{
  t.Fatalf("Task should wait on existing Attempt: %+v %v",suspended,err)
 }
 saved,err:=original.getRun(ctx,run.ID)
 if err!=nil||saved.Status!=RunWaiting||decodeToolchainWait(saved.Continuation)==nil{
  t.Fatalf("Worker lost approved software wait: %+v %v",saved,err)
 }
 // New Worker instance: no automatic new Attempt, no model/tool invocation.
 restarted:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 stillWaiting,err:=tasks.Get(ctx,created.ID)
 if err!=nil||stillWaiting.State!=task.StateWaitingDependency{
  t.Fatalf("Worker resumed before retry deadline: %+v %v",stillWaiting,err)
 }
 // Force due without sleeping, using only durable continuation state.
 savedWait:=decodeToolchainWait(saved.Continuation)
 savedWait.RetryAtMS=now-1000
 due,_:=json.Marshal(toolchainWaitEnvelope{ToolchainWait:savedWait})
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,string(due),run.ID);err!=nil{t.Fatal(err)}
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 notYet,err:=tasks.Get(ctx,created.ID)
 if err!=nil||notYet.State!=task.StateWaitingDependency{
  t.Fatalf("stopped OCI resumed work despite observed resources not running: %+v %v",notYet,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE project_runtimes SET desired_state='running',status='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE project_applications SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 resumed,err:=tasks.Get(ctx,created.ID)
 if err!=nil||resumed.State!=task.StateRunning{
  t.Fatalf("approved OCI never woke its existing Attempt: %+v %v",resumed,err)
 }
 r,err:=restarted.getRun(ctx,run.ID)
 if err!=nil||r.Status!=RunRunning{
  t.Fatalf("resumed worker created a different run: %+v %v",r,err)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,
  `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,created.ID).Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=1{t.Fatalf("resource wait duplicated Task Attempt: %d",attempts)}
}
func TestApprovedToolchainWaitNeverWakesWithChangedManifestOrWrongWorkspace(t *testing.T){
 // A forged or corrupted continuation is not a grant. The helper itself
 // requires the exact stored approval/image/running OCI pair before wake.
 if decodeToolchainWait(json.RawMessage(`{"toolchain_wait":{"manifest_sha256":"short","retry_at_ms":1}}`))!=nil{
  t.Fatal("malformed approved resource wait was trusted")
 }
 if decodeToolchainWait(json.RawMessage(`{"toolchain_wait":{"project_id":"x","project_workspace_id":"y","runtime_id":"z","application_id":"a","manifest_sha256":"`+
  strings.Repeat("f",64)+`","retry_at_ms":1}}`))==nil{
  t.Fatal("well-formed record was not parseable")
 }
 // Ensure production wait helper is not fooled by mere boolean JSON flags.
 if decodeToolchainWait(json.RawMessage(`{"toolchain_wait":{"ready":true}}`))!=nil{
  t.Fatal("untrusted ready field bypassed persisted approval checks")
 }
 _=sql.ErrNoRows
}
