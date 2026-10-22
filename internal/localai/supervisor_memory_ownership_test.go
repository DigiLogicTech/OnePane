package localai

import(
 "context"
 "database/sql"
 "fmt"
 "strings"
 "testing"

 _ "modernc.org/sqlite"
)

func TestManagedMemoryAdmissionRejectsStaleLiveProcessAcrossBackends(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 proc:=&unrecordedProcessBackend{alive:true}
 s:=&RuntimeSupervisor{db:db,processes:proc}
 for _,status:=range []string{"starting","draining","orphaned","failed","stopped"}{
  t.Run(status,func(t *testing.T){
   if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
   if _,err=db.Exec(`INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node','old-model',?,4321)`,status);err!=nil{t.Fatal(err)}
   err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","new-model")
   if err==nil||!strings.Contains(err.Error(),"live process"){
    t.Fatalf("stale %s process permitted fresh model capacity: %v",status,err)
   }
   if proc.signalCalls!=0{t.Fatal("read-only memory admission sent a termination signal")}
  })
 }
}
func TestManagedMemoryAdmissionFailClosedForUnrecordedUncertainPID(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 s:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{}}
 for _,status:=range []string{"starting","draining","orphaned","failed"}{
  if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
  if _,err=db.Exec(`INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node','old-model',?,NULL)`,status);err!=nil{t.Fatal(err)}
  if err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","new-model");err==nil{
   t.Fatalf("uncertain %s old process without PID allowed another launch",status)
  }
 }
}
func TestManagedMemoryAdmissionAllowsDeadOrIndependentNodeAndTarget(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node','old-dead','failed',4321)`,
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('different-node','still-live','failed',4444)`,
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node','target','starting',NULL)`,
 }{if _,err=db.Exec(q);err!=nil{t.Fatal(err)}}
 s:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{alive:false}}
 if err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","target");err!=nil{
  t.Fatalf("dead unrelated or target process should not prevent admission: %v",err)
 }
}
func TestManagedMemoryAdmissionBoundsHugeAbnormalRegistry(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 tx,err:=db.Begin();if err!=nil{t.Fatal(err)}
 for i:=0;i<513;i++{
  if _,err=tx.Exec(`INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node',?,'failed',NULL)`,fmt.Sprintf("old-%04d",i));err!=nil{t.Fatal(err)}
 }
 if err=tx.Commit();err!=nil{t.Fatal(err)}
 s:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{}}
 if err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","new");err==nil||
  !strings.Contains(err.Error(),"bounded"){
  t.Fatalf("oversized unresolved process ledger was silently truncated: %v",err)
 }
}

func TestManagedMemoryAdmissionRejectsCorruptPIDEvenWhenMarkedStopped(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 s:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{alive:false}}
 for _,pid:=range []int64{-4321,0,1}{
  for _,status:=range []string{"failed","stopped"}{
   if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
   if _,err=db.Exec(`INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid) VALUES('node','old',?,?)`,status,pid);err!=nil{t.Fatal(err)}
   if err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","new");err==nil||!strings.Contains(err.Error(),"invalid persisted PID"){
    t.Fatalf("status=%s invalid PID %d freed model budget: %v",status,pid,err)
   }
  }
 }
 // A confirmed never-launched failure records NULL, not 0 or 1.
 if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
 if _,err=db.Exec(`INSERT INTO local_runtime_instances(node_id,deployment_id,status,pid,failure_reason) VALUES('node','old','failed',NULL,?)`,managedNoChildFailureReason);err!=nil{t.Fatal(err)}
 if err:=s.refuseUnreconciledMemoryOwners(context.Background(),"node","new");err!=nil{
  t.Fatalf("valid failed prelaunch without PID wrongly blocks budget: %v",err)
 }
}

// The exact pre-spawn failure marker is required before a failed row with
// no PID can be treated as memory-free. A label alone is insufficient if
// launch timestamps indicate that an engine may have been created.
func TestManagedMemoryAdmissionRequiresDurableNoChildFailureProof(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
  failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 svc:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{}}
 for _,tc:=range []struct{name,reason string;started any;allow bool}{
  {"no reason","",nil,false},
  {"unrelated failure","health probe timed out",nil,false},
  {"forged prelaunch with a start time",managedNoChildFailureReason,int64(42),false},
  {"guarded no-child failure",managedNoChildFailureReason,nil,true},
 }{
  t.Run(tc.name,func(t *testing.T){
   if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
   if _,err=db.Exec(`INSERT INTO local_runtime_instances
    (node_id,deployment_id,status,pid,failure_reason,started_at)
    VALUES('node','old','failed',NULL,?,?)`,tc.reason,tc.started);err!=nil{t.Fatal(err)}
   err:=svc.refuseUnreconciledMemoryOwners(context.Background(),"node","new")
   if (err==nil)!=tc.allow{t.Fatalf("failed/NULL-PID reason=%q started=%v unexpectedly eligible=%v error=%v",tc.reason,tc.started,err==nil,err)}
  })
 }
}

// A stopped/NULL-PID row without the exact pre-spawn proof can still have
// an untracked live engine. Do not free memory merely from status=stopped.
func TestManagedMemoryAdmissionRejectsUnprovenStoppedNullPID(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err:=db.Exec(`CREATE TABLE local_runtime_instances(
 node_id TEXT,deployment_id TEXT,status TEXT,pid INTEGER,
 failure_reason TEXT,started_at INTEGER)`);err!=nil{t.Fatal(err)}
 svc:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{alive:false}}
 for _,tc:=range []struct{name string;reason any;started any;allow bool}{
  {"unknown legacy stopped",nil,nil,false},
  {"unrelated stop marker","operator requested stop",nil,false},
  {"prelaunch marker inconsistent with child timestamp",managedNoChildFailureReason,int64(123),false},
  {"proven no-child stopped",managedNoChildFailureReason,nil,true},
 }{
  t.Run(tc.name,func(t *testing.T){
   if _,err=db.Exec(`DELETE FROM local_runtime_instances`);err!=nil{t.Fatal(err)}
   if _,err=db.Exec(`INSERT INTO local_runtime_instances(
    node_id,deployment_id,status,pid,failure_reason,started_at)
    VALUES('node','old','stopped',NULL,?,?)`,tc.reason,tc.started);err!=nil{t.Fatal(err)}
   e:=svc.refuseUnreconciledMemoryOwners(context.Background(),"node","new")
   if (e==nil)!=tc.allow{t.Fatalf("stopped/NULL-PID reason=%v started=%v allowed=%t err=%v",tc.reason,tc.started,e==nil,e)}
  })
 }
}
