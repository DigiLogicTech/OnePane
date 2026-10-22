//go:build integration

package agentworker

import (
 "context"
 "fmt"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// In a large model queue, an interrupted daemon must assess ALL independent
// orphaned Agent attempts, not only the first 256. A corrupt Attempt cannot
// pin the keyset cursor or make healthy Tasks lose their recovery audit.
func TestOrphanRecoveryPagesBeyond256AndContinuesPastOneFailure(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"orphan-pagination.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=int64(1700000000000)
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 svc:=New(db.SQL(),db,clock.Real{},"local",task.NewService(db.SQL(),db,clock.Real{}),
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=svc.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 tx,err:=db.SQL().BeginTx(ctx,nil)
 if err!=nil{t.Fatal(err)}
 const total=260
 for n:=0;n<total;n++{
  id:=fmt.Sprintf("task-%03d",n)
  if _,err:=tx.ExecContext(ctx,`INSERT INTO tasks(
   id,workspace_id,objective,state,completion_json,revision,created_at,updated_at)
   VALUES(?,'tenant','Recover local work','running','{}',1,?,?)`,id,now+int64(n),now);
   err!=nil{_ =tx.Rollback();t.Fatal(err)}
  if _,err:=tx.ExecContext(ctx,`INSERT INTO task_attempts(
   id,task_id,attempt_number,worker_principal_id,status,metadata_json,started_at)
   VALUES(?,?,1,?,'running','{"agent_worker":"v1"}',?)`,
   "attempt-"+id,id,WorkerPrincipal,now);err!=nil{
   _ =tx.Rollback();t.Fatal(err)
  }
 }
 if err:=tx.Commit();err!=nil{t.Fatal(err)}
 // The first Attempt is deliberately unrecoverable until operator repair.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_first_orphan
  BEFORE UPDATE OF status ON task_attempts
  WHEN OLD.id='attempt-task-000' AND NEW.status='interrupted'
  BEGIN SELECT RAISE(ABORT,'synthetic first Attempt corruption'); END`);
  err!=nil{t.Fatal(err)}
 count,err:=svc.RecoverLostRuns(ctx)
 if err==nil||count!=total-1{
  t.Fatalf("one bad orphan must not strand 259 later Attempts: count=%d err=%v",count,err)
 }
 for _,q:=range []struct{sql string;want int}{
  {`SELECT count(*) FROM tasks WHERE state='blocked'`,total-1},
  {`SELECT count(*) FROM task_attempts WHERE status='interrupted'`,total-1},
  {`SELECT count(*) FROM outbox_jobs WHERE job_type='task.recovery.required'`,total-1},
  {`SELECT count(*) FROM events WHERE event_type='task.attempt_interrupted'`,total-1},
 }{
  var got int
  if err:=db.SQL().QueryRowContext(ctx,q.sql).Scan(&got);err!=nil{t.Fatal(err)}
  if got!=q.want{t.Fatalf("%s: got=%d want=%d",q.sql,got,q.want)}
 }
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_first_orphan`);err!=nil{t.Fatal(err)}
 count,err=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=1{t.Fatalf("operator-repaired original orphan: count=%d err=%v",count,err)}
 count,err=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=0{t.Fatalf("second recovery replayed completed effects: count=%d err=%v",count,err)}
 var events int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT count(*) FROM events
  WHERE event_type='task.attempt_interrupted'`).Scan(&events);err!=nil{t.Fatal(err)}
 if events!=total{t.Fatalf("each Attempt needs exactly one recovery audit: %d",events)}
}
