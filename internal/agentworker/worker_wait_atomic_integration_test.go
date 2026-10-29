//go:build integration

package agentworker

import (
 "context"
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
 if _,err:=w.Tick(ctx,1);err!=nil{t.Fatalf("in-process wait park failed: %v",err)}
 recovered,err:=w.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0||w.dispatchReconciliationFailed{t.Fatalf("live wait park was not durable: count=%d err=%v",recovered,err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if runState!="interrupted"{t.Fatalf("uncommitted wait was replayed: %s",runState)}
}

func TestWorkerWaitStaleAttemptRefusedBeforeStateChange(t *testing.T){
 w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='failed' WHERE id=?`,r.AttemptID);err!=nil{t.Fatal(err)}
 out:=w.waitRunAtomically(ctx,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
  task.StateWaitingApproval,"human approval",[]byte(`{}`),"human","waiting_human")
 if out.Status!="failed"||w.dispatchReconciliationFailed{t.Fatalf("stale Attempt received wait or halted another owner: %+v",out)}
 var runState string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if runState!="running"{t.Fatalf("stale Attempt parked Worker anyway: %s",runState)}
}

func TestExternalWorkerWakeRejectsMismatchedOriginalAttempt(t *testing.T) {
 w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 out:=w.waitRunAtomically(ctx,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
  task.StatePaused,"human pause",[]byte(`{}`),"wait","paused")
 if out.Status!="paused"{t.Fatalf("fixture did not pause: %+v",out)}
 // Corrupt legacy Task-only resume, without resuming its Attempt.
 if _,err:=w.db.ExecContext(ctx,`UPDATE tasks SET state='running',revision=revision+1 WHERE id=?`,r.TaskID);err!=nil{t.Fatal(err)}
 if err:=w.syncResumedRuns(ctx);err==nil{t.Fatal("mismatched original Attempt was silently accepted")}
 var state string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="waiting"{t.Fatalf("corrupt Worker woke: %s",state)}
 // A repaired original Attempt may resume; no new Attempt is created.
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='running' WHERE id=?`,r.AttemptID);err!=nil{t.Fatal(err)}
 if err:=w.syncResumedRuns(ctx);err!=nil{t.Fatalf("valid Attempt still blocked: %v",err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="running"{t.Fatalf("legitimate Worker did not wake: %s",state)}
}

func TestExternalWorkerWakeRejectsForeignPrincipal(t *testing.T) {
 w,ctx,r,cleanup:=prepareUnknownDispatch(t);defer cleanup()
 out:=w.waitRunAtomically(ctx,r,TickResult{TaskID:r.TaskID,RunID:r.ID},
  task.StatePaused,"human pause",[]byte(`{}`),"wait","paused")
 if out.Status!="paused"{t.Fatalf("fixture did not pause: %+v",out)}
 if _,err:=w.db.ExecContext(ctx,`UPDATE tasks SET state='running',revision=revision+1 WHERE id=?`,r.TaskID);err!=nil{t.Fatal(err)}
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='running',worker_principal_id=? WHERE id=?`,
  AuthorityPrincipal,r.AttemptID);err!=nil{t.Fatal(err)}
 if err:=w.syncResumedRuns(ctx);err==nil{t.Fatal("foreign Attempt was promoted")}
 var state string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="waiting"{t.Fatalf("foreign principal woke Worker: %s",state)}
}
