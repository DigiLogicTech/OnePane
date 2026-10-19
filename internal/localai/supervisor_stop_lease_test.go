package localai

import (
 "context"
 "database/sql"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

func stopLeaseFixture(t *testing.T,initialStatus string)(*RuntimeSupervisor,*sql.DB){
 t.Helper()
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 db.SetMaxOpenConns(1)
 t.Cleanup(func(){_ = db.Close()})
 for _,query:=range []string{
  `CREATE TABLE local_runtime_instances(
   id TEXT PRIMARY KEY,node_id TEXT,deployment_id TEXT UNIQUE,
   runtime_id TEXT,managed_model_id TEXT,bind_address TEXT,
   port INTEGER,pid INTEGER,status TEXT,process_fingerprint TEXT,
   launch_json TEXT,health_json TEXT,started_at INTEGER,last_seen_at INTEGER,
   stopped_at INTEGER,failure_reason TEXT,revision INTEGER,
   created_at INTEGER,updated_at INTEGER)`,
  `CREATE TABLE model_deployments(
   id TEXT PRIMARY KEY,runtime_name TEXT,residency_state TEXT,
   updated_at INTEGER,revision INTEGER)`,
  `INSERT INTO local_runtime_instances
   (id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,
    port,pid,status,process_fingerprint,launch_json,health_json,revision,
    created_at,updated_at)
   VALUES ('inst','node','model','runtime','weights','127.0.0.1',
    11333,NULL,'`+initialStatus+`','identity','{}','{}',1,1,1)`,
  `INSERT INTO model_deployments(id,runtime_name,residency_state,updated_at,revision)
   VALUES ('model','llamacpp@cpu','resident',1,1)`,
 }{
  if _,err:=db.Exec(query);err!=nil{t.Fatal(err)}
 }
 return &RuntimeSupervisor{db:db,clock:clock.Real{},
  processes:NewOSProcessBackend(),activeRequests:map[string]int{}},db
}

func TestManagedStopRefusesActiveRequestWithoutChangingDatabase(t *testing.T){
 svc,db:=stopLeaseFixture(t,"busy")
 svc.activeRequests["model"]=2
 err:=svc.Stop(context.Background(),"model")
 if err==nil||!strings.Contains(err.Error(),"active inference"){
  t.Fatalf("active model was stopped or wrong error: %v",err)
 }
 var status string
 var revision int64
 if err:=db.QueryRow(`SELECT status,revision FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&revision);err!=nil{t.Fatal(err)}
 if status!="busy"||revision!=1||svc.activeRequests["model"]!=2{
  t.Fatalf("active model changed without consent: status=%s rev=%d lease=%v",status,revision,svc.activeRequests)
 }
}
func TestManagedStopRefusesUnknownPersistedBusyAfterRestart(t *testing.T){
 svc,db:=stopLeaseFixture(t,"busy")
 err:=svc.Stop(context.Background(),"model")
 if err==nil||!strings.Contains(err.Error(),"persisted busy"){
  t.Fatalf("unknown busy after restart was considered safely idle: %v",err)
 }
 var status string
 if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&status);err!=nil{t.Fatal(err)}
 if status!="busy"{t.Fatalf("unknown process status rewritten: %s",status)}
}
func TestManagedStopSucceedsAfterFinalActiveRequestReleases(t *testing.T){
 svc,db:=stopLeaseFixture(t,"busy")
 svc.activeRequests["model"]=1
 if err:=svc.Release(context.Background(),"model");err!=nil{t.Fatalf("release: %v",err)}
 if err:=svc.Stop(context.Background(),"model");err!=nil{t.Fatalf("clean stopped model refused: %v",err)}
 var runtime,residency string
 if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&runtime);err!=nil{t.Fatal(err)}
 if err:=db.QueryRow(`SELECT residency_state FROM model_deployments WHERE id='model'`).Scan(&residency);err!=nil{t.Fatal(err)}
 if runtime!="stopped"||residency!="stopped"||len(svc.activeRequests)!=0{
  t.Fatalf("runtime was not reconciled after final lease: %s %s %v",runtime,residency,svc.activeRequests)
 }
}
func TestManagedStopWaitsForInFlightLeaseAdmission(t *testing.T){
 svc,db:=stopLeaseFixture(t,"healthy")
 svc.activityMu.Lock()
 begun:=make(chan struct{})
 result:=make(chan error,1)
 go func(){
  close(begun)
  result<-svc.Stop(context.Background(),"model")
 }()
 <-begun
 select{
 case err:=<-result:
  svc.activityMu.Unlock()
  t.Fatalf("stop bypassed locked lease admission: %v",err)
 case <-time.After(30*time.Millisecond):
 }
 svc.activeRequests["model"]=1
 svc.activityMu.Unlock()
 err:=<-result
 if err==nil||!strings.Contains(err.Error(),"active inference"){
  t.Fatalf("stop did not honour pending lease: %v",err)
 }
 var status string
 if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&status);err!=nil{t.Fatal(err)}
 if status!="healthy"{t.Fatalf("stop marked active runtime draining: %s",status)}
}
