//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
)

func TestOldFederatedResponseCacheRedactionPreservesIdempotencyTombstones(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"local","local-fp")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(id,name,local,
  identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,
  last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-fp','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(id,model_ref,
  modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('m','local/model','["text"]','{}','user_trusted',?,?)`,
  now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(id,
  model_id,node_id,runtime_name,runtime_config_json,status,
  deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('d','m','local','local','{}','ready','fp',1,?,?)`,
  now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"local",Local:true},Identity{},"",30*time.Second)
 request:=RemoteInferenceRequest{Protocol:ProtocolVersion,RequestID:"retained-id",
  DeploymentID:"d",RequestJSON:json.RawMessage(`{"prompt":"do not replay"}`)}
 digest,err:=inboundRequestHash(request)
 if err!=nil{t.Fatal(err)}
 expired:=now-8*24*60*60*1000
 for i:=0;i<130;i++{
  id:= "historical-"+strings.Repeat("x",i%3)+"-"+string(rune(0x1000+i))
  if i==0{id=request.RequestID}
  if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_federated_inference_receipts(
   id,peer_node_id,remote_request_id,deployment_id,request_hash,status,
   response_json,usage_json,created_at,completed_at)
   VALUES(?, 'peer',?,'d',?,'succeeded',
    '{"completion":"cached private text"}','{"tokens":13}',?,?)`,
   id,id,digest,expired-1000,expired);err!=nil{t.Fatal(err)}
 }
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_federated_inference_receipts(
  id,peer_node_id,remote_request_id,deployment_id,request_hash,status,
  response_json,usage_json,created_at,completed_at)
  VALUES('new','peer','new','d',?,'succeeded','{"keep":true}','{}',?,?)`,
  digest,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_federated_inference_receipts(
  id,peer_node_id,remote_request_id,deployment_id,request_hash,status,
  response_json,usage_json,created_at,completed_at)
  VALUES('unknown','peer','unknown','d',?,'unknown',NULL,NULL,?,?)`,
  digest,expired-1000,expired);err!=nil{t.Fatal(err)}
 n,err:=svc.PruneCachedInferenceResponses(ctx,DefaultInboundReceiptRetention)
 if err!=nil||n!=128{t.Fatalf("bounded redaction batch: %d %v",n,err)}
 n,err=svc.PruneCachedInferenceResponses(ctx,DefaultInboundReceiptRetention)
 if err!=nil||n!=2{t.Fatalf("redaction remainder: %d %v",n,err)}
 n,err=svc.PruneCachedInferenceResponses(ctx,DefaultInboundReceiptRetention)
 if err!=nil||n!=0{t.Fatalf("redaction not idempotent: %d %v",n,err)}
 var code,status,storedHash string
 var response,usage any
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status,request_hash,
  error_code,response_json,usage_json
  FROM node_federated_inference_receipts WHERE id=?`,request.RequestID).
  Scan(&status,&storedHash,&code,&response,&usage);err!=nil{t.Fatal(err)}
 if status!="succeeded"||storedHash!=digest||code!="remote_cached_response_expired"||
  response!=nil||usage!=nil{
  t.Fatalf("original idempotency tombstone deleted or leaked: %s %s %s %v %v",
   status,storedHash,code,response,usage)
 }
 executor:=&identityBoundExecutor{}
 replay,err:=svc.executeInbound(ctx,"peer",request,executor)
 if err!=nil||replay.OutcomeKnown!=true||
  replay.ErrorCode!="remote_cached_response_expired"||
  len(replay.ResponseJSON)>0||executor.calls.Load()!=0{
  t.Fatalf("expired receipt re-executed/pretended success: %+v %v",replay,err)
 }
 var latestResponse string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT response_json
  FROM node_federated_inference_receipts WHERE id='new'`).Scan(&latestResponse);err!=nil{t.Fatal(err)}
 if latestResponse!=`{"keep":true}`{t.Fatal("recent receipt incorrectly redacted")}
 for _,duration:=range []time.Duration{0,1*time.Minute,366*24*time.Hour}{
  if n,err:=svc.PruneCachedInferenceResponses(ctx,duration);err==nil||n!=0{
   t.Fatalf("invalid retention accepted %s: %d %v",duration,n,err)
  }
 }
}
