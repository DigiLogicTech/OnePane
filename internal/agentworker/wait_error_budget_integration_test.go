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

// A bad large queue must not multiply diagnostic allocations on every Tick,
// nor hide an unrelated healthy Worker when more than 32 continuations fail.
func TestWaitingReconciliationCapsErrorsAndResumesHealthyAttempt(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"waiting-errors.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 const damaged=41
 for i:=0;i<damaged+1;i++ {
  created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",
   Objective:"Recover independently without expanding model authority"})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  continuation:=json.RawMessage(`{"model_wait":null}`)
  if i==damaged{continuation=json.RawMessage(`{}`)}
  if err:=worker.suspendForResource(ctx,run,continuation,
   "waiting for approved local capacity","route",
   map[string]any{"kind":"resource_wait"},nil);err!=nil{t.Fatal(err)}
 }
 // No assumption about randomly generated Worker IDs. Sort to identify
 // exactly which 32 failures are retained under the stable keyset order.
 rows,err:=db.SQL().QueryContext(ctx,`SELECT r.id FROM agent_worker_runs r
  WHERE r.status='waiting' AND r.continuation_json='{"model_wait":null}'
  ORDER BY r.id`)
 if err!=nil{t.Fatal(err)}
 var damagedIDs []string
 for rows.Next(){var id string;if err:=rows.Scan(&id);err!=nil{break};damagedIDs=append(damagedIDs,id)}
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil||closeErr!=nil{t.Fatalf("list damaged runs: %v / %v",err,closeErr)}
 if len(damagedIDs)!=damaged{t.Fatalf("damaged wait fixture count=%d",len(damagedIDs))}
 for attempt:=0;attempt<2;attempt++ {
  err=worker.syncResumedRuns(ctx)
  if err==nil{t.Fatalf("corrupt waits hidden at cycle %d",attempt)}
  msg:=err.Error()
  for _,id:=range damagedIDs[:maxWorkerRecoveryErrorDetails]{
   if !strings.Contains(msg,id){t.Fatalf("missing sampled damaged run %s: %s",id,msg)}
  }
  if strings.Contains(msg,damagedIDs[maxWorkerRecoveryErrorDetails]){
   t.Fatalf("overflow damaged run was retained beyond error bound: %s",msg)
  }
  if !strings.Contains(msg,"9 additional waiting Worker reconciliation failures require operator review"){
   t.Fatalf("missing complete overflow count: %s",msg)
  }
  if len(msg)>9000{t.Fatalf("unbounded recovery diagnostic size=%d",len(msg))}
 }
 var active,waiting,attempts int
 for _,tc:=range []struct{query string;dst *int}{
  {`SELECT COUNT(*) FROM agent_worker_runs WHERE status='running'`,&active},
  {`SELECT COUNT(*) FROM agent_worker_runs WHERE status='waiting'`,&waiting},
  {`SELECT COUNT(*) FROM task_attempts`,&attempts},
 }{
  if err:=db.SQL().QueryRowContext(ctx,tc.query).Scan(tc.dst);err!=nil{t.Fatal(err)}
 }
 if active!=1||waiting!=damaged||attempts!=damaged+1{
  t.Fatalf("healthy work lost or attempts duplicated: running=%d waiting=%d attempts=%d",
   active,waiting,attempts)
 }
}
