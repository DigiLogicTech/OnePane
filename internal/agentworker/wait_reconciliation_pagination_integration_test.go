//go:build integration

package agentworker

import (
 "context"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A full page must not consume all recovery capacity, even when every row
// changes state. A malformed row is reported but cannot starve a later page.
// A single SQLite connection also makes cursor-lifetime regressions visible.
func TestWaitingWorkerReconciliationAcrossPagesWithOneDBConnection(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),120*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"paged-waits.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 var badRunID string
 const healthy=260
 for i:=0;i<healthy+1;i++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:"Recover queued local work without duplicate Attempts",
  })
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if err!=nil{t.Fatal(err)}
  run,result:=worker.startRun(ctx,ready)
  if result.Error!=""{t.Fatal(result.Error)}
  continuation:=json.RawMessage(`{}`)
  if i==0{
   badRunID=run.ID
   continuation=json.RawMessage(`{"model_wait":null}`)
  }
  if err:=worker.suspendForResource(ctx,run,continuation,
   "waiting for local model resources","route",
   map[string]any{"kind":"resource_wait"},nil);err!=nil{t.Fatal(err)}
 }
 err=worker.syncResumedRuns(ctx)
 if err==nil||!strings.Contains(err.Error(),badRunID){
  t.Fatalf("corrupt wait was not separately reported: %v",err)
 }
 var resumed,parked int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs r
  JOIN tasks t ON t.id=r.task_id JOIN task_attempts a ON a.id=r.attempt_id
  WHERE r.status='running' AND t.state='running' AND a.status='running'`).
  Scan(&resumed);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs r
  JOIN tasks t ON t.id=r.task_id JOIN task_attempts a ON a.id=r.attempt_id
  WHERE r.status='waiting' AND t.state='waiting_dependency' AND a.status='waiting'`).
  Scan(&parked);err!=nil{t.Fatal(err)}
 if resumed!=healthy||parked!=1{
  t.Fatalf("paged wake lost independent work: resumed=%d parked=%d",resumed,parked)
 }
 // Another tick must be idempotent: no duplicate Attempts or second wake.
 if err:=worker.syncResumedRuns(ctx);err==nil||
  !strings.Contains(err.Error(),badRunID){
  t.Fatalf("corrupt wait was silently skipped on repeat: %v",err)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).
  Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=healthy+1{
  t.Fatalf("resource reconciliation duplicated Attempts: %d",attempts)
 }
}
