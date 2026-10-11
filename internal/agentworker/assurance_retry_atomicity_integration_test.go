//go:build integration

package agentworker

import (
 "context"
 "errors"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestAssuranceRetryAtomicOriginalAttemptAndJournalRollback(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"retry.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:"Retain failed assurance provenance"})
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,res:=worker.startRun(ctx,ready)
 if res.Error!=""{t.Fatal(res.Error)}
 requested,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:ready.Revision+1,
 })
 if err!=nil{t.Fatal(err)}
 verifying,err:=tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:requested.Revision,
 })
 if err!=nil{t.Fatal(err)}
 if err:=worker.finishRun(ctx,run.ID,RunBlocked,"independent verifier evaluating");err!=nil{t.Fatal(err)}
 verifier:=verification.NewService(db.SQL(),db,clock.Real{})
 v,err:=verifier.Create(ctx,verification.CreateCommand{
  WorkspaceID:"tenant",TaskID:&created.ID,SubjectRef:"agent_worker_run:"+run.ID,
  RequiredLevel:policy.VerificationV1,
  Spec:[]byte(`{"source":"agent_completion_proposal","worker_run_id":"`+run.ID+`"}`),
 })
 if err!=nil{t.Fatal(err)}
 if err:=worker.AssuranceRetry(ctx,run.ID,v.ID,[]byte(`{}`));!errors.Is(err,ErrInvalidWorkerState){
  t.Fatalf("pending verification resumed execution: %v",err)
 }
 v,err=verifier.Resolve(ctx,verification.ResolveCommand{
  VerificationID:v.ID,ExpectedRevision:v.Revision,
  Status:verification.StatusInconclusive,VerifiedBy:VerifierPrincipal,
 })
 if err!=nil{t.Fatal(err)}
 if err:=worker.AssuranceRetry(ctx,run.ID,"unrelated-verification",[]byte(`{}`));!errors.Is(err,ErrInvalidWorkerState){
  t.Fatalf("unrelated verification resumed execution: %v",err)
 }
 // The same Task's failed Verification is not sufficient: it must have
 // been issued to the exact Agent Worker run being reactivated.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE verifications
  SET spec_json='{"source":"agent_completion_proposal","worker_run_id":"old-worker"}'
  WHERE id=?`,v.ID);err!=nil{t.Fatal(err)}
 if err:=worker.AssuranceRetry(ctx,run.ID,v.ID,[]byte(`{}`));!errors.Is(err,ErrInvalidWorkerState){
  t.Fatalf("foreign Worker provenance resumed current Attempt: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE verifications SET spec_json=?
  WHERE id=?`,`{"source":"agent_completion_proposal","worker_run_id":"`+run.ID+`"}`,v.ID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER refuse_assurance_retry
  BEFORE INSERT ON agent_worker_steps
  WHEN NEW.step_kind='verification' AND NEW.status='failed'
   AND json_extract(NEW.detail_json,'$.action')='retry'
  BEGIN SELECT RAISE(ABORT,'inject retry journal failure'); END`);err!=nil{t.Fatal(err)}
 if err:=worker.AssuranceRetry(ctx,run.ID,v.ID,[]byte(`{"reason":"insufficient evidence"}`));err==nil{
  t.Fatal("injected journal failure did not abort coordinated assurance retry")
 }
 state,err:=tasks.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if state.State!=task.StateVerifying||state.Revision!=verifying.Revision{
  t.Fatalf("retry changed Task after journal failed: %+v",state)
 }
 current,err:=worker.getRun(ctx,run.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!=RunBlocked{t.Fatalf("Worker half-resumed after journal failure: %+v",current)}
 var attemptStatus string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,
  run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
 if attemptStatus!="waiting"{t.Fatalf("original Attempt half-resumed: %s",attemptStatus)}
 var audit int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type='task.verification_retry'`,created.ID).
  Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=0{t.Fatalf("failed retry recorded %d false Task audit events",audit)}
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER refuse_assurance_retry`);err!=nil{t.Fatal(err)}
 if err:=worker.AssuranceRetry(ctx,run.ID,v.ID,[]byte(`{"reason":"insufficient evidence"}`));err!=nil{
  t.Fatalf("legitimate matched retry refused: %v",err)
 }
 state,err=tasks.Get(ctx,created.ID)
 if err!=nil||state.State!=task.StateRunning{t.Fatalf("Task did not resume: %+v %v",state,err)}
 current,err=worker.getRun(ctx,run.ID)
 if err!=nil||current.Status!=RunRunning{t.Fatalf("Worker did not resume: %+v %v",current,err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,
  run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
 if attemptStatus!="running"{t.Fatalf("original Attempt did not resume: %s",attemptStatus)}
 if err:=worker.AssuranceRetry(ctx,run.ID,v.ID,[]byte(`{}`));err!=nil{
  t.Fatalf("idempotent same-verification retry rejected: %v",err)
 }
 if err:=worker.AssuranceRetry(ctx,run.ID,"unrelated",[]byte(`{}`));!errors.Is(err,ErrInvalidWorkerState){
  t.Fatalf("RUNNING Worker silently accepted unrelated evidence: %v",err)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
  WHERE run_id=? AND step_kind='verification' AND status='failed'
  AND json_extract(detail_json,'$.action')='retry'`,run.ID).Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=1{t.Fatalf("retry recorded %d journal entries; want 1",audit)}
}
