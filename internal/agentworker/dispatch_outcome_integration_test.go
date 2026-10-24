//go:build integration

package agentworker

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func prepareUnknownDispatch(t *testing.T)(*Service,context.Context,Run,func()){
 t.Helper()
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 db:=migratedUnknownDispatchDatabase(t,ctx)
 cleanup:=func(){_ = db.Close();cancel()}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{cleanup();t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{cleanup();t.Fatal(err)}
 created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:"Do not retry unknown external requests"})
 if err!=nil{cleanup();t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{cleanup();t.Fatal(err)}
 run,res:=worker.startRun(ctx,ready)
 if res.Error!=""{cleanup();t.Fatal(res.Error)}
 return worker,ctx,run,cleanup
}

func TestUnknownDispatchAtomicInterruptAndJournal(t *testing.T){
 worker,ctx,run,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 result:=worker.interruptDispatchForReconciliation(ctx,run,
  TickResult{TaskID:run.TaskID,RunID:run.ID},"model","model_deployment",
  "candidate-1","inference-1",errors.New("transport uncertain"))
 if result.Status!="blocked"{t.Fatalf("unknown call did not block: %+v",result)}
 var state,attemptStatus,runStatus,stepStatus string
 var count int
 if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&state);err!=nil{t.Fatal(err)}
 if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
 if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&runStatus);err!=nil{t.Fatal(err)}
 if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_steps WHERE run_id=?`,run.ID).Scan(&stepStatus);err!=nil{t.Fatal(err)}
 if err:=worker.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,run.TaskID).Scan(&count);err!=nil{t.Fatal(err)}
 if state!="blocked"||attemptStatus!="interrupted"||runStatus!="interrupted"||stepStatus!="unknown"||count!=1{
  t.Fatalf("not atomic: task=%s attempt=%s run=%s journal=%s count=%d",state,attemptStatus,runStatus,stepStatus,count)
 }
 recovered,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0{t.Fatalf("completed recovery repeated: count=%d err=%v",recovered,err)}
}

func TestUnknownDispatchFailedCommitHaltsSchedulingUntilRecovery(t *testing.T){
 worker,ctx,run,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 result:=worker.interruptDispatchForReconciliation(cancelled,run,
  TickResult{TaskID:run.TaskID,RunID:run.ID},"agent_runtime","agent_runtime",
  "runtime-1","agentinv-1",errors.New("remote result unknown"))
 if result.Status!="failed"||!worker.dispatchReconciliationFailed{
  t.Fatalf("failed persistence did not halt Worker: %+v",result)
 }
 if _,err:=worker.Tick(ctx,1);!errors.Is(err,ErrDispatchRequiresReconciliation){
  t.Fatalf("uncertain Worker was retried: %v",err)
 }
 recovered,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=1{t.Fatalf("startup safety recovery: count=%d err=%v",recovered,err)}
 if worker.dispatchReconciliationFailed{t.Fatal("verified recovery failed to clear scheduler alarm")}
 var state,attempt,runState string
 _=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&state)
 _=worker.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).Scan(&attempt)
 _=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&runState)
 if state!="blocked"||attempt!="interrupted"||runState!="interrupted"{
  t.Fatalf("startup did not hold uncertain work: task=%s attempt=%s run=%s",state,attempt,runState)
 }
}

func TestUnknownDispatchRefusesStaleAttemptOwnership(t *testing.T){
 worker,ctx,run,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 // Synthetic corruption: the durable Worker still claims this Attempt,
 // but it is no longer active. Do not interrupt a different Attempt.
 if _,err:=worker.db.ExecContext(ctx,`UPDATE task_attempts
  SET status='failed' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
 result:=worker.interruptDispatchForReconciliation(ctx,run,
  TickResult{TaskID:run.TaskID,RunID:run.ID},"model","model_deployment",
  "candidate-1","inference-1",errors.New("unknown transport outcome"))
 if result.Status!="failed"||!worker.dispatchReconciliationFailed{
  t.Fatalf("stale original Attempt was accepted: %+v",result)
 }
 var state,runState string
 if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&state);err!=nil{t.Fatal(err)}
 if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if state!="running"||runState!="running"{
  t.Fatalf("invalid Attempt partially mutated durable records: task=%s run=%s",state,runState)
 }
 if _,err:=worker.Tick(ctx,1);!errors.Is(err,ErrDispatchRequiresReconciliation){
  t.Fatalf("corrupt in-flight result was retried: %v",err)
 }
}
