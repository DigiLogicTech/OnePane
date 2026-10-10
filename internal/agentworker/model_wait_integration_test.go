package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestNamedWorkspaceModelWaitSurvivesWorkerRestartWithoutDuplicateAttempt(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"model_wait.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"World Project",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 w,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 taskService:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=New(db.SQL(),db,clock.Real{},"local",taskService,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 created,err:=taskService.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&w.ID,
  Objective:"Build world on local CPU when model available",
  Completion:json.RawMessage(`{"type":"operator_review"}`),
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=taskService.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,started:=svc.startRun(ctx,ready)
 if started.Error!=""{t.Fatal(started.Error)}
 stale:=run
 stale.Revision++
 rejected:=svc.waitForLocalModel(ctx,stale,
  TickResult{TaskID:created.ID,RunID:run.ID},"awaiting eligible local model")
 if rejected.Status=="waiting_model"||rejected.Error==""{
  t.Fatalf("stale Worker incarnation suspended Task: %+v",rejected)
 }
 unchangedTask,err:=taskService.Get(ctx,created.ID)
 if err!=nil||unchangedTask.State!=task.StateRunning{
  t.Fatalf("stale Worker transitioned Task despite failed CAS: %+v %v",unchangedTask,err)
 }
 unchangedRun,err:=svc.getRun(ctx,run.ID)
 if err!=nil||unchangedRun.Status!=RunRunning||unchangedRun.Revision!=run.Revision{
  t.Fatalf("stale Worker changed persisted run: %+v %v",unchangedRun,err)
 }

 waiting:=svc.waitForLocalModel(ctx,run,TickResult{TaskID:created.ID,RunID:run.ID},
  "awaiting eligible local model")
 if waiting.Status!="waiting_model"||waiting.Error==""{
  t.Fatalf("no durable model wait: %+v",waiting)
 }
 suspended,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if suspended.State!=task.StateWaitingDependency{
  t.Fatalf("Task should wait, not fail: %s",suspended.State)
 }
 workerState,err:=svc.getRun(ctx,run.ID)
 if err!=nil{t.Fatal(err)}
 if workerState.Status!=RunWaiting||decodeModelWait(workerState.Continuation)==nil{
  t.Fatalf("wait continuation not durable: %+v",workerState)
 }
 // Corrupt persisted model waits must fail closed. JSON null previously
 // decoded as no resource requirement, waking a Task without checking
 // local model availability. No Worker or Attempt may be rescheduled.
 for _,malformed:=range []string{
  `{"model_wait":null}`,
  `{"model_wait":"invalid"}`,
  `{"model_wait":{"attempt":0,"retry_at_ms":1}}`,
  `{"model_wait":{"attempt":1,"retry_at_ms":0}}`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,
   `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,
   malformed,run.ID);err!=nil{t.Fatal(err)}
  if err:=svc.syncResumedRuns(ctx);err==nil{
   t.Fatalf("invalid model wait unexpectedly resumed: %s",malformed)
  }
  held,err:=taskService.Get(ctx,created.ID)
  if err!=nil||held.State!=task.StateWaitingDependency{
   t.Fatalf("corrupt model wait advanced Task: %+v %v",held,err)
  }
  var attemptState string
  if err:=db.SQL().QueryRowContext(ctx,
   `SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).
   Scan(&attemptState);err!=nil||attemptState!="waiting"{
   t.Fatalf("corrupt model wait advanced Attempt: %q %v",attemptState,err)
  }
  heldRun,err:=svc.getRun(ctx,run.ID)
  if err!=nil||heldRun.Status!=RunWaiting{
   t.Fatalf("corrupt model wait advanced Worker: %+v %v",heldRun,err)
  }
 }
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,
  string(workerState.Continuation),run.ID);err!=nil{t.Fatal(err)}
 if hasModelWaitField(json.RawMessage(`{}`)){
  t.Fatal("unrelated dependency continuation treated as model wait")
 }
 // A new service instance simulates an ordinary worker/service restart.
 restarted:=New(db.SQL(),db,clock.Real{},"local",taskService,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 stillWaiting,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if stillWaiting.State!=task.StateWaitingDependency{
  t.Fatalf("worker restarted Task before retry window: %s",stillWaiting.State)
 }
 // Advance the stored retry time without sleeping or mutating user Task data.
 persisted:=modelWaitEnvelope{ModelWait:&modelWaitRecord{
  Attempt:1,RetryAtMS:now-1000,Reason:"waiting",
 }}
 raw,err:=json.Marshal(persisted)
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,string(raw),run.ID);err!=nil{
  t.Fatal(err)
 }
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 resumed,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if resumed.State!=task.StateRunning{
  t.Fatalf("due local-model wait not resumed: %s",resumed.State)
 }
 resumedRun,err:=restarted.getRun(ctx,run.ID)
 if err!=nil||resumedRun.Status!=RunRunning{
  t.Fatalf("existing worker run not resumed: %+v %v",resumedRun,err)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,created.ID).Scan(&attempts);err!=nil{
  t.Fatal(err)
 }
 if attempts!=1{t.Fatalf("retry created duplicate Task Attempt: %d",attempts)}
 var snapshots int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs WHERE task_id=? AND status='running'`,created.ID).Scan(&snapshots);err!=nil&&err!=sql.ErrNoRows{
  t.Fatal(err)
 }
 if snapshots!=1{t.Fatalf("worker retry duplicated active Run: %d",snapshots)}
}


func TestWorkerWakeIsAtomicAcrossAttemptTaskAndRun(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"resume_atomic.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces
 (id,name,status,revision,created_at,updated_at)
 VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 ts:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",ts,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 created,err:=ts.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"Retry using the original Attempt",
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=ts.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,result:=worker.startRun(ctx,ready)
 if result.Error!=""{t.Fatal(result.Error)}
 if res:=worker.waitForLocalModel(ctx,run,
  TickResult{TaskID:created.ID,RunID:run.ID},"local model unavailable");
  res.Status!="waiting_model"{t.Fatalf("failed to suspend: %+v",res)}
 persisted,err:=worker.getRun(ctx,run.ID)
 if err!=nil{t.Fatal(err)}
 state:=decodeModelWait(persisted.Continuation)
 if state==nil{t.Fatal("missing original model wait")}
 state.RetryAtMS=now-1000
 due,_:=json.Marshal(modelWaitEnvelope{ModelWait:state})
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,
  string(due),run.ID);err!=nil{t.Fatal(err)}
 // Force failure AFTER the Worker CAS and Attempt wake but before Task
 // commit. All writes, including the event outbox, must be rolled back.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_worker_resume
  BEFORE UPDATE ON tasks
  WHEN OLD.state='waiting_dependency' AND NEW.state='running'
  BEGIN SELECT RAISE(ABORT,'injected Task resume failure'); END`);err!=nil{t.Fatal(err)}
 if err:=worker.syncResumedRuns(ctx);err==nil{
  t.Fatal("fault-injected Task resume silently succeeded")
 }
 heldTask,err:=ts.Get(ctx,created.ID)
 if err!=nil||heldTask.State!=task.StateWaitingDependency{
  t.Fatalf("Task escaped atomic rollback: %+v %v",heldTask,err)
 }
 heldRun,err:=worker.getRun(ctx,run.ID)
 if err!=nil||heldRun.Status!=RunWaiting{
  t.Fatalf("Worker escaped atomic rollback: %+v %v",heldRun,err)
 }
 var attemptState string
 if err:=db.SQL().QueryRowContext(ctx,
  `SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).
  Scan(&attemptState);err!=nil||attemptState!="waiting"{
  t.Fatalf("Attempt escaped atomic rollback: %q %v",attemptState,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
  `DROP TRIGGER reject_worker_resume`);err!=nil{t.Fatal(err)}
 if err:=worker.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 resumedTask,err:=ts.Get(ctx,created.ID)
 if err!=nil||resumedTask.State!=task.StateRunning{
  t.Fatalf("Task not resumed: %+v %v",resumedTask,err)
 }
 resumedRun,err:=worker.getRun(ctx,run.ID)
 if err!=nil||resumedRun.Status!=RunRunning{
  t.Fatalf("Worker not resumed: %+v %v",resumedRun,err)
 }
 if err:=db.SQL().QueryRowContext(ctx,
  `SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).
  Scan(&attemptState);err!=nil||attemptState!="running"{
  t.Fatalf("Attempt not resumed: %q %v",attemptState,err)
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,
  `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,created.ID).Scan(&count);err!=nil{
  t.Fatal(err)
 }
 if count!=1{t.Fatalf("Worker wake created extra Attempts: %d",count)}

 // An operator may resume a Task independently; the Worker must not
 // reactivate if its original Attempt is no longer actually running.
 again:=worker.waitForLocalModel(ctx,resumedRun,
  TickResult{TaskID:created.ID,RunID:run.ID},"model drained")
 if again.Status!="waiting_model"{t.Fatalf("second wait failed: %+v",again)}
 heldTask,err=ts.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if _,err:=ts.Resume(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:heldTask.Revision,
 });err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts
   SET status='waiting' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
 if err:=worker.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 heldRun,err=worker.getRun(ctx,run.ID)
 if err!=nil||heldRun.Status!=RunWaiting{
  t.Fatalf("Worker resumed without original running Attempt: %+v %v",heldRun,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts
   SET status='running' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
 if err:=worker.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 resumedRun,err=worker.getRun(ctx,run.ID)
 if err!=nil||resumedRun.Status!=RunRunning{
  t.Fatalf("operator-resumed original Attempt did not reconcile: %+v %v",resumedRun,err)
 }
}
