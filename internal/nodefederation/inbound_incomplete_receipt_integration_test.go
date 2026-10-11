//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
)

func TestFederatedMissingCachedOutcomeNeverBecomesSuccessOrReexecutes(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","origin-fp")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,
  capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-fp','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(
  id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('model','local/example','["text"]','{}','user_trusted',?,?)`,
  now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(
  id,model_id,node_id,runtime_name,runtime_config_json,status,
  deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('deployment','model','origin','local','{}','ready','sha',1,?,?)`,
  now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},Identity{},"",30*time.Second)
 exec:=&identityBoundExecutor{}
 req:=RemoteInferenceRequest{Protocol:ProtocolVersion,RequestID:"cached",
  DeploymentID:"deployment",RequestJSON:json.RawMessage(`{"prompt":"test"}`)}
 first,err:=svc.executeInbound(ctx,"peer",req,exec)
 if err!=nil||!first.OutcomeKnown||len(first.ResponseJSON)==0||exec.calls.Load()!=1{
  t.Fatalf("expected one original execution: %+v %v",first,err)
 }
 for _,tt:=range []struct{
  name,status string
  cached,errorCode *string
  want string
 }{
  {name:"missing successful response",status:"succeeded",want:"remote_cached_response_unavailable"},
  {name:"expired successful response",status:"succeeded",errorCode:receiptTestStringPtr("remote_cached_response_expired"),want:"remote_cached_response_expired"},
  {name:"failed receipt without failure code",status:"failed",want:"remote_receipt_unavailable"},
  {name:"failed receipt with explicit code",status:"failed",errorCode:receiptTestStringPtr("remote_inference_failed"),want:"remote_inference_failed"},
 }{
  t.Run(tt.name,func(t *testing.T){
   if _,err:=db.SQL().ExecContext(ctx,`UPDATE node_federated_inference_receipts
    SET status=?,response_json=?,usage_json=NULL,error_code=?
    WHERE peer_node_id='peer' AND remote_request_id='cached'`,
    tt.status,tt.cached,tt.errorCode);err!=nil{t.Fatal(err)}
   out,err:=svc.executeInbound(ctx,"peer",req,exec)
   if err!=nil||!out.OutcomeKnown||out.ErrorCode!=tt.want||
    len(out.ResponseJSON)>0||len(out.UsageJSON)>0||exec.calls.Load()!=1{
    t.Fatalf("missing receipt returned success or was reexecuted: %+v err=%v calls=%d",
     out,err,exec.calls.Load())
   }
  })
 }
}

func receiptTestStringPtr(s string)*string{return &s}
