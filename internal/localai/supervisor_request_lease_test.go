package localai

import(
 "context"
 "database/sql"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

func TestLocalRuntimeRequestLeasesStayCoherentWithSQLite(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 ctx:=context.Background()
 for _,query:=range []string{
  `CREATE TABLE local_runtime_instances(
    deployment_id TEXT PRIMARY KEY,status TEXT,last_seen_at INTEGER,
    updated_at INTEGER,revision INTEGER NOT NULL)`,
  `INSERT INTO local_runtime_instances VALUES('model','starting',0,0,1)`,
 }{
  if _,err=db.ExecContext(ctx,query);err!=nil{t.Fatal(err)}
 }
 sup:=&RuntimeSupervisor{
  db:db,clock:clock.Real{},activeRequests:map[string]int{},
 }
 if err:=sup.SetBusy(ctx,"model",true);err==nil||sup.activeRequests["model"]!=0{
  t.Fatalf("starting model was incorrectly acquired: %v map=%v",err,sup.activeRequests)
 }
 if _,err=db.ExecContext(ctx,`UPDATE local_runtime_instances SET status='healthy'`);err!=nil{t.Fatal(err)}
 if err:=sup.SetBusy(ctx,"model",true);err!=nil||sup.activeRequests["model"]!=1{
  t.Fatalf("first healthy lease not admitted: %v count=%d",err,sup.activeRequests["model"])
 }
 if err:=sup.SetBusy(ctx,"model",true);err!=nil||sup.activeRequests["model"]!=2{
  t.Fatalf("second shared lease lost: %v count=%d",err,sup.activeRequests["model"])
 }
 if err:=sup.SetBusy(ctx,"model",false);err!=nil||sup.activeRequests["model"]!=1{
  t.Fatalf("first request release interrupted remaining lease: %v count=%d",err,sup.activeRequests["model"])
 }
 var state string
 if err=db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances WHERE deployment_id='model'`).Scan(&state);
  err!=nil||state!="busy"{
  t.Fatalf("still-active runtime prematurely marked idle: %q %v",state,err)
 }
 if err:=sup.SetBusy(ctx,"model",false);err!=nil||len(sup.activeRequests)!=0{
  t.Fatalf("final release did not restore availability: %v %v",err,sup.activeRequests)
 }
 if err=db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances WHERE deployment_id='model'`).Scan(&state);
  err!=nil||state!="healthy"{
  t.Fatalf("final lease did not restore healthy state: %q %v",state,err)
 }
 if err:=sup.SetBusy(ctx,"model",false);err!=nil||len(sup.activeRequests)!=0{
  t.Fatalf("replayed release changed lease state: %v %v",err,sup.activeRequests)
 }
 if err:=sup.SetBusy(ctx,"model",true);err!=nil{t.Fatal(err)}
 // Defer-cleanup after a request cancellation must still release its
 // SQLite and in-memory active-lease record using a bounded clean context.
 cancelled,cancel:=context.WithCancel(ctx)
 cancel()
 if err:=sup.Release(cancelled,"model");err!=nil||sup.activeRequests["model"]!=0{
  t.Fatalf("cancelled task stranded otherwise healthy runtime lease: %v %+v",err,sup.activeRequests)
 }
 if err:=sup.SetBusy(ctx,"model",true);err!=nil{t.Fatal(err)}
 if _,err=db.ExecContext(ctx,`DROP TABLE local_runtime_instances`);err!=nil{t.Fatal(err)}
 if err:=sup.SetBusy(ctx,"model",false);err==nil||sup.activeRequests["model"]!=1{
  t.Fatalf("failed database release discarded live request protection: %v %v",err,sup.activeRequests)
 }
 if err:=sup.SetBusy(ctx,"model",true);err==nil||sup.activeRequests["model"]!=1{
  t.Fatalf("failed database acquire created ghost lease: %v %v",err,sup.activeRequests)
 }
}
