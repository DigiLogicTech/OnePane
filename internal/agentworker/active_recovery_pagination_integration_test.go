//go:build integration

package agentworker

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// Hundreds of live model jobs may be interrupted by one host restart. Every
// uncertain Attempt must be blocked exactly once without exhausting old
// hardware or holding the single SQLite connection during recovery writes.
func TestRecoverLostRunsPagesEveryLiveAttemptWithOneSQLiteConnection(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"active-recovery.db"))
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
 worker:=New(db.SQL(),db,clk,"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 type running struct{taskID,attemptID,runID string}
 const total=260 // Cross the former unbounded read and one 256-row page.
 active:=make([]running,0,total)
 for n:=0;n<total;n++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("local queued model work %03d",n)})
  if err!=nil{t.Fatalf("create %d: %v",n,err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatalf("ready %d: %v",n,err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatalf("start %d: %s",n,res.Error)}
  active=append(active,running{created.ID,run.AttemptID,run.ID})
 }
 // Force 259 equal-timestamp rows across the 256-record page
 // boundary; the run ID must be the stable secondary cursor key.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
  SET started_at=? WHERE status='running'`,now);err!=nil{t.Fatal(err)}
 // Make one earliest Task independently unrecoverable, but leave its
 // Worker running. It must NOT suppress later recovery pages.
 corrupt:=active[0]
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
  SET started_at=1 WHERE id=?`,corrupt.runID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts SET status='succeeded'
  WHERE id=?`,corrupt.attemptID);err!=nil{t.Fatal(err)}
 recovered,err:=worker.RecoverLostRuns(ctx)
 if err==nil||recovered!=total-1{
  t.Fatalf("one corrupt live Attempt must not suppress %d safe recoveries: got=%d err=%v",
   total-1,recovered,err)
 }
 if errors.Is(err,context.DeadlineExceeded)||ctx.Err()!=nil{
  t.Fatalf("single-connection DB deadlocked during bounded recovery: %v",err)
 }
 var safeRuns,uncertainRuns,audits,outbox int
 for _,q:=range []struct{sql string;value *int}{
  {`SELECT COUNT(*) FROM agent_worker_runs WHERE status='interrupted'`,&safeRuns},
  {`SELECT COUNT(*) FROM agent_worker_runs WHERE status='running'`,&uncertainRuns},
  {`SELECT COUNT(*) FROM events WHERE event_type='task.attempt_interrupted'`,&audits},
  {`SELECT COUNT(*) FROM outbox_jobs WHERE job_type='task.recovery.required'`,&outbox},
 }{
  if err:=db.SQL().QueryRowContext(ctx,q.sql).Scan(q.value);err!=nil{t.Fatal(err)}
 }
 if safeRuns!=total-1||uncertainRuns!=1||audits!=total-1||outbox!=total-1{
  t.Fatalf("uncertain worker effect escaped recovery: interrupted=%d running=%d audit=%d outbox=%d",
   safeRuns,uncertainRuns,audits,outbox)
 }
 // Fixing the original corrupted Attempt must recover it independently.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts SET status='running'
  WHERE id=?`,corrupt.attemptID);err!=nil{t.Fatal(err)}
 recovered,err=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=1{t.Fatalf("repaired attempt recovery=%d err=%v",recovered,err)}
 recovered,err=worker.RecoverLostRuns(ctx)
 if err!=nil||recovered!=0{t.Fatalf("crash-recovery replay duplicated work: %d %v",recovered,err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM task_attempts WHERE status='interrupted'`).Scan(&safeRuns);
  err!=nil&&err!=sql.ErrNoRows{t.Fatal(err)}
 if safeRuns!=total{t.Fatalf("not every original Attempt interrupted exactly once: %d",safeRuns)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM events WHERE event_type='task.attempt_interrupted'`).Scan(&audits);err!=nil{t.Fatal(err)}
 if audits!=total{t.Fatalf("recovery audit not exactly-once: %d",audits)}
}
