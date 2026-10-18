//go:build integration

package agentworker

import (
 "context"
 "errors"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A separate Tick must never initiate another step while this Service has
// an unresolved inference/proposal cycle. Crash recovery also cannot treat
// the currently executing step as evidence that its effects did not happen.
func TestWorkerCycleGuardRefusesOverlapWithoutMutatingUnknownEffects(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"worker-cycle.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"Never replay unknown external side effects"})
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 run,res:=worker.startRun(ctx,ready)
 if res.Error!=""{t.Fatal(res.Error)}
 // Hold the exact shared cycle mutex, as if an earlier Tick is inside a
 // long-running model invocation. Both operations must return immediately
 // without opening new external requests or mutating Task/Attempt/Worker.
 worker.workerCycleMu.Lock()
 type response struct{err error;count int}
 tickCh:=make(chan response,1)
 recoverCh:=make(chan response,1)
 go func(){
  results,err:=worker.Tick(ctx,1)
  tickCh<-response{err:err,count:len(results)}
 }()
 go func(){
  count,err:=worker.RecoverLostRuns(ctx)
  recoverCh<-response{err:err,count:count}
 }()
 check:=func(name string,ch <-chan response){
  t.Helper()
  select{
  case got:=<-ch:
   if !errors.Is(got.err,ErrWorkerCycleInProgress)||got.count!=0{
    t.Fatalf("%s tried to execute busy Worker: %+v",name,got)
   }
  case <-time.After(5*time.Second):
   t.Fatalf("%s blocked instead of refusing an overlapping scheduler cycle",name)
  }
 }
 check("Tick",tickCh)
 check("RecoverLostRuns",recoverCh)
 var runStatus,attemptStatus,taskState string
 var steps int64
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status,step_count
  FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&runStatus,&steps);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status
  FROM task_attempts WHERE id=?`,run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT state
  FROM tasks WHERE id=?`,run.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if runStatus!="running"||attemptStatus!="running"||taskState!="running"||steps!=0{
  t.Fatalf("busy cycle mutated uncertain attempt: run=%s attempt=%s task=%s steps=%d",
   runStatus,attemptStatus,taskState,steps)
 }
 worker.workerCycleMu.Unlock()
 // Once the real scheduler cycle is no longer active, startup recovery
 // remains fail-closed and journals the unknown outcome exactly once.
 count,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||count!=1{t.Fatalf("safe recovery count=%d err=%v",count,err)}
 count,err=worker.RecoverLostRuns(ctx)
 if err!=nil||count!=0{t.Fatalf("second recovery duplicated effects: count=%d err=%v",count,err)}
 var audit,outbox int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM events WHERE event_type='task.attempt_interrupted'
  AND aggregate_id=?`,run.TaskID).Scan(&audit);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM outbox_jobs WHERE job_type='task.recovery.required'
  AND workspace_id='tenant'`).Scan(&outbox);err!=nil{t.Fatal(err)}
 if audit!=1||outbox!=1{
  t.Fatalf("lost or duplicate crash-recovery audit: events=%d outbox=%d",audit,outbox)
 }
}
