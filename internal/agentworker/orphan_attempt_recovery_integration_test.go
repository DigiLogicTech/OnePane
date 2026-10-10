//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestCrashRecoveryDetectsOrphanedAgentWorkerAttemptWithoutRetry(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"orphan.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=int64(1700000000000)
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 tasks:=task.NewService(db.SQL(),db,clk)
 worker:=New(db.SQL(),db,clk,"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 createReady:=func(objective string)task.Task{
  t.Helper()
  made,e:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:objective})
  if e!=nil{t.Fatal(e)}
  made,e=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:made.ID,ExpectedRevision:made.Revision})
  if e!=nil{t.Fatal(e)}
  return made
 }
 owned:=createReady("orphaned Agent Worker action has uncertain side effects")
 agent:=WorkerPrincipal
 _,orphanAttempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:owned.ID,ExpectedRevision:owned.Revision,
  WorkerPrincipalID:&agent,Metadata:json.RawMessage(`{"agent_worker":"v1"}`),
 })
 if err!=nil{t.Fatal(err)}
 // An unrelated running executor is NOT part of Agent Worker recovery.
 foreign:=createReady("other Task execution owned by another protocol")
 other:=AuthorityPrincipal
 _,foreignAttempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:foreign.ID,ExpectedRevision:foreign.Revision,
  WorkerPrincipalID:&other,Metadata:json.RawMessage(`{"agent_worker":"v1"}`),
 })
 if err!=nil{t.Fatal(err)}
 // A legitimate parked Worker must keep the exact existing Attempt.
 parked:=createReady("local model is not yet available")
 live,res:=worker.startRun(ctx,parked)
 if res.Error!=""{t.Fatal(res.Error)}
 parkedTask,err:=tasks.Get(ctx,parked.ID)
 if err!=nil{t.Fatal(err)}
 _,err=tasks.WaitDependency(ctx,task.TransitionCommand{
  TaskID:parked.ID,ExpectedRevision:parkedTask.Revision})
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
  SET status='waiting' WHERE id=?`,live.ID);err!=nil{t.Fatal(err)}
 recovered,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=1{t.Fatalf("reconciled=%d err=%v",recovered,err)}
 var orphanStatus,foreignStatus,parkedStatus string
 for _,v:=range []struct{id string;out *string}{
  {orphanAttempt.ID,&orphanStatus},{foreignAttempt.ID,&foreignStatus},
  {live.AttemptID,&parkedStatus},
 }{
  if err=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,
   v.id).Scan(v.out);err!=nil{t.Fatal(err)}
 }
 if orphanStatus!="interrupted"||foreignStatus!="running"||parkedStatus!="waiting"{
  t.Fatalf("incorrect Attempt recovery: orphan=%s foreign=%s parked=%s",
   orphanStatus,foreignStatus,parkedStatus)
 }
 got,err:=tasks.Get(ctx,owned.ID)
 if err!=nil||got.State!=task.StateBlocked{
  t.Fatalf("orphan Task not fail-closed: %+v %v",got,err)
 }
 var audit,outbox int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type='task.attempt_interrupted'`,
  owned.ID).Scan(&audit);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM outbox_jobs
  WHERE workspace_id='tenant' AND job_type='task.recovery.required'
  AND json_extract(payload_json,'$.attempt_id')=?`,
  orphanAttempt.ID).Scan(&outbox);err!=nil{t.Fatal(err)}
 if audit!=1||outbox!=1{t.Fatalf("missing atomic audit/outbox: %d/%d",audit,outbox)}
 again,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||again!=0{t.Fatalf("idempotent orphan recovery failed: %d %v",again,err)}
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type='task.attempt_interrupted'`,
  owned.ID).Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=1{t.Fatal("duplicate orphan recovery event")}
}

func TestOrphanRecoveryRollsBackWhenAttemptAlreadyTerminal(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"orphan-corrupt.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=int64(1700000000000)
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
 VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 made,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:"terminal nonactive attempt"})
 if err!=nil{t.Fatal(err)}
 made,err=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:made.ID,ExpectedRevision:made.Revision})
 if err!=nil{t.Fatal(err)}
 agent:=WorkerPrincipal
 _,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:made.ID,ExpectedRevision:made.Revision,
  WorkerPrincipalID:&agent,Metadata:json.RawMessage(`{"agent_worker":"v1"}`),
 })
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE task_attempts SET status='succeeded'
 WHERE id=?`,attempt.ID);err!=nil{t.Fatal(err)}
 recovered,err:=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0{t.Fatalf("terminal nonorphan mutated: %d %v",recovered,err)}
 state,err:=tasks.Get(ctx,made.ID)
 if err!=nil||state.State!=task.StateRunning{t.Fatalf("terminal task mutated %+v %v",state,err)}
}
