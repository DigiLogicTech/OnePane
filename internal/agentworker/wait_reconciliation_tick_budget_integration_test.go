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

// A large corrupt waiting queue must not consume unlimited CPU per Tick or
// block a healthy continuation beyond the 128-row inspection budget. The
// original Worker and Attempt must survive the wake, with no new authority.
func TestWaitingQueueBoundedFairRotationWithSingleSQLiteConnection(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),120*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"wait-tick-budget.db"))
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
 const broken=131
 for n:=0;n<broken+1;n++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:"Recover bounded local waiting Task"})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  if err:=worker.suspendForResource(ctx,run,json.RawMessage(`{}`),
   "awaiting approved local runtime","route",
   map[string]any{"kind":"resource_wait"},nil);err!=nil{t.Fatal(err)}
 }
 // UUID order rather than creation order drives this queue. Leave the
 // last ordered Worker healthy, and mark the earlier 131 as malformed.
 var healthyRunID,healthyTaskID,healthyAttemptID string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT id,task_id,attempt_id
  FROM agent_worker_runs WHERE status='waiting'
  ORDER BY id DESC LIMIT 1`).Scan(&healthyRunID,&healthyTaskID,
  &healthyAttemptID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
  SET continuation_json='{"model_wait":null}'
  WHERE status='waiting' AND id<>?`,healthyRunID);err!=nil{t.Fatal(err)}
 rows,err:=db.SQL().QueryContext(ctx,`SELECT id FROM agent_worker_runs
  WHERE status='waiting' AND id<>? ORDER BY id`,healthyRunID)
 if err!=nil{t.Fatal(err)}
 badIDs:=make([]string,0,broken)
 for rows.Next(){
  var id string
  if err=rows.Scan(&id);err!=nil{break}
  badIDs=append(badIDs,id)
 }
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil||closeErr!=nil{t.Fatalf("load ordered damaged runs: %v / %v",err,closeErr)}
 if len(badIDs)!=broken{t.Fatalf("bad fixture count %d",len(badIDs))}
 first:=worker.syncResumedRuns(ctx)
 if first==nil||!strings.Contains(first.Error(),badIDs[0])||
  !strings.Contains(first.Error(),"96 additional waiting Worker reconciliation failures require operator review"){
  t.Fatalf("initial Tick did not inspect exactly 128 damaged rows: %v",first)
 }
 if strings.Contains(first.Error(),badIDs[128]){
  t.Fatalf("first Tick exceeded 128-candidate budget")
 }
 var healthyStatus string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs
  WHERE id=?`,healthyRunID).Scan(&healthyStatus);err!=nil{t.Fatal(err)}
 if healthyStatus!="waiting"{
  t.Fatalf("later healthy Worker was reached before fair cursor: %s",healthyStatus)
 }
 second:=worker.syncResumedRuns(ctx)
 if second==nil||!strings.Contains(second.Error(),badIDs[130])||
  strings.Contains(second.Error(),"additional waiting Worker reconciliation failures"){
  t.Fatalf("second Tick failed to reach end of damaged queue: %v",second)
 }
 var attemptState,taskState string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs
  WHERE id=?`,healthyRunID).Scan(&healthyStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM task_attempts
  WHERE id=?`,healthyAttemptID).Scan(&attemptState);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT state FROM tasks
  WHERE id=?`,healthyTaskID).Scan(&taskState);err!=nil{t.Fatal(err)}
 if healthyStatus!="running"||attemptState!="running"||taskState!="running"{
  t.Fatalf("healthy Task/Attempt did not wake atomically: Worker=%s Attempt=%s Task=%s",
   healthyStatus,attemptState,taskState)
 }
 var countAttempts,waits int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).Scan(&countAttempts);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs
  WHERE status='waiting'`).Scan(&waits);err!=nil{t.Fatal(err)}
 if countAttempts!=broken+1||waits!=broken{
  t.Fatalf("new Attempt or missing damaged Worker: attempts=%d waiting=%d",countAttempts,waits)
 }
 // One more call must revisit the earliest damaged Workers for repair,
 // without waking the same healthy run or changing its original Attempt.
 third:=worker.syncResumedRuns(ctx)
 if third==nil||!strings.Contains(third.Error(),badIDs[0]){
  t.Fatalf("waiting queue wrap hid unresolved diagnostics: %v",third)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE task_id=?`,healthyTaskID).Scan(&countAttempts);err!=nil{t.Fatal(err)}
 if countAttempts!=1{t.Fatalf("healthy wake created duplicate Attempts: %d",countAttempts)}
}
