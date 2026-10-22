package localai

import (
 "context"
 "database/sql"
 "strings"
 "testing"
)

// Missing PID in a recovered/failed-to-persist launch is NOT evidence that
// the original child exited; a Stop that rewrites stopped releases a
// reservation and could make another local model overcommit the host.
func TestManagedStopRetainsUnknownProcessOwnershipWithoutPID(t *testing.T) {
 for _,status:=range []RuntimeInstanceStatus{RuntimeStarting,RuntimeDraining,RuntimeOrphaned} {
  for _,pid:=range []int{0,1} {
   t.Run(string(status)+"/pid"+string(rune('0'+pid)),func(t *testing.T){
    svc,db:=stopLeaseFixture(t,string(status))
    if pid!=0 {
     if _,err:=db.Exec(`UPDATE local_runtime_instances SET pid=? WHERE id='inst'`,pid);err!=nil{t.Fatal(err)}
    }
    err:=svc.Stop(context.Background(),"model")
    if err==nil||!strings.Contains(err.Error(),"operator reconciliation"){
     t.Fatalf("unsafe Stop accepted uncertain %s pid=%d: %v",status,pid,err)
    }
    var got, residency string
    var rev int64
    var rawPID sql.NullInt64
    if err:=db.QueryRow(`SELECT status,revision,pid FROM local_runtime_instances WHERE id='inst'`).Scan(&got,&rev,&rawPID);err!=nil{t.Fatal(err)}
    if err:=db.QueryRow(`SELECT residency_state FROM model_deployments WHERE id='model'`).Scan(&residency);err!=nil{t.Fatal(err)}
    if got!=string(status)||rev!=1||residency!="resident" {
     t.Fatalf("uncertain model ownership was rewritten: %s rev=%d residency=%s pid=%v",got,rev,residency,rawPID)
    }
   })
  }
 }
}

func TestManagedShutdownCannotSilentlyStopUnknownPIDStartingOrDrain(t *testing.T) {
 for _,status:=range []string{"starting","draining"} {
  t.Run(status,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,status)
   err:=svc.Shutdown(context.Background())
   if err==nil||!strings.Contains(err.Error(),"operator reconciliation"){
    t.Fatalf("daemon shutdown silently discarded uncertain %s child: %v",status,err)
   }
   var saved,residency string
   var rev int
   if err:=db.QueryRow(`SELECT status,revision FROM local_runtime_instances WHERE id='inst'`).Scan(&saved,&rev);err!=nil{t.Fatal(err)}
   if err:=db.QueryRow(`SELECT residency_state FROM model_deployments WHERE id='model'`).Scan(&residency);err!=nil{t.Fatal(err)}
   if saved!=status||rev!=1||residency!="resident" {
    t.Fatalf("shutdown erased unknown engine: %s rev=%d residency=%s",saved,rev,residency)
   }
  })
 }
}

func TestConfirmedPrelaunchFailureWithNoPIDStillAllowsExplicitStop(t *testing.T) {
 svc,db:=stopLeaseFixture(t,"failed")
 if _,err:=db.Exec(`UPDATE local_runtime_instances SET failure_reason='managed runtime launch cancelled or could not create child' WHERE id='inst'`);err!=nil{t.Fatal(err)}
 if err:=svc.Stop(context.Background(),"model");err!=nil{
  t.Fatalf("confirmed no-child failure unnecessarily blocked: %v",err)
 }
 var got string
 if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&got);err!=nil{t.Fatal(err)}
 if got!="stopped"{t.Fatalf("confirmed no-child failed model was not stopped: %s",got)}
}
