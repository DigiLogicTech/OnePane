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

func TestV0DeclarationCompletionIsAtomicAndNeverIndependentAssurance(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"v0-atomic.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 now:=clk.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clk)
 worker:=New(db.SQL(),db,clk,"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"Report only a model declaration",
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,started:=worker.startRun(ctx,ready)
 if started.Error!=""||run.Status!=RunRunning{t.Fatalf("start: %+v %+v",run,started)}
 requested,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:ready.Revision+1,
 })
 if err!=nil{t.Fatal(err)}
 verifying,err:=tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:requested.Revision,
 })
 if err!=nil{t.Fatal(err)}
 verifier:=verification.NewService(db.SQL(),db,clk)
 create:=func(level policy.VerificationLevel)verification.Verification{
  t.Helper()
  v,e:=verifier.Create(ctx,verification.CreateCommand{
   WorkspaceID:"tenant",TaskID:&created.ID,
   SubjectRef:"agent_worker_run:"+run.ID,
   RequiredLevel:level,
   Spec:[]byte(`{"source":"agent_completion_proposal","worker_run_id":"`+run.ID+`","rule":"V0 is declaration only"}`),
  })
  if e!=nil{t.Fatal(e)}
  v,e=verifier.Resolve(ctx,verification.ResolveCommand{
   VerificationID:v.ID,ExpectedRevision:v.Revision,
   Status:verification.StatusPass,AchievedLevel:&level,
   VerifiedBy:VerifierPrincipal,
  })
  if e!=nil{t.Fatal(e)}
  return v
 }
 // A V1 checkpoint cannot be silently downgraded to the V0 shortcut.
 // Make V1 first: the next checkpoint supersedes its predecessor.
 v1:=create(policy.VerificationV1)
 cp1,err:=verifier.CreateCheckpoint(ctx,verification.CheckpointCommand{
  WorkspaceID:"tenant",TaskID:created.ID,VerificationID:v1.ID,
  State:[]byte(`{"level":"V1"}`),
 })
 if err!=nil{t.Fatal(err)}
 v:=create(policy.VerificationV0)
 cp,err:=verifier.CreateCheckpoint(ctx,verification.CheckpointCommand{
  WorkspaceID:"tenant",TaskID:created.ID,VerificationID:v.ID,
  State:[]byte(`{"level":"V0","independent":false}`),
 })
 if err!=nil{t.Fatal(err)}
 result:=[]byte(`{"declared":"done"}`)
 if err:=worker.completeV0InTransaction(ctx,run,verifying,"not-the-verification",cp.ID,result);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("foreign Verification finalized: %v",err)}
 if err:=worker.completeV0InTransaction(ctx,run,verifying,v1.ID,cp1.ID,result);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("V1 evidence bypassed independent assurance: %v",err)}
 // The V0 completion's terminal Task/Attempt and Worker update must be
 // undone together if its journal write fails after the status update.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_v0_journal
  BEFORE INSERT ON agent_worker_steps
  WHEN NEW.step_kind='verification' AND NEW.status='succeeded'
  BEGIN SELECT RAISE(ABORT,'injected V0 declaration journal failure'); END`);err!=nil{t.Fatal(err)}
 if err:=worker.completeV0InTransaction(ctx,run,verifying,v.ID,cp.ID,result);err==nil{
  t.Fatal("failed V0 audit still committed Task completion")
 }
 check:=func(wantTask task.State,wantAttempt,wantRun string,wantAudit int){
  t.Helper()
  current,e:=tasks.Get(ctx,created.ID)
  if e!=nil{t.Fatal(e)}
  if current.State!=wantTask{t.Fatalf("Task state %s want %s",current.State,wantTask)}
  var attemptStatus,workerStatus string
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,
   run.AttemptID).Scan(&attemptStatus);e!=nil{t.Fatal(e)}
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
   run.ID).Scan(&workerStatus);e!=nil{t.Fatal(e)}
  if attemptStatus!=wantAttempt||workerStatus!=wantRun{
   t.Fatalf("Task/Attempt/Worker drift: %s %s %s",current.State,attemptStatus,workerStatus)
  }
  var audited int
  if e=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
   WHERE aggregate_id=? AND event_type='task.completed'`,created.ID).Scan(&audited);e!=nil{t.Fatal(e)}
  if audited!=wantAudit{t.Fatalf("durable Task completions=%d want %d",audited,wantAudit)}
 }
 check(task.StateVerifying,"waiting","running",0)
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_v0_journal`);err!=nil{t.Fatal(err)}
 // Scope loss after the model's completion proposal must prevent V0 commit.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE workspaces SET status='suspended'
  WHERE id='tenant'`);err!=nil{t.Fatal(err)}
 if err:=worker.completeV0InTransaction(ctx,run,verifying,v.ID,cp.ID,result);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("inactive tenancy accepted V0 declaration: %v",err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE workspaces SET status='active'
  WHERE id='tenant'`);err!=nil{t.Fatal(err)}
 if err:=worker.completeV0InTransaction(ctx,run,verifying,v.ID,cp.ID,result);err!=nil{
  t.Fatalf("legitimate V0 atomic completion rejected: %v",err)
 }
 check(task.StateComplete,"succeeded","succeeded",1)
 var audit int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
  WHERE run_id=? AND step_kind='verification' AND status='succeeded'
   AND json_extract(detail_json,'$.independent')=0
   AND json_extract(detail_json,'$.semantics')='model_declaration_only'`,
  run.ID).Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=1{t.Fatalf("V0 misrepresented as independently verified or duplicated: %d",audit)}
 if err:=worker.completeV0InTransaction(ctx,run,verifying,v.ID,cp.ID,result);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("stale V0 proposal replayed: %v",err)}
 check(task.StateComplete,"succeeded","succeeded",1)
}
