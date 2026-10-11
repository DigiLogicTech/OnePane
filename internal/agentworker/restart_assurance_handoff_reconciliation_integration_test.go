//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestCrashAfterIssuedAssurancePreservesOriginalWaitingAttempt(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"assurance-handoff-crash.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 start:=func(objective string)(Run,task.Task){
  t.Helper()
  created,e:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:objective})
  if e!=nil{t.Fatal(e)}
  ready,e:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if e!=nil{t.Fatal(e)}
  run,result:=worker.startRun(ctx,ready)
  if result.Error!=""{t.Fatalf("start: %v",result)}
  return run,ready
 }
 pending,ready:=start("V1 verifier already issued before daemon crashed")
 unrelated,_:=start("Model may still be mid-dispatch on daemon crash")
 requested,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:pending.TaskID,ExpectedRevision:ready.Revision+1,
 })
 if err!=nil{t.Fatal(err)}
 verifying,err:=tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:pending.TaskID,ExpectedRevision:requested.Revision,
 })
 if err!=nil{t.Fatal(err)}
 ver:=verification.NewService(db.SQL(),db,clock.Real{})
 issued,err:=ver.Create(ctx,verification.CreateCommand{
  WorkspaceID:"tenant",TaskID:&pending.TaskID,
  SubjectRef:"agent_worker_run:"+pending.ID,
  RequiredLevel:policy.VerificationV1,
  Spec:[]byte(`{"assurance_version":1,"source":"agent_completion_proposal","worker_run_id":"`+pending.ID+`","proposed_result":{"ok":true},"criteria":{"result_checks":[{"pointer":"/ok","operator":"equals","value":true}]},"evidence":{}}`),
 })
 if err!=nil{t.Fatal(err)}
 // Simulate an SQLite failure after the Task+Verification commit but
 // before the Worker BLOCKED continuation/audit became durable.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_recovery_handoff
  BEFORE INSERT ON agent_worker_steps
  WHEN NEW.step_kind='verification' AND NEW.status='waiting'
  BEGIN SELECT RAISE(ABORT,'synthetic recovery handoff journal failure'); END`);err!=nil{t.Fatal(err)}
 recovered,problem:=worker.RecoverLostRuns(ctx)
 if recovered!=1||problem==nil{
  t.Fatalf("unrelated crash recovery should continue despite failed handoff: count=%d err=%v",recovered,problem)
 }
 check:=func(run Run,wantTask task.State,wantAttempt,wantWorker string){
  t.Helper()
  saved,e:=tasks.Get(ctx,run.TaskID)
  if e!=nil{t.Fatal(e)}
  if saved.State!=wantTask{t.Fatalf("Task %s state=%s want=%s",run.TaskID,saved.State,wantTask)}
  var a,w string
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,
   run.AttemptID).Scan(&a);e!=nil{t.Fatal(e)}
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
   run.ID).Scan(&w);e!=nil{t.Fatal(e)}
  if a!=wantAttempt||w!=wantWorker{
   t.Fatalf("Task %s Attempt/Worker mismatch: %s/%s",run.TaskID,a,w)
  }
 }
 check(pending,task.StateVerifying,"waiting","running")
 check(unrelated,task.StateBlocked,"interrupted","interrupted")
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_recovery_handoff`);err!=nil{t.Fatal(err)}
 recovered,problem=worker.RecoverLostRuns(ctx)
 if problem!=nil||recovered!=1{
  t.Fatalf("eligible orphan V1 handoff not recovered: count=%d err=%v",recovered,problem)
 }
 check(pending,task.StateVerifying,"waiting","blocked")
 var content string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT continuation_json FROM agent_worker_runs
  WHERE id=?`,pending.ID).Scan(&content);err!=nil{t.Fatal(err)}
 if !containsVerificationRef(content,issued.ID){
  t.Fatalf("recovered Worker lost original exact pending Verification ref: %s",content)
 }
 var n int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
  WHERE run_id=? AND step_kind='verification' AND status='waiting'
   AND result_ref=? AND json_extract(detail_json,'$.no_model_or_tool_replay')=1`,
  pending.ID,issued.ID).Scan(&n);err!=nil{t.Fatal(err)}
 if n!=1{t.Fatalf("recovery lacked durable safe handoff audit: %d",n)}
 recovered,problem=worker.RecoverLostRuns(ctx)
 if problem!=nil||recovered!=0{t.Fatalf("repeat recovery duplicated handoff: count=%d err=%v",recovered,problem)}
 current,err:=tasks.Get(ctx,pending.TaskID)
 if err!=nil||current.Revision!=verifying.Revision{
  t.Fatalf("safe handoff modified VERIFYING Task or original Attempt: %+v %v",current,err)
 }
}

func containsVerificationRef(raw,id string)bool{
 var v struct{VerificationID string `json:"verification_id"`}
 return json.Unmarshal([]byte(raw),&v)==nil&&v.VerificationID==id
}
