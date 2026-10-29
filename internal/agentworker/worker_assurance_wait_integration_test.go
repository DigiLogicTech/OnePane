//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIndependentAssuranceParksWorkerWithSchemaValidJournal(t *testing.T){
 w,ctx,r,_,_,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 out:=w.parkWorkerForIndependentAssurance(ctx,r,
  TickResult{TaskID:r.TaskID,RunID:r.ID},json.RawMessage(`{"level":"V2"}`),
  "pending-verification","V2")
 if out.Status!="blocked_verification"{t.Fatalf("assurance not durably parked: %+v",out)}
 var runState,taskState,attemptState,journalStatus,continuation string
 if err:=w.db.QueryRowContext(ctx,`SELECT status,continuation_json FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState,&continuation);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_steps WHERE run_id=? AND step_kind='verification'`,r.ID).Scan(&journalStatus);err!=nil{t.Fatal(err)}
 if runState!="blocked"||taskState!="verifying"||attemptState!="waiting"||
  journalStatus!="waiting"||continuation!=`{"level":"V2"}`{
  t.Fatalf("assurance inconsistent run=%s task=%s attempt=%s journal=%s continuation=%s",
   runState,taskState,attemptState,journalStatus,continuation)
 }
 if w.dispatchReconciliationFailed{t.Fatal("committed assurance park halted scheduler")}
}

func TestIndependentAssuranceFailedParkDoesNotHideRunningWorker(t *testing.T){
 w,ctx,r,_,_,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 out:=w.parkWorkerForIndependentAssurance(cancelled,r,
  TickResult{TaskID:r.TaskID,RunID:r.ID},json.RawMessage(`{}`),
  "pending-verification","V3")
 if out.Status!="failed"||!w.dispatchReconciliationFailed{
  t.Fatalf("uncommitted assurance park was reported successful: %+v",out)
 }
 var runState,taskState string
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if runState!="running"||taskState!="verifying"{
  t.Fatalf("incomplete assurance park mutated state: %s/%s",runState,taskState)
 }
 if _,err:=w.Tick(ctx,1);err!=nil{
  t.Fatalf("in-process assurance park failed: %v",err)
 }
 recovered,err:=w.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0||w.dispatchReconciliationFailed{
  t.Fatalf("live assurance park was not durable: n=%d err=%v",recovered,err)
 }
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if runState!="interrupted"{t.Fatalf("assurance worker was replayed: %s",runState)}
}

func TestIndependentAssuranceStaleAttemptRejected(t *testing.T){
 w,ctx,r,_,_,cleanup:=prepareVerifiedWorkerCompletion(t);defer cleanup()
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='failed' WHERE id=?`,r.AttemptID);err!=nil{t.Fatal(err)}
 out:=w.parkWorkerForIndependentAssurance(ctx,r,
  TickResult{TaskID:r.TaskID,RunID:r.ID},json.RawMessage(`{}`),
  "pending-verification","V2")
 if out.Status!="failed"||!w.dispatchReconciliationFailed{
  t.Fatalf("stale Attempt silently parked: %+v",out)
 }
}

func TestPendingIndependentAssuranceIsNotRecoveredAsOrphan(t *testing.T) {
 w,ctx,r,_,_,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 out:=w.parkWorkerForIndependentAssurance(ctx,r,
  TickResult{TaskID:r.TaskID,RunID:r.ID},json.RawMessage(`{"verification_id":"pending"}`),
  "verification-pending","V2")
 if out.Status!="blocked_verification"{t.Fatalf("cannot establish pending assurance: %+v",out)}
 n,err:=w.RecoverLostRuns(ctx)
 if err!=nil||n!=0{t.Fatalf("pending assurance misclassified as orphan: n=%d err=%v",n,err)}
 var ts,as,rs string
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&ts);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&as);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&rs);err!=nil{t.Fatal(err)}
 if ts!="verifying"||as!="waiting"||rs!="blocked"{
  t.Fatalf("pending assurance was interrupted: Task=%s Attempt=%s Worker=%s",ts,as,rs)
 }
 var events int
 if err:=w.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.attempt_interrupted'`,r.TaskID).Scan(&events);err!=nil{t.Fatal(err)}
 if events!=0{t.Fatalf("unexpected recovery event: %d",events)}
}
