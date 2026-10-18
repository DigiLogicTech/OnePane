//go:build integration

package agentworker

import (
 "context"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestWorkerWaitingTransitionsAtomicAndExact(t *testing.T){
 for _,tc:=range []struct{kind,status,stringStatus,taskStatus,audit string}{
  {"wait","paused","waiting","paused","task.paused"},
  {"human","waiting_human","waiting","waiting_approval","task.waiting_approval"},
 }{
  t.Run(tc.kind,func(t *testing.T){
   w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
   next:=task.StatePaused
   if tc.kind=="human"{next=task.StateWaitingApproval}
   out:=w.waitRunAtomically(ctx,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
    next,"await input",[]byte(`{"state":"parked"}`),tc.kind,tc.status)
   if out.Status!=tc.status{t.Fatalf("wait not committed: %+v",out)}
   var runState,taskState,attemptState,cont string
   var count int
   if err:=w.db.QueryRowContext(ctx,`SELECT status,continuation_json FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState,&cont);err!=nil{t.Fatal(err)}
   if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
   if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
   if err:=w.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type=?`,r.TaskID,tc.audit).Scan(&count);err!=nil{t.Fatal(err)}
   if runState!=tc.stringStatus||taskState!=tc.taskStatus||attemptState!="waiting"||cont!=`{"state":"parked"}`||count!=1{
    t.Fatalf("wait state mismatch run=%s task=%s attempt=%s cont=%s audit=%d",runState,taskState,attemptState,cont,count)
   }
   if w.dispatchReconciliationFailed{t.Fatal("successful wait must not poison scheduler")}
  })
 }
}

func TestWorkerWaitCancelledTransactionDoesNotStrandWaitingRun(t *testing.T){
 w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 out:=w.waitRunAtomically(cancelled,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
  task.StatePaused,"pause",[]byte(`{"wait_reason":"pause"}`),"wait","paused")
 if out.Status!="failed"||!w.dispatchReconciliationFailed{t.Fatalf("uncommitted wait reported success: %+v",out)}
 var runState,taskState,attemptState string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
 if runState!="running"||taskState!="running"||attemptState!="running"{
  t.Fatalf("partial wait committed run=%s task=%s attempt=%s",runState,taskState,attemptState)
 }
 if _,err:=w.Tick(ctx,1);!errors.Is(err,ErrDispatchRequiresReconciliation){t.Fatalf("busy Worker was retried: %v",err)}
 recovered,err:=w.RecoverLostRuns(ctx)
 if err!=nil||recovered!=1||w.dispatchReconciliationFailed{t.Fatalf("recovery count=%d err=%v",recovered,err)}
}

func TestWorkerWaitStaleAttemptRefusedBeforeStateChange(t *testing.T){
 w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='failed' WHERE id=?`,r.AttemptID);err!=nil{t.Fatal(err)}
 out:=w.waitRunAtomically(ctx,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
  task.StateWaitingApproval,"human approval",[]byte(`{}`),"human","waiting_human")
 if out.Status!="failed"||!w.dispatchReconciliationFailed{t.Fatalf("stale Attempt received wait: %+v",out)}
 var runState string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if runState!="running"{t.Fatalf("stale Attempt parked Worker anyway: %s",runState)}
}
