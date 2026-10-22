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
 if _,err:=db.Exec(`INSERT INTO local_runtime_instances
 (id,node_id,deployment_id,status,revision,updated_at)
 VALUES('old-inst','node','previous','healthy',1,2)`);err!=nil{t.Fatal(err)}
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
   cancel() // the chat request ended AFTER the old resident was evicted
   return RuntimeInstance{},context.Canceled
  }
  if id!="previous"{t.Fatalf("unexpected model start %s",id)}
  if callCtx.Err()!=nil{t.Fatal("rollback reused cancelled chat context")}
  if _,ok:=callCtx.Deadline();!ok{t.Fatal("rollback has no bound")}
  if _,err:=db.Exec(`UPDATE local_runtime_instances
    SET status='healthy' WHERE deployment_id='previous'`);err!=nil{t.Fatal(err)}
  return RuntimeInstance{ID:"old-inst",Status:RuntimeHealthy},nil
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
