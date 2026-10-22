//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/inference"
)

type identityBoundExecutor struct{
 calls atomic.Int32
 entered chan struct{}
 release chan struct{}
}
func(e *identityBoundExecutor)DispatchFederatedLocal(ctx context.Context,dep,requestID string,prompt json.RawMessage)(inference.DispatchResult,error){
 e.calls.Add(1)
 if e.entered!=nil{
  select {case e.entered<-struct{}{}:default:}
  select {case <-e.release:case <-ctx.Done():return inference.DispatchResult{},ctx.Err()}
 }
 return inference.DispatchResult{ResponseJSON:json.RawMessage(`{"echo":`+string(prompt)+`}`),UsageJSON:json.RawMessage(`{"remote":true}`)},nil
}

func TestFederatedRemoteRequestIdempotencyIsBoundToPromptAndModel(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","origin-fp")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,
  capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Remote',0,'peer-fp','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(id,model_ref,
  modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('model','example/local','["text"]','{}','user_trusted',?,?)`,now,now);
  err!=nil{t.Fatal(err)}
 for _,dep:=range []string{"a","b"}{
  if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(
   id,model_id,node_id,runtime_name,runtime_config_json,status,
   deployment_fingerprint,revision,discovered_at,updated_at)
   VALUES(?,'model','origin','local','{}','ready',?,1,?,?)`,
   dep,"hash-"+dep,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},Identity{},"",30*time.Second)
 e:=&identityBoundExecutor{}
 request:=RemoteInferenceRequest{Protocol:ProtocolVersion,RequestID:"shared-id",
  DeploymentID:"a",RequestJSON:json.RawMessage(`{"prompt":"user-secret-unique"}`)}
 first,err:=svc.executeInbound(ctx,"peer",request,e)
 if err!=nil||!first.OutcomeKnown||!strings.Contains(string(first.ResponseJSON),"user-secret-unique"){
  t.Fatalf("initial remote outcome not recorded: %v %+v",err,first)
 }
 second,err:=svc.executeInbound(ctx,"peer",request,e)
 if err!=nil||string(second.ResponseJSON)!=string(first.ResponseJSON)||e.calls.Load()!=1{
  t.Fatalf("same immutable idempotent request re-executed: %v %+v",err,second)
 }
 forged:=request
 forged.RequestJSON=json.RawMessage(`{"prompt":"other-secret"}`)
 out,err:=svc.executeInbound(ctx,"peer",forged,e)
 if err!=nil||out.ErrorCode!="remote_request_identity_conflict"||
  !out.OutcomeKnown||len(out.ResponseJSON)>0||e.calls.Load()!=1{
  t.Fatalf("replayed identifier disclosed an unrelated cached response: %v %+v",err,out)
 }
 forged=request
 forged.DeploymentID="b"
 out,err=svc.executeInbound(ctx,"peer",forged,e)
 if err!=nil||out.ErrorCode!="remote_request_identity_conflict"||
  len(out.ResponseJSON)>0||e.calls.Load()!=1{
  t.Fatalf("different model claimed same request identifier: %v %+v",err,out)
 }
 forged=request
 forged.ContextManifest=json.RawMessage(`{"document":"different-evidence"}`)
 out,err=svc.executeInbound(ctx,"peer",forged,e)
 if err!=nil||out.ErrorCode!="remote_request_identity_conflict"{
  t.Fatalf("reused identifier with new evidence accepted: %v %+v",err,out)
 }
 var storedHash string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT request_hash FROM
  node_federated_inference_receipts WHERE peer_node_id='peer'
  AND remote_request_id=?`,request.RequestID).Scan(&storedHash);err!=nil{t.Fatal(err)}
 if len(storedHash)!=64||strings.Contains(storedHash,"secret"){
  t.Fatalf("request identity did not store a bounded digest: %q",storedHash)
 }
 // Legacy receipts cannot prove prompt/model identity and must never
 // replay possibly sensitive historical outputs after schema upgrade.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE node_federated_inference_receipts
  SET request_hash='' WHERE peer_node_id='peer' AND remote_request_id=?`,request.RequestID);
  err!=nil{t.Fatal(err)}
 out,err=svc.executeInbound(ctx,"peer",request,e)
 if err!=nil||out.ErrorCode!="remote_request_identity_conflict"||len(out.ResponseJSON)>0{
  t.Fatalf("unproven legacy response was leaked: %v %+v",err,out)
 }
}

func TestFederatedInFlightDuplicateDoesNotStartAnotherExecution(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","origin-fp")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(id,name,local,
 identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,
 last_seen_at,revision,created_at,updated_at)
 VALUES('peer','Peer',0,'peer','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
 now,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(id,model_ref,
 modalities_json,static_metadata_json,trust_state,created_at,updated_at)
 VALUES('m','test','["text"]','{}','user_trusted',?,?)`,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(id,
 model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,
 revision,discovered_at,updated_at)
 VALUES('d','m','origin','local','{}','ready','fingerprint',1,?,?)`,
 now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},Identity{},"",30*time.Second)
 ex:=&identityBoundExecutor{entered:make(chan struct{},1),release:make(chan struct{})}
 req:=RemoteInferenceRequest{Protocol:ProtocolVersion,RequestID:"inflight",
 DeploymentID:"d",RequestJSON:json.RawMessage(`{"prompt":"first"}`)}
 completed:=make(chan error,1)
 go func(){_,err:=svc.executeInbound(ctx,"peer",req,ex);completed<-err}()
 select{case <-ex.entered:case <-time.After(3*time.Second):t.Fatal("executor was not entered")}
 unknown,err:=svc.executeInbound(ctx,"peer",req,ex)
 if err!=nil||unknown.ErrorCode!="duplicate_or_unknown_remote_request"||
 unknown.OutcomeKnown||ex.calls.Load()!=1{
  t.Fatalf("concurrent idempotent dispatch was replayed: %v %+v",err,unknown)
 }
 changed:=req
 changed.RequestJSON=json.RawMessage(`{"prompt":"different"}`)
 denied,err:=svc.executeInbound(ctx,"peer",changed,ex)
 if err!=nil||denied.ErrorCode!="remote_request_identity_conflict"||
 len(denied.ResponseJSON)>0||ex.calls.Load()!=1{
  t.Fatalf("concurrent colliding request escaped identity fence: %v %+v",err,denied)
 }
 close(ex.release)
 select{case err:=<-completed:if err!=nil{t.Fatal(err)}
 case <-time.After(3*time.Second):t.Fatal("original execution did not complete")}
 replay,err:=svc.executeInbound(ctx,"peer",req,ex)
 if err!=nil||!replay.OutcomeKnown||len(replay.ResponseJSON)==0||ex.calls.Load()!=1{
  t.Fatalf("completed same-request replay did not use verified receipt: %v %+v",err,replay)
 }
}


type cancelledInboundExecutor struct{
 cancel context.CancelFunc
 fail bool
 calls int
}
func(e *cancelledInboundExecutor)DispatchFederatedLocal(
 _ context.Context,dep,requestID string,raw json.RawMessage)(inference.DispatchResult,error){
 e.calls++
 e.cancel()
 if e.fail{return inference.DispatchResult{},context.Canceled}
 return inference.DispatchResult{ResponseJSON:json.RawMessage(`{"completed":"once"}`),
  UsageJSON:json.RawMessage(`{"tokens":1}`)},nil
}

func TestCancelledInboundFederatedInferencePersistsKnownOrUnknownReceipt(t *testing.T){
 background:=context.Background()
 db,now:=seedNodeDB(t,"node-origin","node-origin-fp")
 defer db.Close()
 for _,stmt:=range []string{
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
   trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('peer','Peer',0,'peer','paired','PAIRED_MTLS','{}','{}',1,1,1,1)`,
  `INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,
   created_at,updated_at) VALUES('m','test/local','["text"]','{}','user_trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,
   status,deployment_fingerprint,revision,discovered_at,updated_at)
   VALUES('d','m','node-origin','local','{}','ready','fp',1,1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(background,stmt);err!=nil{t.Fatal(err)}
 }
 _=now
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"node-origin",Local:true},
  Identity{},"",30*time.Second)
 for _,tc:=range []struct{name string;fail bool;status,code string}{
  {"completed after cancelled request",false,"succeeded",""},
  {"uncertain cancellation",true,"unknown","remote_inference_outcome_unknown"},
 }{
  t.Run(tc.name,func(t *testing.T){
   callCtx,cancel:=context.WithCancel(background)
   executor:=&cancelledInboundExecutor{cancel:cancel,fail:tc.fail}
   request:=RemoteInferenceRequest{
    Protocol:ProtocolVersion,RequestID:tc.name,DeploymentID:"d",
    RequestJSON:json.RawMessage(`{"prompt":"private one-time task"}`),
   }
   first,err:=svc.executeInbound(callCtx,"peer",request,executor)
   if err!=nil||executor.calls!=1{
    t.Fatalf("cancelled inference did not finalise: calls=%d err=%v",executor.calls,err)
   }
   if tc.fail&&(first.OutcomeKnown||first.ErrorCode!=tc.code){
    t.Fatalf("cancelled uncertain execution misrepresented as known: %+v",first)
   }
   var status,code string
   var completed int64
   if err:=db.SQL().QueryRowContext(background,`SELECT status,COALESCE(error_code,''),
    completed_at FROM node_federated_inference_receipts
    WHERE peer_node_id='peer' AND remote_request_id=?`,request.RequestID).
    Scan(&status,&code,&completed);err!=nil||status!=tc.status||
    code!=tc.code||completed<=0{
    t.Fatalf("cancelled receipt was not persisted: %s %s %d err=%v",
     status,code,completed,err)
   }
   retry,err:=svc.executeInbound(background,"peer",request,executor)
   if err!=nil||executor.calls!=1{
    t.Fatalf("cancelled request replay executed work: calls=%d err=%v",executor.calls,err)
   }
   if tc.fail{
    if retry.OutcomeKnown||retry.ErrorCode!="duplicate_or_unknown_remote_request"{
     t.Fatalf("uncertain work silently retried or declared safe: %+v",retry)
    }
   }else if !retry.OutcomeKnown||string(retry.ResponseJSON)!=`{"completed":"once"}`{
    t.Fatalf("known completion was not reused idempotently: %+v",retry)
   }
  })
 }
}
