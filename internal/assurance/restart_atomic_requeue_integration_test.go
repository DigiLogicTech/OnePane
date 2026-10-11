//go:build integration

package assurance

import (
 "context"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// A previous two-commit restart path could strand a deterministic assurance
// run as INTERRUPTED after a crash between status updates. Recovery must
// requeue those rows atomically with currently RUNNING deterministic jobs,
// without modifying human waits or creating duplicate recovery transitions.
func TestAssuranceRestartRequeueIsAtomicAndRepairsStrandedInterrupted(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"recovery-atomic.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 for _,v:=range []struct{id string;stamp int64}{
  {"verification-running",now-3},
  {"verification-interrupted",now-2},
  {"verification-human",now-1},
 }{
  if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
   id,workspace_id,subject_ref,required_level,status,spec_json,started_at,revision)
   VALUES(?,'tenant','standalone-deterministic','V1','pending','{}',?,1)`,
   v.id,v.stamp);err!=nil{t.Fatal(err)}
 }
 for _,r:=range []struct{id,vid,status string}{
  {"run-running","verification-running","running"},
  {"run-stranded","verification-interrupted","interrupted"},
  {"run-human","verification-human","waiting_human"},
 }{
  if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO assurance_runs(
   id,verification_id,workspace_id,status,required_level,result_json,revision,
   started_at,updated_at)
   VALUES(?,?,'tenant',?,'V1','{}',1,?,?)`,
   r.id,r.vid,r.status,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=New(db.SQL(),db,clock.Real{},nil,nil,nil,nil,nil)
 // Abort on the second row's transition. A single SQL statement must
 // guarantee rollback of BOTH state changes, even if a row was visited.
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_stranded_requeue
  BEFORE UPDATE OF status ON assurance_runs
  WHEN OLD.id='run-stranded' AND NEW.status='queued'
  BEGIN SELECT RAISE(ABORT,'inject failed stranded recovery'); END`);err!=nil{t.Fatal(err)}
 count,err:=svc.RecoverInterrupted(ctx)
 if err==nil||count!=0{t.Fatalf("recovery failure must not claim partial success: %d %v",count,err)}
 for _,r:=range []struct{id,status string}{
  {"run-running","running"},{"run-stranded","interrupted"},{"run-human","waiting_human"},
 }{
  var status string
  var revision int64
  if err:=db.SQL().QueryRowContext(ctx,`SELECT status,revision FROM assurance_runs
   WHERE id=?`,r.id).Scan(&status,&revision);err!=nil{t.Fatal(err)}
  if status!=r.status||revision!=1{
   t.Fatalf("partial recovery after journal failure: %s status=%s revision=%d",r.id,status,revision)
  }
 }
 if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_stranded_requeue`);err!=nil{t.Fatal(err)}
 count,err=svc.RecoverInterrupted(ctx)
 if err!=nil||count!=2{t.Fatalf("failed to recover both deterministic records: %d %v",count,err)}
 for _,r:=range []struct{id,status string;revision int64}{
  {"run-running","queued",2},{"run-stranded","queued",2},{"run-human","waiting_human",1},
 }{
  var status string
  var revision int64
  if err:=db.SQL().QueryRowContext(ctx,`SELECT status,revision FROM assurance_runs
   WHERE id=?`,r.id).Scan(&status,&revision);err!=nil{t.Fatal(err)}
  if status!=r.status||revision!=r.revision{
   t.Fatalf("recovery mutated unrelated status/revision for %s: %s/%d",r.id,status,revision)
  }
 }
 count,err=svc.RecoverInterrupted(ctx)
 if err!=nil||count!=0{t.Fatalf("repeat restart duplicated deterministic requeue: %d %v",count,err)}
}
