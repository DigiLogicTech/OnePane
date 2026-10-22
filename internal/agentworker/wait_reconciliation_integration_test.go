//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestCorruptWaitingWorkerDoesNotStarveIndependentResourceWait(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"wait-scope.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 tasks:=task.NewService(db.SQL(),db,clk)
 worker:=New(db.SQL(),db,clk,"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 startWait:=func(objective string,continuation json.RawMessage)(task.Task,Run){
  t.Helper()
  created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:objective})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatalf("start %s: %s",objective,res.Error)}
  if err:=worker.suspendForResource(ctx,run,continuation,
   "waiting on qualified local resources","route",
   map[string]any{"kind":"local-resource-wait"},nil);err!=nil{t.Fatal(err)}
  return ready,run
 }
 brokenTask,brokenRun:=startWait("Malformed model wait",json.RawMessage(`{"model_wait":"bad_schema"}`))
 operationTask,operationRun:=startWait("Null Operation wait",json.RawMessage(`{"operation_id":null}`))
 healthyTask,healthyRun:=startWait("Healthy resource wait",json.RawMessage(`{}`))
 err=worker.syncResumedRuns(ctx)
 if err==nil||!strings.Contains(err.Error(),brokenRun.ID)||
  !strings.Contains(err.Error(),operationRun.ID)||
  !strings.Contains(err.Error(),"corrupt persisted local model wait"){
  t.Fatalf("bad wait not independently reported: %v",err)
 }
 var brokenStatus,operationStatus,healthyStatus string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
  brokenRun.ID).Scan(&brokenStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
  operationRun.ID).Scan(&operationStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
  healthyRun.ID).Scan(&healthyStatus);err!=nil{t.Fatal(err)}
 if brokenStatus!=string(RunWaiting)||operationStatus!=string(RunWaiting)||
  healthyStatus!=string(RunRunning){
  t.Fatalf("bad continuations starved healthy Worker: model=%s operation=%s good=%s",
   brokenStatus,operationStatus,healthyStatus)
 }
 brokenState,err:=tasks.Get(ctx,brokenTask.ID)
 if err!=nil{t.Fatal(err)}
 operationState,err:=tasks.Get(ctx,operationTask.ID)
 if err!=nil{t.Fatal(err)}
 healthyState,err:=tasks.Get(ctx,healthyTask.ID)
 if err!=nil{t.Fatal(err)}
 if brokenState.State!=task.StateWaitingDependency||
  operationState.State!=task.StateWaitingDependency||healthyState.State!=task.StateRunning{
  t.Fatalf("wait Task states corrupted: model=%s operation=%s good=%s",
   brokenState.State,operationState.State,healthyState.State)
 }
 var active int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE task_id=? AND status='running'`,healthyRun.TaskID).Scan(&active);err!=nil{t.Fatal(err)}
 if active!=1{t.Fatalf("healthy Task original Attempt not resumed: %d",active)}
 // Replaying the same reconciler must not wake the corrupt run or duplicate
 // the healthy Task/Attempt. The error must remain visible for intervention.
 if err:=worker.syncResumedRuns(ctx);err==nil||
  !strings.Contains(err.Error(),brokenRun.ID)||
  !strings.Contains(err.Error(),operationRun.ID){
  t.Fatalf("corrupt waits were silently accepted on retry: %v",err)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE task_id=?`,healthyRun.TaskID).Scan(&active);err!=nil{t.Fatal(err)}
 if active!=1{t.Fatalf("successful wait reconciliation duplicated Attempt: %d",active)}
}
