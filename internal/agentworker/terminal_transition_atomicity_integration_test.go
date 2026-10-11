//go:build integration

package agentworker

import (
 "context"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestWorkerTerminalTransitionsRollbackAcrossTaskAttemptWorkerAndJournal(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"terminal-atomic.db"))
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
 start:=func(objective string)Run{
  t.Helper()
  created,e:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:objective})
  if e!=nil{t.Fatal(e)}
  ready,e:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if e!=nil{t.Fatal(e)}
  run,result:=worker.startRun(ctx,ready)
  if result.Error!=""||run.ID==""{t.Fatalf("start failed: %+v %+v",run,result)}
  return run
 }
 fail:=start("Fail this original Task with audit")
 block:=start("Block this original Task with audit")
 healthy:=start("Keep this independent Task running")
 check:=func(r Run,state task.State,attempt,workerStatus string,audit int){
  t.Helper()
  got,e:=tasks.Get(ctx,r.TaskID)
  if e!=nil{t.Fatal(e)}
  if got.State!=state{t.Fatalf("Task %s state=%s want=%s",r.TaskID,got.State,state)}
  var a,w string
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts
   WHERE id=?`,r.AttemptID).Scan(&a);e!=nil{t.Fatal(e)}
  if e=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs
   WHERE id=?`,r.ID).Scan(&w);e!=nil{t.Fatal(e)}
  if a!=attempt||w!=workerStatus{
   t.Fatalf("Task/Attempt/Worker mismatch: %s/%s/%s",got.State,a,w)
  }
  var n int
  if e=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
   WHERE run_id=? AND step_kind='control'`,r.ID).Scan(&n);e!=nil{t.Fatal(e)}
  if n!=audit{t.Fatalf("Worker journal count=%d want=%d",n,audit)}
 }
 for _,tc:=range []struct{run Run;status string;blocked bool}{
  {fail,"failed",false},{block,"waiting",true},
 }{
  // If any immutable journal insert fails, even the Task's event and
  // Attempt transition must roll back. A false terminal result is forbidden.
  if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER refuse_terminal_journal
   BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='control'
   BEGIN SELECT RAISE(ABORT,'synthetic Worker journal outage'); END`);err!=nil{t.Fatal(err)}
  e:=worker.transitionTerminalWorker(ctx,tc.run,"operator-visible failure",tc.blocked)
  if e==nil{t.Fatalf("journal failure committed terminal status on %s",tc.run.ID)}
  if !strings.Contains(e.Error(),"synthetic Worker journal outage"){
   t.Fatalf("terminal failed for wrong reason: %v",e)
  }
  check(tc.run,task.StateRunning,"running","running",0)
  if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER refuse_terminal_journal`);err!=nil{t.Fatal(err)}
  if err:=worker.transitionTerminalWorker(ctx,tc.run,"operator-visible failure",tc.blocked);err!=nil{
   t.Fatalf("valid terminal %s denied: %v",tc.run.ID,err)
  }
  if tc.blocked{check(tc.run,task.StateBlocked,"waiting","blocked",1)
  }else{check(tc.run,task.StateFailed,"failed","failed",1)}
  if err:=worker.transitionTerminalWorker(ctx,tc.run,"stale terminal replay",tc.blocked);err==nil{
   t.Fatalf("terminal Worker %s accepted duplicate transition",tc.run.ID)
  }
 }
 check(healthy,task.StateRunning,"running","running",0)
 // The original active Attempt identity is mandatory. A sibling's
 // Attempt cannot be used to fail or block an unrelated Task.
 if err:=worker.transitionTerminalWorker(ctx,Run{
  ID:healthy.ID,TaskID:healthy.TaskID,WorkspaceID:healthy.WorkspaceID,
  AttemptID:fail.AttemptID,WorkerPrincipalID:WorkerPrincipal,Revision:healthy.Revision,
 },"foreign attempt",false);err==nil{
  t.Fatal("foreign Task Attempt terminalized healthy Worker")
 }
 check(healthy,task.StateRunning,"running","running",0)
}
