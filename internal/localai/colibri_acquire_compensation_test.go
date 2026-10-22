package localai

import (
 "context"
 "database/sql"
 "errors"
 "os"
 "strings"
 "testing"
)

func colibriSwapFailureFixture(t *testing.T)(*RuntimeSupervisor,*sql.DB){
 t.Helper()
 svc,db:=stopLeaseFixture(t,"failed")
 if _,err:=db.Exec("ALTER TABLE model_deployments ADD COLUMN runtime_config_json TEXT");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(`UPDATE model_deployments
 SET runtime_name='colibri',runtime_config_json='{}' WHERE id='model'`);err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(`INSERT INTO model_deployments
  (id,runtime_name,residency_state,updated_at,revision,runtime_config_json)
  VALUES('previous','colibri','resident',1,1,'{}')`);err!=nil{t.Fatal(err)}
 // This predecessor genuinely launched and has a durable PID. After
 // synthetic eviction the test backend independently reports it dead;
 // a stopped/NULL-PID row would instead be unresolved ownership and must
 // not allow a replacement or rollback.
 if _,err:=db.Exec(`INSERT INTO local_runtime_instances
 (id,node_id,deployment_id,runtime_id,managed_model_id,status,pid,
  started_at,revision,updated_at,bind_address,port,process_fingerprint,
  launch_json,health_json,created_at)
 VALUES('old-inst','node','previous','colibri-runtime','model-weights',
  'healthy',4321,1,1,2,'127.0.0.1',11333,'old-fingerprint','{}','{}',1)`);err!=nil{t.Fatal(err)}
 // No OS process belongs to this deterministic rollback fixture.
 // The recorded predecessor PID is historical evidence only; never
 // inspect or signal a coincidentally reused CI worker PID 4321.
 svc.processes=&unrecordedProcessBackend{alive:false}
 return svc,db
}

func TestChatColibriReplacementCancelledAfterEvictionRestoresPreviousWithIndependentDeadline(t *testing.T){
 svc,db:=colibriSwapFailureFixture(t)
 ctx,cancel:=context.WithCancel(context.Background())
 defer cancel()
 calls:=[]string{}
 starter:=func(callCtx context.Context,id string)(RuntimeInstance,error){
  calls=append(calls,id)
  if id=="model"{
   if _,err:=db.Exec(`UPDATE local_runtime_instances
     SET status='stopped' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
   // This scenario deliberately cancels BEFORE the replacement child was
   // spawned. Preserve the same durable no-child evidence production writes
   // at the prelaunch cancellation boundary; an unknown launch would
   // correctly withhold rollback instead of restoring the old model.
   if _,err:=db.Exec(`UPDATE local_runtime_instances
     SET status='failed',pid=NULL,started_at=NULL,failure_reason=?
     WHERE deployment_id='model'`,managedNoChildFailureReason);err!=nil{t.Fatal(err)}
   cancel() // the chat request ended AFTER the old resident was evicted
   return RuntimeInstance{},context.Canceled
  }
  if id!="previous"{t.Fatalf("unexpected model start %s",id)}
  if callCtx.Err()!=nil{t.Fatal("rollback reused cancelled chat context")}
  if _,ok:=callCtx.Deadline();!ok{t.Fatal("rollback has no bound")}
  if _,err:=db.Exec(`UPDATE local_runtime_instances
    SET status='healthy' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
  return RuntimeInstance{ID:"old-inst",NodeID:"node",DeploymentID:"previous",
   PID:4321,ProcessFingerprint:"old-fingerprint",BindAddress:"127.0.0.1",
   Port:11333,Status:RuntimeHealthy},nil
 }
 svc.residencyMu.Lock()
 state,inst,err:=svc.colibriSwapOnNodeLocked(ctx,"model","node",starter)
 svc.residencyMu.Unlock()
 if err==nil||!strings.Contains(err.Error(),"restored previous model previous"){
  t.Fatalf("cancelled replacement failed to restore old model: state=%+v err=%v",state,err)
 }
 if inst.ID!=""{t.Fatal("failed replacement falsely returned usable runtime")}
 if len(calls)!=2||calls[0]!="model"||calls[1]!="previous"{
  t.Fatalf("previous Colibri model not restored in order: %v",calls)
 }
 var status string
 if err:=db.QueryRow("SELECT status FROM local_runtime_instances WHERE deployment_id='previous'").Scan(&status);err!=nil{t.Fatal(err)}
 if status!="healthy"{t.Fatalf("existing Colibri remained evicted after failed chat: %s",status)}
 if len(state.EvictedDeploymentIDs)!=0||
  len(state.ActiveDeploymentIDs)!=1||state.ActiveDeploymentIDs[0]!="previous"||
  state.Status!="failed"||
  !strings.Contains(state.Message,"previous Colibri model was restored"){
  t.Fatalf("rollback outcome misreported as still evicted or target success: %+v",state)
 }
}

func TestColibriReplacementDoesNotGuessRollbackWhenTargetPIDUnknown(t *testing.T){
 svc,db:=colibriSwapFailureFixture(t)
 if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='starting',pid=NULL
 WHERE deployment_id='model'`);err!=nil{t.Fatal(err)}
 calls:=0
 starter:=func(_ context.Context,id string)(RuntimeInstance,error){
  calls++
  if id!="model"{t.Fatalf("unexpected rollback start: %s",id)}
  if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='stopped'
   WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
  return RuntimeInstance{},errors.New("model engine launch failed")
 }
 svc.residencyMu.Lock()
 _,_,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
 svc.residencyMu.Unlock()
 if err==nil||!strings.Contains(err.Error(),"rollback withheld")||calls!=1{
  t.Fatalf("uncertain target PID allowed unsafe predecessor restart: calls=%d err=%v",calls,err)
 }
}

func TestChatColibriUsesSameFailClosedSwapAsExplicitControl(t *testing.T){
 acquire,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 scope:=string(acquire)
 start:=strings.Index(scope,"func (s *RuntimeSupervisor) Acquire(")
 if start<0{t.Fatal("inference model Acquire not found")}
 scope=scope[start:]
 if end:=strings.Index(scope,"\nfunc ");end>=0{scope=scope[:end]}
 if !strings.Contains(scope,"s.colibriHotSwapLocked(ctx,deploymentID)") {
  t.Fatal("normal chat Acquire bypasses the verified Colibri rollback path")
 }
 swap,err:=os.ReadFile("colibri_hotswap.go")
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(swap),"return s.colibriSwapOnNodeLocked(ctx,deploymentID,nodeID,s.startLocked)"){
  t.Fatal("Colibri compensation is not shared by explicit and chat swap paths")
 }
}

func TestColibriReplacementUnknownOwnerCannotRestartEvictedModel(t *testing.T){
 for _,status:=range []string{"orphaned","draining","failed","busy"}{
  t.Run(status,func(t *testing.T){
   svc,db:=colibriSwapFailureFixture(t)
   calls:=[]string{}
   starter:=func(_ context.Context,id string)(RuntimeInstance,error){
    calls=append(calls,id)
    if id!="model"{t.Fatalf("unsafe restoration of previous model %s",id)}
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status=?,pid=NULL,failure_reason=NULL WHERE deployment_id='model'`,status);err!=nil{t.Fatal(err)}
    return RuntimeInstance{},errors.New("replacement outcome uncertain")
   }
   svc.residencyMu.Lock()
   _,_,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
   svc.residencyMu.Unlock()
   if err==nil||!strings.Contains(err.Error(),"rollback withheld")||len(calls)!=1{
    t.Fatalf("unknown %s replacement permitted old engine reload: calls=%v err=%v",status,calls,err)
   }
   var stored string
   if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='model'`).Scan(&stored);err!=nil{t.Fatal(err)}
   if stored!=status{t.Fatalf("uncertain replacement state rewritten: %s",stored)}
  })
 }
}

func TestColibriStarterCannotAdvertiseUnverifiedResidentProcess(t *testing.T){
 for _,tc:=range []struct{name string;mutate func(*RuntimeInstance)}{
  {"no instance",func(x *RuntimeInstance){*x=RuntimeInstance{}}},
  {"wrong deployment",func(x *RuntimeInstance){x.DeploymentID="foreign"}},
  {"wrong node",func(x *RuntimeInstance){x.NodeID="foreign-node"}},
  {"missing instance ID",func(x *RuntimeInstance){x.ID=""}},
  {"missing process PID",func(x *RuntimeInstance){x.PID=0}},
  {"missing process fingerprint",func(x *RuntimeInstance){x.ProcessFingerprint=""}},
  {"not yet healthy",func(x *RuntimeInstance){x.Status=RuntimeStarting}},
  {"wrong host",func(x *RuntimeInstance){x.BindAddress="0.0.0.0"}},
  {"missing port",func(x *RuntimeInstance){x.Port=0}},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,db:=colibriSwapFailureFixture(t)
   good:=RuntimeInstance{ID:"verified",DeploymentID:"model",NodeID:"node",
    PID:4321,ProcessFingerprint:"fingerprint",BindAddress:"127.0.0.1",
    Port:11333,Status:RuntimeHealthy}
   tc.mutate(&good)
   calls:=0
   starter:=func(_ context.Context,model string)(RuntimeInstance,error){
    calls++
    if model!="model"{t.Fatalf("unverified target initiated rollback to %s",model)}
    return good,nil
   }
   svc.residencyMu.Lock()
   state,inst,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
   svc.residencyMu.Unlock()
   if err==nil||!strings.Contains(err.Error(),"unverified model residency")||
    calls!=1||inst.ID!=""||len(state.ActiveDeploymentIDs)!=0{
    t.Fatalf("invalid model was advertised ready: calls=%d state=%+v inst=%+v err=%v",calls,state,inst,err)
   }
   var prev string
   if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='previous'`).Scan(&prev);err!=nil{t.Fatal(err)}
   if prev!="healthy"{t.Fatalf("unverified launch altered working predecessor: %s",prev)}
  })
 }
}

func TestColibriVerifiedStarterResultAcceptsBusyAndHealthy(t *testing.T){
 for _,status:=range []RuntimeInstanceStatus{RuntimeHealthy,RuntimeBusy}{
  inst:=RuntimeInstance{ID:"instance",NodeID:"node",DeploymentID:"model",
   PID:4133,Status:status,ProcessFingerprint:"fp",BindAddress:"127.0.0.1",
   Port:11333}
  if !verifiedColibriResidency(inst,"model","node"){
   t.Fatalf("legitimate %s resident was rejected",status)
  }
 }
}

func TestColibriVerifiedReplacementRejectsUnstoppedPredecessor(t *testing.T){
 svc,db:=colibriSwapFailureFixture(t)
 good:=RuntimeInstance{ID:"verified",DeploymentID:"model",NodeID:"node",
  PID:4321,ProcessFingerprint:"fp",BindAddress:"127.0.0.1",Port:11333,
  Status:RuntimeHealthy}
 calls:=0
 starter:=func(_ context.Context,id string)(RuntimeInstance,error){
  calls++
  if id!="model"{t.Fatalf("unexpected auto rollback for model %q",id)}
  return good,nil
 }
 svc.residencyMu.Lock()
 state,inst,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
 svc.residencyMu.Unlock()
 if err==nil||!strings.Contains(err.Error(),"competing process residency")||
  calls!=1||inst.ID!=""||len(state.ActiveDeploymentIDs)!=0{
  t.Fatalf("two Colibri engines were incorrectly reported usable: state=%+v inst=%+v calls=%d err=%v",state,inst,calls,err)
 }
 var saved string
 if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='previous'`).Scan(&saved);err!=nil{t.Fatal(err)}
 if saved!="healthy"{t.Fatalf("unverified overlap modified healthy predecessor: %s",saved)}
}

func TestColibriVerifiedTargetCanReplaceActuallyStoppedPredecessor(t *testing.T){
 svc,db:=colibriSwapFailureFixture(t)
 target:=RuntimeInstance{ID:"inst",DeploymentID:"model",
  NodeID:"node",PID:5678,ProcessFingerprint:"fingerprint",
  BindAddress:"127.0.0.1",Port:11333,Status:RuntimeHealthy}
 starter:=func(_ context.Context,id string)(RuntimeInstance,error){
  if id!="model"{t.Fatalf("unexpected target %s",id)}
  if _,err:=db.Exec(`UPDATE local_runtime_instances
    SET status='stopped' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
  // Simulate the guarded PID/readiness persistence performed by Start.
  if _,err:=db.Exec(`UPDATE local_runtime_instances
    SET status='healthy',pid=?,process_fingerprint=?,port=?,
        bind_address='127.0.0.1'
    WHERE deployment_id='model'`,target.PID,target.ProcessFingerprint,target.Port);err!=nil{t.Fatal(err)}
  return target,nil
 }
 svc.residencyMu.Lock()
 state,got,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
 svc.residencyMu.Unlock()
 if err!=nil || got.ID!=target.ID || state.Status!="healthy" ||
  len(state.ActiveDeploymentIDs)!=1||state.ActiveDeploymentIDs[0]!="model"{
  t.Fatalf("valid single Colibri resident rejected: state=%+v got=%+v err=%v",state,got,err)
 }
}

func TestDirectColibriStartSharesExplicitAndChatRecovery(t *testing.T) {
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 code:=string(raw)
 start:=strings.Index(code,"func (s *RuntimeSupervisor) Start(")
 end:=strings.Index(code[start:],"\nfunc (s *RuntimeSupervisor) startLocked(")
 if start<0||end<=0{t.Fatal("direct model Start boundary missing")}
 direct:=code[start:start+end]
 cfg:=strings.Index(direct,"s.resolve(ctx,deploymentID)")
 route:=strings.Index(direct,"s.colibriHotSwapLocked(ctx,deploymentID)")
 bare:=strings.LastIndex(direct,"return s.startLocked(ctx, deploymentID)")
 if cfg<0||route<=cfg||bare<=route||
  !strings.Contains(direct,"strings.EqualFold(cfg.RuntimeBackend,\"colibri\")"){
  t.Fatal("direct Models Start bypasses Colibri's inspected and bounded rollback coordinator")
 }
 if !strings.Contains(direct,"s.residencyMu.Lock()"){
  t.Fatal("direct model Start lacks residency mutex for model swap")
 }
}

func TestColibriRollbackCannotClaimAnUnverifiedRestoredModel(t *testing.T){
 for _,tc:=range []struct{name string;modify func(*RuntimeInstance,*sql.DB,*testing.T)}{
  {"starter returned empty result",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){*inst=RuntimeInstance{}}},
  {"restored wrong deployment",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.DeploymentID="foreign"}},
  {"restored wrong node",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.NodeID="foreign"}},
  {"restored missing PID",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.PID=0}},
  {"restored not healthy",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.Status=RuntimeStarting}},
  {"returned PID differs from persisted",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.PID=9876}},
  {"returned fingerprint differs from persisted",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.ProcessFingerprint="forged"}},
  {"persisted predecessor remains stopped",func(_ *RuntimeInstance,_ *sql.DB,_ *testing.T){}},
  {"competing replacement resurfaced",func(_ *RuntimeInstance,db *sql.DB,t *testing.T){
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='orphaned',pid=NULL,failure_reason=NULL WHERE deployment_id='model'`);err!=nil{t.Fatal(err)}
  }},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,db:=colibriSwapFailureFixture(t)
   calls:=0
   starter:=func(_ context.Context,id string)(RuntimeInstance,error){
    calls++
    if id=="model"{
     if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
     if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='failed',pid=NULL,started_at=NULL,failure_reason=? WHERE deployment_id='model'`,managedNoChildFailureReason);err!=nil{t.Fatal(err)}
     return RuntimeInstance{},errors.New("replacement launch failed")
    }
    if id!="previous"{t.Fatalf("unexpected rollback target %s",id)}
    if tc.name!="persisted predecessor remains stopped"{
     if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='healthy' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
    }
    restored:=RuntimeInstance{ID:"old-inst",NodeID:"node",DeploymentID:"previous",PID:4321,
      ProcessFingerprint:"old-fingerprint",BindAddress:"127.0.0.1",Port:11333,Status:RuntimeHealthy}
    tc.modify(&restored,db,t)
    return restored,nil
   }
   svc.residencyMu.Lock()
   state,inst,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
   svc.residencyMu.Unlock()
   if err==nil||!strings.Contains(err.Error(),"operator reconciliation required")||calls!=2||
      inst.ID!=""||len(state.ActiveDeploymentIDs)!=0||
      strings.Contains(state.Message,"was restored"){
    t.Fatalf("unproven Colibri rollback advertised as restored: state=%+v instance=%+v calls=%d err=%v",state,inst,calls,err)
   }
  })
 }
}

func TestColibriTargetCannotAdvertiseUnpersistedSuccessfulStart(t *testing.T){
 for _,tc:=range []struct{name string;mutate func(*RuntimeInstance,*sql.DB,*testing.T)}{
  {"result ID absent in SQLite",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.ID="not-persisted"}},
  {"result PID differs from durable owner",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.PID=2222}},
  {"result fingerprint differs from durable owner",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.ProcessFingerprint="different"}},
  {"result port differs from durable owner",func(inst *RuntimeInstance,_ *sql.DB,_ *testing.T){inst.Port=11999}},
  {"SQLite retained stopped state",func(_ *RuntimeInstance,db *sql.DB,t *testing.T){
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='model'`);err!=nil{t.Fatal(err)}
  }},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,db:=colibriSwapFailureFixture(t)
   target:=RuntimeInstance{ID:"inst",NodeID:"node",DeploymentID:"model",
    PID:5678,ProcessFingerprint:"fingerprint",BindAddress:"127.0.0.1",
    Port:11333,Status:RuntimeHealthy}
   starter:=func(_ context.Context,id string)(RuntimeInstance,error){
    if id!="model"{t.Fatalf("unverified target triggered rollback: %s",id)}
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
    if _,err:=db.Exec(`UPDATE local_runtime_instances SET status='healthy',pid=?,
      process_fingerprint=?,port=?,bind_address='127.0.0.1'
      WHERE deployment_id='model'`,target.PID,target.ProcessFingerprint,target.Port);err!=nil{t.Fatal(err)}
    tc.mutate(&target,db,t)
    return target,nil
   }
   svc.residencyMu.Lock()
   state,got,err:=svc.colibriSwapOnNodeLocked(context.Background(),"model","node",starter)
   svc.residencyMu.Unlock()
   if err==nil||!strings.Contains(err.Error(),"unverified durable residency")||
       got.ID!=""||len(state.ActiveDeploymentIDs)!=0||
       strings.Contains(state.Message,"ready"){
    t.Fatalf("false-ready Colibri target accepted: state=%+v got=%+v err=%v",state,got,err)
   }
  })
 }
}
