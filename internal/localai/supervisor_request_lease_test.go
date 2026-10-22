package localai

import(
 "context"
 "database/sql"
 "testing"
 "sync"

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


// Twenty-four concurrent workers holding one local model must not make the
// runtime appear idle when only some have returned their GPU/CPU leases.
func TestConcurrentLocalInferenceLeasesWaitForLastWorker(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 ctx:=context.Background()
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(
   deployment_id TEXT PRIMARY KEY,status TEXT,last_seen_at INTEGER,
   updated_at INTEGER,revision INTEGER NOT NULL)`,
  `INSERT INTO local_runtime_instances VALUES('shared-local','healthy',0,0,1)`,
 }{
  if _,err=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 svc:=&RuntimeSupervisor{db:db,clock:clock.Real{},activeRequests:map[string]int{}}
 const workers=24
 admitted:=make(chan error,workers)
 released:=make(chan error,workers)
 finish:=make(chan struct{})
 var wg sync.WaitGroup
 for i:=0;i<workers;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   err:=svc.SetBusy(ctx,"shared-local",true)
   admitted<-err
   if err!=nil{return}
   <-finish
   released<-svc.Release(ctx,"shared-local")
  }()
 }
 failedAdmission:=false
 for i:=0;i<workers;i++{
  if err:=<-admitted;err!=nil{
   failedAdmission=true
  }
 }
 count:=svc.activeRequests["shared-local"]
 var state string
 stateErr:=db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances
   WHERE deployment_id='shared-local'`).Scan(&state)
 close(finish)
 wg.Wait()
 close(released)
 failedRelease:=false
 nReleased:=0
 for err:=range released{
  nReleased++
  if err!=nil{failedRelease=true}
 }
 if failedAdmission||stateErr!=nil||state!="busy"||count!=workers||
  failedRelease||nReleased!=workers||len(svc.activeRequests)!=0{
  t.Fatalf("shared worker lease accounting failed: admission=%v before=%d state=%s stateErr=%v release=%v count=%d remaining=%v",
   failedAdmission,count,state,stateErr,failedRelease,nReleased,svc.activeRequests)
 }
 if err=db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances
   WHERE deployment_id='shared-local'`).Scan(&state);err!=nil||state!="healthy"{
  t.Fatalf("last worker release did not return model to idle: %s %v",state,err)
 }
}
