//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestTaskGraphSnapshotMarksOrphanAttemptAsOperatorReview(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/attempt-health.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Operator','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('system:agent-worker','system','Agent Worker','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
 }
 clk:=clock.Real{}
 projects:=projectworkspace.NewService(db.SQL(),db,clk)
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Crash recovery",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Isolated build",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clk)
 svc:=NewService(db.SQL(),db,clk,nil,nil,nil,tasks,nil)
 graph,err:=svc.CreateTaskGraph(ctx,CreateTaskGraphCommand{
  ProjectID:project.ID,Name:"Recovery audit",
  ActorPrincipalID:"human",IdempotencyKey:"recover-inspect-v1",
  Nodes:[]TaskGraphNodeSpec{{
   Key:"build",ProjectWorkspaceID:workspace.ID,Objective:"Build within sandbox",
  }},
 })
 if err!=nil{t.Fatal(err)}
 if graph.Nodes[0].AttemptHealth!="none"{t.Fatalf("Task not yet admitted: %+v",graph.Nodes[0])}
 taskID:=graph.Nodes[0].TaskID
 created,err:=tasks.Get(ctx,taskID)
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:taskID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 agent:="system:agent-worker"
 _,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:taskID,ExpectedRevision:ready.Revision,
  WorkerPrincipalID:&agent,Metadata:json.RawMessage(`{"agent_worker":"v1"}`),
 })
 if err!=nil{t.Fatal(err)}
 orphan,err:=svc.TaskGraph(ctx,project.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 n:=orphan.Nodes[0]
 if n.AttemptHealth!="orphaned_agent_attempt"||
  n.Readiness!="needs_attention"||orphan.Progress.NeedsAttention!=1{
  t.Fatalf("orphaned attempt incorrectly admitted: %+v",n)
 }
 snapshot,err:=svc.TaskGraphRecoveryCheckpoint(ctx,project.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 if snapshot.AutomaticRetryAllowed||!snapshot.NeedsOperatorReview||
  snapshot.Nodes[0].RecoveryClass!="operator_review"||
  snapshot.Nodes[0].AttemptHealth!="orphaned_agent_attempt"{
  t.Fatalf("invalid recovery checkpoint: %+v",snapshot)
 }
 // Mutating only the active Attempt state changes the checkpoint
 // fingerprint while Task revision and DAG manifest stay unchanged.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE task_attempts
  SET status='interrupted' WHERE id=?`,attempt.ID);err!=nil{t.Fatal(err)}
 changed,err:=svc.TaskGraphRecoveryCheckpoint(ctx,project.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 if changed.Nodes[0].AttemptHealth!="missing_active_attempt"||
  changed.ObservationSHA256==snapshot.ObservationSHA256{
  t.Fatalf("stale checkpoint survived Attempt state change: %+v",changed)
 }
}
