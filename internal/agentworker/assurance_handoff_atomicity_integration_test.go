//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "errors"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestIndependentAssuranceHandoffWorkerAndJournalCommitTogether(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"assurance-handoff.db"))
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
 created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:"Needs independent assurance"})
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,started:=worker.startRun(ctx,ready)
 if started.Error!=""||run.Status!=RunRunning{t.Fatalf("failed initial Task start: %+v %+v",run,started)}
 request,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:ready.Revision+1,
 })
 if err!=nil{t.Fatal(err)}
 _,err=tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:request.Revision,
 })
 if err!=nil{t.Fatal(err)}
 verifier:=verification.NewService(db.SQL(),db,clk)
 v,err:=verifier.Create(ctx,verification.CreateCommand{
  WorkspaceID:"tenant",TaskID:&created.ID,SubjectRef:"agent_worker_run:"+run.ID,
  RequiredLevel:policy.VerificationV1,
  Spec:[]byte(`{"assurance_version":1,"source":"agent_completion_proposal","worker_run_id":"`+run.ID+`"}`),
 })
 if err!=nil{t.Fatal(err)}
 continuation,_:=json.Marshal(map[string]any{"verification_id":v.ID,
  "required_verification":"V1"})
 if err:=worker.blockForIndependentAssurance(ctx,run,"wrong",continuation);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("foreign V1 accepted: %v",err)}
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_waiting_journal
  BEFORE INSERT ON agent_worker_steps
  WHEN NEW.step_kind='verification' AND NEW.status='waiting'
  BEGIN SELECT RAISE(ABORT,'injected assurance handoff failure'); END`);err!=nil{t.Fatal(err)}
 if err:=worker.blockForIndependentAssurance(ctx,run,v.ID,continuation);err==nil{
  t.Fatal("failed Worker journal still allowed transition")
 }
 after,err:=worker.getRun(ctx,run.ID)
 if err!=nil{t.Fatal(err)}
 if after.Status!=RunRunning||after.Revision!=run.Revision{
  t.Fatalf("blocked Worker persisted without journal: %+v",after)
 }
 var steps int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
  WHERE run_id=? AND step_kind='verification' AND status='waiting'`,
  run.ID).Scan(&steps);err!=nil{t.Fatal(err)}
 if steps!=0{t.Fatalf("partial waiting audit committed %d steps",steps)}
 taskCurrent,err:=tasks.Get(ctx,created.ID)
 if err!=nil||taskCurrent.State!=task.StateVerifying{
  t.Fatalf("handoff mutated verifying Task: %+v %v",taskCurrent,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_waiting_journal`);err!=nil{t.Fatal(err)}
 if err:=worker.blockForIndependentAssurance(ctx,run,v.ID,continuation);err!=nil{
  t.Fatalf("valid independent handoff did not persist: %v",err)
 }
 after,err=worker.getRun(ctx,run.ID)
 if err!=nil||after.Status!=RunBlocked{
  t.Fatalf("completed handoff Worker status: %+v %v",after,err)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
  WHERE run_id=? AND step_kind='verification' AND status='waiting'
   AND result_ref=? AND json_extract(detail_json,'$.independent')=1`,
  run.ID,v.ID).Scan(&steps);err!=nil{t.Fatal(err)}
 if steps!=1{t.Fatalf("handoff lacks exactly one honest journal: %d",steps)}
 if err:=worker.blockForIndependentAssurance(ctx,run,v.ID,continuation);
  !errors.Is(err,ErrInvalidWorkerState){t.Fatalf("stale handoff replayed: %v",err)}
}
