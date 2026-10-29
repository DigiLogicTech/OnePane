//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

func prepareVerifiedWorkerCompletion(t *testing.T)(
 *Service,context.Context,Run,int64,string,func(),
){
 t.Helper()
 w,ctx,r,cleanup:=prepareUnknownDispatch(t)
 actor:=WorkerPrincipal
 verifier:=VerifierPrincipal
 cur,err:=w.tasks.Get(ctx,r.TaskID)
 if err!=nil{cleanup();t.Fatal(err)}
 cur,err=w.tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:r.TaskID,ExpectedRevision:cur.Revision,ActorPrincipalID:&actor,
 })
 if err!=nil{cleanup();t.Fatal(err)}
 cur,err=w.tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:r.TaskID,ExpectedRevision:cur.Revision,ActorPrincipalID:&verifier,
 })
 if err!=nil{cleanup();t.Fatal(err)}
 vs:=verification.NewService(w.db,w.tx,clock.Real{})
 level:=policy.VerificationV0
 v,err:=vs.Create(ctx,verification.CreateCommand{
  WorkspaceID:r.WorkspaceID,TaskID:&r.TaskID,
  SubjectRef:"agent_worker_run:"+r.ID,RequiredLevel:level,
  Spec:json.RawMessage(`{}`),ActorPrincipalID:&verifier,
 })
 if err!=nil{cleanup();t.Fatal(err)}
 v,err=vs.Resolve(ctx,verification.ResolveCommand{
  VerificationID:v.ID,ExpectedRevision:v.Revision,Status:verification.StatusPass,
  AchievedLevel:&level,Result:json.RawMessage(`{}`),
  VerifiedBy:verifier,ActorPrincipalID:&verifier,
 })
 if err!=nil{cleanup();t.Fatal(err)}
 cp,err:=vs.CreateCheckpoint(ctx,verification.CheckpointCommand{
  WorkspaceID:r.WorkspaceID,TaskID:r.TaskID,VerificationID:v.ID,
  State:json.RawMessage(`{}`),ActorPrincipalID:&verifier,
 })
 if err!=nil{cleanup();t.Fatal(err)}
 return w,ctx,r,cur.Revision,cp.ID,cleanup
}

func TestVerifiedV0CompletionTaskAttemptWorkerAndJournalAtomic(t *testing.T){
 w,ctx,r,revision,checkpoint,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 result:=w.completeVerifiedWorkerRun(ctx,r,TickResult{
  TaskID:r.TaskID,RunID:r.ID},revision,checkpoint,json.RawMessage(`{"ok":true}`),"verification-id")
 if result.Status!="complete"||result.Error!=""{t.Fatalf("completed response misreported: %+v",result)}
 var taskState,attemptState,workerState,stepState,resultJSON string
 var events int
 if err:=w.db.QueryRowContext(ctx,`SELECT state,result_json FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState,&resultJSON);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&workerState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_steps WHERE run_id=? AND step_kind='verification'`,r.ID).Scan(&stepState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.completed'`,r.TaskID).Scan(&events);err!=nil{t.Fatal(err)}
 if taskState!="complete"||attemptState!="succeeded"||workerState!="succeeded"||stepState!="succeeded"||events!=1||
  resultJSON!=`{"ok":true}`{
  t.Fatalf("inconsistent completion: task=%s attempt=%s worker=%s step=%s event=%d result=%s",
   taskState,attemptState,workerState,stepState,events,resultJSON)
 }
 if w.dispatchReconciliationFailed{t.Fatal("successful atomic completion stopped scheduler")}
}

func TestVerifiedV0CancelledFinalCommitDoesNotClaimCompletion(t *testing.T){
 w,ctx,r,revision,checkpoint,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 result:=w.completeVerifiedWorkerRun(cancelled,r,TickResult{
  TaskID:r.TaskID,RunID:r.ID},revision,checkpoint,json.RawMessage(`{}`),"verification-id")
 if result.Status!="failed"||!w.dispatchReconciliationFailed{t.Fatalf("failed completion commit was reported successful: %+v",result)}
 var taskState,attemptState,workerState string
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,r.AttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&workerState);err!=nil{t.Fatal(err)}
 if taskState!="verifying"||attemptState!="waiting"||workerState!="running"{
  t.Fatalf("partial completion persisted: task=%s attempt=%s worker=%s",taskState,attemptState,workerState)
 }
 if _,err:=w.Tick(ctx,1);err!=nil{
  t.Fatalf("in-process completion park failed: %v",err)
 }
 recovered,err:=w.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0||w.dispatchReconciliationFailed{
  t.Fatalf("live completion park was not durable: n=%d err=%v",recovered,err)
 }
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if taskState!="blocked"{t.Fatalf("failed completion was retried or completed: %s",taskState)}
}

func TestVerifiedV0StaleAttemptCannotCompleteOriginalTask(t *testing.T){
 w,ctx,r,revision,checkpoint,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 if _,err:=w.db.ExecContext(ctx,`UPDATE task_attempts SET status='failed' WHERE id=?`,r.AttemptID);err!=nil{t.Fatal(err)}
 result:=w.completeVerifiedWorkerRun(ctx,r,TickResult{
  TaskID:r.TaskID,RunID:r.ID},revision,checkpoint,json.RawMessage(`{}`),"verification-id")
 if result.Status!="failed"||!w.dispatchReconciliationFailed{t.Fatalf("stale Attempt completed Worker: %+v",result)}
 var taskState,runState string
 if err:=w.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,r.TaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if err:=w.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,r.ID).Scan(&runState);err!=nil{t.Fatal(err)}
 if taskState!="verifying"||runState!="running"{t.Fatalf("stale Attempt yielded success: %s / %s",taskState,runState)}
}

func TestPublicTaskCompleteVerifiedPreservesJSONTextColumn(t *testing.T){
 w,ctx,r,revision,checkpoint,cleanup:=prepareVerifiedWorkerCompletion(t)
 defer cleanup()
 verifier:=VerifierPrincipal
 stored,err:=w.tasks.CompleteVerified(ctx,task.CompleteCommand{
  TaskID:r.TaskID,ExpectedRevision:revision,CheckpointID:checkpoint,
  Result:json.RawMessage(`{"public_api":"works"}`),
  ActorPrincipalID:&verifier,
 })
 if err!=nil{t.Fatalf("existing public completion still writes BLOB: %v",err)}
 if stored.State!=task.StateComplete||string(stored.Result)!=`{"public_api":"works"}`{
  t.Fatalf("Task public completion did not round-trip JSON TEXT: state=%s result=%s",stored.State,stored.Result)
 }
}
