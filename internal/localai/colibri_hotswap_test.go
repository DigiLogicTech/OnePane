package localai

import (
    "reflect"
 "os"
 "context"
 "sort"
 "database/sql"
 "errors"
 "fmt"
 "time"
    "strings"
    "testing"
)

func TestColibriHotSwapEvictsOnlyOneIdleResident(t *testing.T) {
    idle:=[]colibriSwapCandidate{{ID:"sole",Status:RuntimeHealthy,LastUsed:2}}
    out,err:=selectColibriEvictions(idle,map[string]int{})
    if err!=nil||!reflect.DeepEqual(out,[]string{"sole"}){
        t.Fatalf("single safely idle Colibri not selected: %v %v",out,err)
    }
}

func TestColibriHotSwapRefusesAbnormalMultipleIdleResidents(t *testing.T){
    peers:=[]colibriSwapCandidate{
        {ID:"older",Status:RuntimeHealthy,LastUsed:2},
        {ID:"newer",Status:RuntimeHealthy,LastUsed:9},
    }
    out,err:=selectColibriEvictions(peers,map[string]int{})
    if err==nil||!strings.Contains(err.Error(),"multiple Colibri")||len(out)!=0{
        t.Fatalf("multiple resident Colibri would be evicted partially: %v %v",out,err)
    }
}

func TestColibriHotSwapRefusesBusyBeforeAnyEviction(t *testing.T) {
    tests:=[]struct{
        name string
        candidates []colibriSwapCandidate
        active map[string]int
        errorPart string
    }{
        {"busy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"busy",Status:RuntimeBusy}},nil,"busy"},
        {"active lease despite healthy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"leased",Status:RuntimeHealthy}},map[string]int{"leased":1},"active"},
        {"starting",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"loading",Status:RuntimeStarting}},nil,"starting"},
        {"draining",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"drain",Status:RuntimeDraining}},nil,"draining"},
        {"orphaned",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"unknown",Status:RuntimeOrphaned}},nil,"orphaned"},
    }
    for _,tt:=range tests{
        t.Run(tt.name,func(t *testing.T){
            out,err:=selectColibriEvictions(tt.candidates,tt.active)
            if err==nil||!strings.Contains(err.Error(),tt.errorPart){
                t.Fatalf("expected %s error, got evictions %v, err %v",tt.errorPart,out,err)
            }
            if len(out)!=0{t.Fatalf("partial eviction allowed: %v",out)}
        })
    }
}

func TestColibriHotSwapNoConflictingModels(t *testing.T) {
    out,err:=selectColibriEvictions(nil,nil)
    if err!=nil||len(out)!=0 {t.Fatalf("unexpected result: out=%v err=%v",out,err)}
}

func TestColibriSwapStatusRejectsMissingSupervisor(t *testing.T){
 ctx:=context.Background()
 var missing *Service
 if _,err:=missing.ColibriHotSwapStatus(ctx,"model");err==nil{
  t.Fatal("nil local runtime service allowed status inspection")
 }
 svc:=&Service{}
 if _,err:=svc.ColibriHotSwapStatus(ctx,"model");err==nil{
  t.Fatal("unavailable runtime supervisor caused a nil dereference")
 }
}

func TestColibriSwapStatusPreservesFailedVersusStoppedInstance(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,query:=range []string{
  `CREATE TABLE local_runtime_instances(
   deployment_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,
   status TEXT NOT NULL,last_seen_at INTEGER,started_at INTEGER,
   updated_at INTEGER NOT NULL,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances
   (deployment_id,node_id,status,last_seen_at,started_at,updated_at,pid,failure_reason)
   VALUES('colibri-a','node-1','failed',3,NULL,5,NULL,'managed runtime launch cancelled or could not create child')`,
  `INSERT INTO local_runtime_instances
   (deployment_id,node_id,status,last_seen_at,started_at,updated_at,pid,failure_reason)
   VALUES('colibri-b','node-1','healthy',4,2,6,NULL,NULL)`,
 }{
  if _,err:=db.Exec(query);err!=nil{t.Fatal(err)}
 }
 service:=&Service{db:db,supervisor:&RuntimeSupervisor{db:db}}
 ctx:=context.Background()
 failed,err:=service.ColibriHotSwapStatus(ctx,"colibri-a")
 if err!=nil{t.Fatal(err)}
 if failed.Status!="failed"||failed.Message==""||
  !reflect.DeepEqual(failed.ActiveDeploymentIDs,[]string{"colibri-b"}){
  t.Fatalf("failed engine was hidden or healthy neighbour misplaced: %+v",failed)
 }
 if _,err=db.Exec(`UPDATE local_runtime_instances SET status='stopped'
   WHERE deployment_id='colibri-a'`);err!=nil{t.Fatal(err)}
 stopped,err:=service.ColibriHotSwapStatus(ctx,"colibri-a")
 if err!=nil||stopped.Status!="stopped"||stopped.Message!=""{
  t.Fatalf("operator-requested stop was classified as failure: %+v %v",stopped,err)
 }
 missing,err:=service.ColibriHotSwapStatus(ctx,"colibri-remote")
 if err!=nil||missing.Status!="stopped"||len(missing.ActiveDeploymentIDs)!=0{
  t.Fatalf("other Node's model was included in local Colibri status: %+v %v",missing,err)
 }
}


// The source-level startup order is an intentionally reviewed pre-effect
// contract. Missing Python must be detected before the old Colibri model
// receives any stop signal, for explicit swaps AND ordinary chat acquisition.
func TestColibriStartPreflightsPythonBeforeEvictingModels(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 start:=strings.Index(source,"func (s *RuntimeSupervisor) startLocked(")
 if start<0{t.Fatal("managed start path missing")}
 scope:=source[start:]
 // Only inspect startLocked, not later supervisor functions that are
 // independently allowed to discover Python for other purposes.
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 prior:=strings.Index(scope,"colibriPython,err=findPythonExecutable()")
 eviction:=strings.Index(scope,"s.prepareColibriSwapLocked(ctx, nodeID, deploymentID)")
 if prior<0||eviction<=prior||strings.Index(scope,"var existing *RuntimeInstance")>prior{
  t.Fatal("working Colibri could be evicted before existing-runtime check or Python preflight")
 }
 if strings.Contains(scope[eviction:],"py, err := findPythonExecutable()"){
  t.Fatal("Python discovery repeated after healthy predecessor eviction")
 }
}


// A persisted healthy or starting row alone cannot authorise reusing a
// possibly recycled PID. Require the same independent process identity
// contract that guards termination before any reused runtime is returned.
func TestRuntimeReuseMustVerifyPIDIdentityBeforeReturningOrHealth(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 begin:=strings.Index(source,"func (s *RuntimeSupervisor) startLocked(")
 if begin<0{t.Fatal("runtime admission entrypoint missing")}
 scope:=source[begin:]
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 check:=strings.Index(scope,"s.VerifyIdentity(ctx,old)")
 wait:=strings.Index(scope,"s.waitHealthy(ctx, old.ID, 120*time.Second)")
 if check<0||wait<=check{
  t.Fatal("reused starting local PID can be probed before identity validation")
 }
 start:=strings.Index(scope,"if (old.Status == RuntimeHealthy")
 ret:=strings.Index(scope[check:],"return old, nil")
 if start<0||ret<0||start>check{
  t.Fatal("healthy or busy old PID not guarded by original process fingerprint")
 }
}

func TestCancelledColibriSwapGetsBoundedIndependentRecoveryContext(t *testing.T){
 parent,cancel:=context.WithCancel(context.Background())
 cancel()
 recovery,finish:=colibriFailureRecoveryContext(parent)
 defer finish()
 if recovery.Err()!=nil{t.Fatalf("cancelled request also canceled cleanup: %v",recovery.Err())}
 if deadline,ok:=recovery.Deadline();!ok||time.Until(deadline)>colibriFailureRecoveryLimit||
  time.Until(deadline)<colibriFailureRecoveryLimit-5*time.Second{
  t.Fatalf("Colibri cleanup deadline absent or unbounded: %v %v",deadline,ok)
 }
}
func TestColibriReplacementUnknownPIDBlocksRollback(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 recovery,finish:=colibriFailureRecoveryContext(ctx)
 defer finish()
 if err:=svc.reconcileFailedColibriReplacement(recovery,"model");err==nil||
  !strings.Contains(err.Error(),"rollback withheld"){
  t.Fatalf("unrecorded PID allowed old Colibri model to start: %v",err)
 }
 var status string
 if err:=db.QueryRow("SELECT status FROM local_runtime_instances WHERE id='inst'").Scan(&status);err!=nil{t.Fatal(err)}
 if status!="starting"{t.Fatalf("uncertain launching model was overwritten: %s",status)}
}
func TestColibriReplacementInspectionErrorsFailClosed(t *testing.T){
 svc,db:=stopLeaseFixture(t,"failed")
 ctx:=context.Background()
 if _,err:=db.Exec("DROP TABLE local_runtime_instances");err!=nil{t.Fatal(err)}
 err:=svc.reconcileFailedColibriReplacement(ctx,"model")
 if err==nil||!strings.Contains(err.Error(),"rollback withheld")||errors.Is(err,sql.ErrNoRows){
  t.Fatalf("storage inspection failure permitted rollback: %v",err)
 }
}
func TestColibriConfirmedNoChildFailureAllowsRollback(t *testing.T){
 for _,status:=range []string{"failed","stopped"} {
  t.Run(status,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,status)
   if _,err:=db.Exec(`UPDATE local_runtime_instances SET failure_reason=? WHERE id='inst'`,managedNoChildFailureReason);err!=nil{t.Fatal(err)}
   if err:=svc.reconcileFailedColibriReplacement(context.Background(),"model");err!=nil{
    t.Fatalf("proven no-child or completed stop unnecessarily prevents restore: %v",err)
   }
  })
 }
}

func TestColibriReplacementUnknownResultBlocksRollbackWithoutSignals(t *testing.T){
 for _,tc:=range []struct{
  name,status string
  pid int
  reason string
 }{
  {"orphaned no PID","orphaned",0,""},
  {"orphaned dead parent","orphaned",4321,""},
  {"draining no PID","draining",0,""},
  {"draining dead parent","draining",4321,""},
  {"starting live parent unverified","starting",4321,""},
  {"busy dead parent","busy",4321,""},
  {"healthy dead parent","healthy",4321,""},
  {"failed without no-child evidence","failed",0,""},
  {"failed with unrelated failure","failed",0,"health probe failed"},
  {"recorded corrupt PID","failed",1,managedNoChildFailureReason},
  {"failed with start evidence","failed",0,managedNoChildFailureReason},
  {"stopped with unproven NULL PID","stopped",0,""},
  {"stopped with unrelated reason","stopped",0,"operator stop"},
  {"stopped with recorded bad PID","stopped",1,managedNoChildFailureReason},
  {"stopped with start evidence","stopped",0,managedNoChildFailureReason},
  {"invalid state","unknown",0,""},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,tc.status)
   if tc.pid!=0{
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET pid=? WHERE id='inst'`,tc.pid);err!=nil{t.Fatal(err)}
   }
   if tc.reason!=""{
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET failure_reason=? WHERE id='inst'`,tc.reason);err!=nil{t.Fatal(err)}
   }
   if tc.name=="failed with start evidence"||tc.name=="stopped with start evidence"{
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET started_at=12 WHERE id='inst'`);err!=nil{t.Fatal(err)}
   }
   proc:=&unrecordedProcessBackend{alive:false}
   svc.processes=proc
   err:=svc.reconcileFailedColibriReplacement(context.Background(),"model")
   if err==nil||!strings.Contains(err.Error(),"rollback withheld"){
    t.Fatalf("uncertain replacement wrongly allowed rollback: %v",err)
   }
   if proc.signalCalls!=0{t.Fatal("unverified replacement triggered process signal")}
   var status string
   var rev int64
   if err:=db.QueryRow(`SELECT status,revision FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&rev);err!=nil{t.Fatal(err)}
   if status!=tc.status||rev!=1{t.Fatalf("failed reconciliation changed owner: status=%s revision=%d",status,rev)}
  })
 }
}

func TestColibriHotSwapRejectsLiveFailedAndStoppedEngineBeforeEviction(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,query:=range []string{
  `CREATE TABLE local_runtime_instances(
    node_id TEXT,deployment_id TEXT,status TEXT,last_seen_at INTEGER,
    started_at INTEGER,updated_at INTEGER,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,last_seen_at,started_at,updated_at,pid) VALUES
    ('node-1','colibri-a','healthy',5,4,6,1234),
    ('node-1','colibri-b','failed',6,5,7,4321)`,
 }{
  if _,err:=db.Exec(query);err!=nil{t.Fatal(err)}
 }
 proc:=&unrecordedProcessBackend{alive:true}
 supervisor:=&RuntimeSupervisor{db:db,processes:proc,activeRequests:map[string]int{}}
 ctx:=context.Background()
 for _,status:=range []string{"failed","stopped"}{
  if _,err:=db.ExecContext(ctx,"UPDATE local_runtime_instances SET status=? WHERE deployment_id='colibri-b'",status);err!=nil{t.Fatal(err)}
  residents,err:=supervisor.colibriResidents(ctx,"node-1","new-colibri")
  if err!=nil{t.Fatal(err)}
  selected,selectionErr:=selectColibriEvictions(residents,nil)
  if selectionErr==nil||len(selected)!=0||
    !strings.Contains(selectionErr.Error(),"live process"){
    t.Fatalf("live %s engine was ignored and healthy model evicted: selected=%v err=%v",status,selected,selectionErr)
  }
  stateService:=&Service{db:db,supervisor:supervisor}
  state,err:=stateService.ColibriHotSwapStatus(ctx,"colibri-b")
  if err!=nil{t.Fatal(err)}
  sort.Strings(state.ActiveDeploymentIDs)
  if state.Status!=status||!reflect.DeepEqual(state.ActiveDeploymentIDs,[]string{"colibri-a","colibri-b"})||
    !strings.Contains(state.Message,"still running"){
    t.Fatalf("operator view hid live %s engine: %+v",status,state)
  }
 }
 if proc.signalCalls!=0{t.Fatal("admission inspection unexpectedly signalled a model")}
}

func TestColibriFailedRowWithConfirmedDeadPIDDoesNotBlockSwap(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(
    node_id TEXT,deployment_id TEXT,status TEXT,last_seen_at INTEGER,
    started_at INTEGER,updated_at INTEGER,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,last_seen_at,started_at,updated_at,pid)
    VALUES ('node-1','colibri-b','failed',5,4,6,4321)`,
 }{
  if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 supervisor:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{alive:false}}
 residents,err:=supervisor.colibriResidents(context.Background(),"node-1","new-colibri")
 if err!=nil||len(residents)!=0{t.Fatalf("confirmed dead legacy PID blocked safe swap: %v %v",residents,err)}
}

func TestColibriHotSwapStatusShowsOrphanedAndDrainingAsPossibleResidents(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(
    node_id TEXT,deployment_id TEXT,status TEXT,last_seen_at INTEGER,
    started_at INTEGER,updated_at INTEGER,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances(node_id,deployment_id,status,last_seen_at,started_at,updated_at,pid) VALUES
   ('node-1','colibri-a','healthy',5,4,6,1234),
   ('node-1','colibri-b','orphaned',6,5,7,NULL)`,
 }{
  if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 proc:=&unrecordedProcessBackend{alive:false}
 svc:=&Service{db:db,supervisor:&RuntimeSupervisor{db:db,processes:proc}}
 for _,status:=range []string{"orphaned","draining"}{
  t.Run(status,func(t *testing.T){
   if _,err:=db.Exec(`UPDATE local_runtime_instances SET status=? WHERE deployment_id='colibri-b'`,status);err!=nil{t.Fatal(err)}
   state,err:=svc.ColibriHotSwapStatus(context.Background(),"colibri-b")
   if err!=nil{t.Fatal(err)}
   sort.Strings(state.ActiveDeploymentIDs)
   if state.Status!=status ||
    !reflect.DeepEqual(state.ActiveDeploymentIDs,[]string{"colibri-a","colibri-b"}) ||
    !strings.Contains(state.Message,"RAM/VRAM may still be occupied"){
    t.Fatalf("%s model hidden from operator's possibly resident list: %+v",status,state)
   }
  })
 }
 if proc.signalCalls!=0{t.Fatal("read-only Colibri state observation signalled a process")}
}

func TestColibriUnknownStoppedOrFailedPIDRemainsVisibleAsPossibleResident(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(
   node_id TEXT,deployment_id TEXT,status TEXT,last_seen_at INTEGER,
   started_at INTEGER,updated_at INTEGER,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances
   (node_id,deployment_id,status,last_seen_at,updated_at,pid)
   VALUES('node-1','colibri-b','stopped',5,6,NULL)`,
 }{if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}}
 supervisor:=&RuntimeSupervisor{db:db,processes:&unrecordedProcessBackend{alive:false}}
 service:=&Service{db:db,supervisor:supervisor}
 for _,status:=range []string{"stopped","failed"}{
  t.Run(status,func(t *testing.T){
   if _,err:=db.Exec(`UPDATE local_runtime_instances
    SET status=?,failure_reason=NULL,started_at=NULL,pid=NULL WHERE deployment_id='colibri-b'`,status);err!=nil{t.Fatal(err)}
   residents,err:=supervisor.colibriResidents(context.Background(),"node-1","other")
   if err!=nil||len(residents)!=1||!residents[0].OwnershipUnverified{
    t.Fatalf("unknown %s model hidden from occupancy: %+v %v",status,residents,err)
   }
   ids,e:=selectColibriEvictions(residents,nil)
   if e==nil||len(ids)!=0||!strings.Contains(e.Error(),"operator reconciliation"){
    t.Fatalf("unknown %s predecessor eligible for eviction: %v %v",status,ids,e)
   }
   state,err:=service.ColibriHotSwapStatus(context.Background(),"colibri-b")
   if err!=nil||state.Status!=status||
    !reflect.DeepEqual(state.ActiveDeploymentIDs,[]string{"colibri-b"})||
    !strings.Contains(state.Message,"RAM/VRAM may still be occupied"){
    t.Fatalf("unknown %s model shown as free: %+v %v",status,state,err)
   }
  })
 }
 // Exact no-child evidence must not strand legitimate ordinary model swaps.
 if _,err:=db.Exec(`UPDATE local_runtime_instances SET
  status='stopped',pid=NULL,started_at=NULL,failure_reason=?
  WHERE deployment_id='colibri-b'`,managedNoChildFailureReason);err!=nil{t.Fatal(err)}
 residents,err:=supervisor.colibriResidents(context.Background(),"node-1","other")
 if err!=nil||len(residents)!=0{t.Fatalf("verified no-child model falsely reported as live: %+v %v",residents,err)}
 state,err:=service.ColibriHotSwapStatus(context.Background(),"colibri-b")
 if err!=nil||len(state.ActiveDeploymentIDs)!=0||state.Message!=""{
  t.Fatalf("verified no-child stopped state falsely blocked: %+v %v",state,err)
 }
}

func TestColibriHealthyBusyWithoutPIDCannotClaimSafeEviction(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(
    node_id TEXT,deployment_id TEXT,status TEXT,last_seen_at INTEGER,
    started_at INTEGER,updated_at INTEGER,pid INTEGER,failure_reason TEXT)`,
  `INSERT INTO local_runtime_instances(
     node_id,deployment_id,status,last_seen_at,started_at,updated_at,pid)
    VALUES('node-1','colibri-a','healthy',5,4,6,NULL)`,
 }{if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}}
 proc:=&unrecordedProcessBackend{alive:false}
 sup:=&RuntimeSupervisor{db:db,processes:proc,activeRequests:map[string]int{}}
 svc:=&Service{db:db,supervisor:sup}
 for _,status:=range []string{"healthy","busy"}{
  for _,pid:=range []any{nil,int64(0),int64(1)}{
   t.Run(status+"/pid"+fmt.Sprint(pid),func(t *testing.T){
    if _,err:=db.Exec(`UPDATE local_runtime_instances
       SET status=?,pid=? WHERE deployment_id='colibri-a'`,status,pid);err!=nil{t.Fatal(err)}
    residents,err:=sup.colibriResidents(context.Background(),"node-1","colibri-b")
    if err!=nil||len(residents)!=1||!residents[0].OwnershipUnverified{
     t.Fatalf("%s missing PID %v incorrectly recognised as verified: %v %v",status,pid,residents,err)
    }
    evicted,err:=selectColibriEvictions(residents,nil)
    if err==nil||len(evicted)!=0||!strings.Contains(err.Error(),"operator reconciliation")&&status=="healthy"{
     t.Fatalf("unverified Colibri healthy/busy evicted: %v %v",evicted,err)
    }
    state,err:=svc.ColibriHotSwapStatus(context.Background(),"colibri-a")
    if err!=nil||state.Status!=status||
       !reflect.DeepEqual(state.ActiveDeploymentIDs,[]string{"colibri-a"})||
       !strings.Contains(state.Message,"RAM/VRAM ownership is uncertain"){
     t.Fatalf("%s PID %v was advertised safe: %+v %v",status,pid,state,err)
    }
   })
  }
 }
 if proc.signalCalls!=0{t.Fatal("read-only residency inspection signalled an OS process")}
}
