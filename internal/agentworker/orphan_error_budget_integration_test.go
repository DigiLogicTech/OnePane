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

// Retain bounded diagnostics but traverse every orphan. Unknown effects stay
// blocked until a real operator repairs independently corrupt Attempts.
func TestOrphanRecoveryCapsErrorsAndPreservesSafeIndependentCommits(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"orphan-error-budget.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 const now int64=1700000000000
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
   id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 svc:=New(db.SQL(),db,clock.Real{},"local",task.NewService(db.SQL(),db,clock.Real{}),
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=svc.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 tx,err:=db.SQL().BeginTx(ctx,nil)
 if err!=nil{t.Fatal(err)}
 const damaged=42
 const healthy=3
 for i:=0;i<damaged+healthy;i++ {
  id:=fmt.Sprintf("task-%03d",i)
  if _,err:=tx.ExecContext(ctx,`INSERT INTO tasks(
    id,workspace_id,objective,state,completion_json,revision,created_at,updated_at)
    VALUES(?,'tenant','Recover known Task','running','{}',1,?,?)`,
   id,now+int64(i),now);err!=nil{_ =tx.Rollback();t.Fatal(err)}
  if _,err:=tx.ExecContext(ctx,`INSERT INTO task_attempts(
    id,task_id,attempt_number,worker_principal_id,status,metadata_json,started_at)
    VALUES(?,?,1,?,'running','{"agent_worker":"v1"}',?)`,
   "attempt-"+id,id,WorkerPrincipal,now);err!=nil{_ =tx.Rollback();t.Fatal(err)}
 }
 if err:=tx.Commit();err!=nil{t.Fatal(err)}
 // Reject only the first 42 attempts. Never advance their Task/Attempt
 // state on a synthetic DB failure, but still recover the last three.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_corrupt_orphans
  BEFORE UPDATE OF status ON task_attempts
  WHEN OLD.id<'attempt-task-042' AND NEW.status='interrupted'
  BEGIN SELECT RAISE(ABORT,'synthetic independent Attempt corruption'); END`);
  err!=nil{t.Fatal(err)}
 count,err:=svc.RecoverLostRuns(ctx)
 if count!=healthy||err==nil{t.Fatalf("orphan recovery count=%d err=%v",count,err)}
 msg:=err.Error()
 for _,must:=range []string{"task-000","task-031",
  "10 additional orphan Agent Attempt recovery failures require operator review"}{
  if !strings.Contains(msg,must){t.Fatalf("lost error summary %q: %s",must,msg)}
 }
 if strings.Contains(msg,"orphan Task task-032 Attempt"){
  t.Fatalf("omitted orphan identity exceeded bounded diagnostic array: %s",msg)
 }
 if len(msg)>16000{t.Fatalf("unbounded orphan recovery message: %d",len(msg))}
 var recovered,unresolved,audit int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE status='interrupted'`).Scan(&recovered);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE status='running'`).Scan(&unresolved);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='task.attempt_interrupted'`).Scan(&audit);err!=nil{t.Fatal(err)}
 if recovered!=healthy||unresolved!=damaged||audit!=healthy{
  t.Fatalf("recovered=%d unresolved=%d audit=%d",recovered,unresolved,audit)
 }
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_corrupt_orphans`);err!=nil{t.Fatal(err)}
 count,err=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=damaged{t.Fatalf("repaired orphan recovery=%d err=%v",count,err)}
 count,err=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=0{t.Fatalf("idempotent orphan recovery=%d err=%v",count,err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='task.attempt_interrupted'`).Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=healthy+damaged{t.Fatalf("repeated recovery changed audit count %d",audit)}
}
