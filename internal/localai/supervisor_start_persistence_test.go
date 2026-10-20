package localai

import (
 "context"
 "database/sql"
 "strings"
 "errors"
 "os"
 "syscall"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
)

type unrecordedProcessBackend struct{
 alive bool
 signalCalls int
 onAlive func(int)
}
func (p *unrecordedProcessBackend) Start(context.Context,LaunchSpec)(int,error){
 return 0,errors.New("unexpected process start during recovery test")
}
func (p *unrecordedProcessBackend) Alive(pid int)bool{
 if p.onAlive!=nil{p.onAlive(pid)}
 return p.alive
}
func (p *unrecordedProcessBackend) Signal(_ int,_ os.Signal)error{
 p.signalCalls++
 p.alive=false
 return nil
}
var _ ProcessBackend=(*unrecordedProcessBackend)(nil)
var _ =syscall.SIGTERM

func TestFailedModelPIDPersistenceUsesUncancelledRecoveryToKeepOrphan(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 proc:=&unrecordedProcessBackend{alive:true}
 svc.processes=proc
 svc.clock=clock.Real{}
 parent,cancel:=context.WithCancel(context.Background())
 cancel()
 original:=parent.Err()
 err:=svc.reconcileUnrecordedLaunch(parent,RuntimeInstance{ID:"inst",Revision:1,ProcessFingerprint:"identity",DeploymentID:"model",Port:11333},4321,original)
 if !errors.Is(err,original){t.Fatalf("original cancelled-launch error lost: %v",err)}
 var status,reason string
 var pid int
 if scanErr:=db.QueryRow(`SELECT status,pid,failure_reason FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&pid,&reason);scanErr!=nil{t.Fatal(scanErr)}
 if status!="orphaned"||pid!=4321||reason==""{
  t.Fatalf("unknown live model lost durable PID: status=%s pid=%d reason=%s",status,pid,reason)
 }
 if proc.signalCalls!=0{t.Fatal("unverified process was signalled by PID alone")}
}
func TestFailedModelPIDPersistenceRecordsConfirmedExitedChild(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 proc:=&unrecordedProcessBackend{alive:false}
 svc.processes=proc
 cause:=errors.New("sqlite write refused")
 err:=svc.reconcileUnrecordedLaunch(context.Background(),RuntimeInstance{ID:"inst",Revision:1,ProcessFingerprint:"identity",DeploymentID:"model"},9876,cause)
 if !errors.Is(err,cause){t.Fatalf("lost SQLite error: %v",err)}
 var status string
 var pid int
 if scanErr:=db.QueryRow(`SELECT status,pid FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&pid);scanErr!=nil{t.Fatal(scanErr)}
 if status!="failed"||pid!=9876||proc.signalCalls!=0{
  t.Fatalf("exited process was re-signalled or misreported: %s %d signals=%d",status,pid,proc.signalCalls)
 }
}
func TestUnrecordedModelPIDPersistenceDoesNotOverwriteAnotherLaunch(t *testing.T){
 svc,db:=stopLeaseFixture(t,"healthy")
 proc:=&unrecordedProcessBackend{alive:true}
 svc.processes=proc
 cause:=errors.New("original launch database failure")
 err:=svc.reconcileUnrecordedLaunch(context.Background(),RuntimeInstance{ID:"inst",Revision:1,ProcessFingerprint:"identity",DeploymentID:"model"},4321,cause)
 if !errors.Is(err,cause)||err.Error()==cause.Error(){
  t.Fatalf("launch state conflict was hidden: %v",err)
 }
 var status string
 if scanErr:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&status);scanErr!=nil{t.Fatal(scanErr)}
 if status!="healthy"||proc.signalCalls!=0{
  t.Fatalf("unrecorded launch overwrote new owner or sent unauthorised signal: %s %d",status,proc.signalCalls)
 }
}

func TestUnrecordedModelRecoveryRejectsStaleLaunchGeneration(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 proc:=&unrecordedProcessBackend{alive:true}
 svc.processes=proc
 if _,err:=db.Exec(`UPDATE local_runtime_instances SET revision=2,process_fingerprint='new-launch' WHERE id='inst'`);err!=nil{t.Fatal(err)}
 err:=svc.reconcileUnrecordedLaunch(context.Background(),
  RuntimeInstance{ID:"inst",Revision:1,ProcessFingerprint:"identity",DeploymentID:"model"},
  4321,errors.New("original launch store failed"))
 if err==nil||!strings.Contains(err.Error(),"state changed"){
  t.Fatalf("old launch generation was allowed to claim replacement: %v",err)
 }
 var revision int64
 var fingerprint string
 var pid sql.NullInt64
 if e:=db.QueryRow(`SELECT revision,process_fingerprint,pid FROM local_runtime_instances WHERE id='inst'`).Scan(&revision,&fingerprint,&pid);e!=nil{t.Fatal(e)}
 if revision!=2||fingerprint!="new-launch"||pid.Valid||proc.signalCalls!=0{
  t.Fatalf("old launched model overwrote newer generation: revision=%d fingerprint=%s pid=%v signals=%d",revision,fingerprint,pid,proc.signalCalls)
 }
}

func TestUnrecordedModelPersistsPIDBeforeProcessInspection(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 proc:=&unrecordedProcessBackend{alive:true}
 seen:=0
 proc.onAlive=func(pid int){
  seen++
  var gotPID int
  var gotStatus string
  if err:=db.QueryRow(`SELECT pid,status FROM local_runtime_instances WHERE id='inst'`).Scan(&gotPID,&gotStatus);err!=nil{
   t.Fatalf("process inspected before PID was durably stored: %v",err)
  }
  if gotPID!=pid||gotStatus!="orphaned"{
   t.Fatalf("process checked without orphan ownership: got %s pid %d, expected %d",gotStatus,gotPID,pid)
  }
 }
 svc.processes=proc
 _=svc.reconcileUnrecordedLaunch(context.Background(),
  RuntimeInstance{ID:"inst",Revision:1,ProcessFingerprint:"identity",DeploymentID:"model",Port:11333},
  4477,errors.New("simulated launch CAS refusal"))
 if seen<1{t.Fatal("fake process never checked after durable ownership")}
}
