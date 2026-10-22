//go:build integration

package agentworker

import (
 "context"
 "fmt"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// Many unresolved live attempts must not create an unbounded startup error
// report or cause other Tasks to lose their transactional recovery markers.
func TestActiveWorkerRecoveryCapsErrorsWithoutDroppingIndependentFailures(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"running-error-budget.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 tasks:=task.NewService(db.SQL(),db,clk)
 worker:=New(db.SQL(),db,clk,"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 const damaged=42
 const healthy=3
 corruptRunIDs:=make([]string,0,damaged)
 corruptAttemptIDs:=make([]string,0,damaged)
 for n:=0;n<damaged+healthy;n++ {
  created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",
   Objective:fmt.Sprintf("model recovery %03d",n)})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  // Strictly ordered timestamps prove the first 32 identities sampled
  // even if the generated worker IDs are not lexicographically monotonic.
  if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
   SET started_at=? WHERE id=?`,int64(n+1),run.ID);err!=nil{t.Fatal(err)}
  if n<damaged {
   corruptRunIDs=append(corruptRunIDs,run.ID)
   corruptAttemptIDs=append(corruptAttemptIDs,run.AttemptID)
   if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts
    SET status='succeeded' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
  }
 }
 recovered,err:=worker.RecoverLostRuns(ctx)
 if recovered!=healthy||err==nil{t.Fatalf("recovered=%d err=%v",recovered,err)}
 report:=err.Error()
 for _,runID:=range corruptRunIDs[:maxWorkerRecoveryErrorDetails] {
  if !strings.Contains(report,runID){t.Fatalf("missing sampled run %s",runID)}
 }
 if strings.Contains(report,corruptRunIDs[maxWorkerRecoveryErrorDetails]){
  t.Fatalf("error retention exceeded 32 sampled running Workers")
 }
 if !strings.Contains(report,"10 additional lost running Worker recoveries require operator review"){
  t.Fatalf("missing overflow count: %s",report)
 }
 if len(report)>16000{t.Fatalf("unbounded startup failure report: %d",len(report))}
 var interrupted,running,audits int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs
  WHERE status='interrupted'`).Scan(&interrupted);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs
  WHERE status='running'`).Scan(&running);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='task.attempt_interrupted'`).Scan(&audits);err!=nil{t.Fatal(err)}
 if interrupted!=healthy||running!=damaged||audits!=healthy {
  t.Fatalf("partial recovery: interrupted=%d running=%d audits=%d",
   interrupted,running,audits)
 }
 for _,attemptID:=range corruptAttemptIDs {
  if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts
   SET status='running' WHERE id=?`,attemptID);err!=nil{t.Fatal(err)}
 }
 recovered,err=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=damaged{t.Fatalf("repaired recovery=%d err=%v",recovered,err)}
 recovered,err=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0{t.Fatalf("idempotent recovery=%d err=%v",recovered,err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='task.attempt_interrupted'`).Scan(&audits);err!=nil{t.Fatal(err)}
 if audits!=damaged+healthy{t.Fatalf("failed exact-once audit: %d",audits)}
}
