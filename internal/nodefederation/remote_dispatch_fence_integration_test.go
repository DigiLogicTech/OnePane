//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/inference"
)

func TestRemoteInferenceRequiresCurrentExactPairedNodeManifestAtDispatch(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","origin-fingerprint")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  endpoint_json,protocol_json,capabilities_json,last_seen_at,
  revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-fp','paired','PAIRED_MTLS','{}','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at)
  VALUES('pair','peer','outbound','paired','token','hashed','000000',
  'hashed',1,1,'sha256:cert','',?,?,1,?,?)`,
  now+60000,now,now,now);err!=nil{t.Fatal(err)}
 manifest:=`{"protocol":"onepane-federation-v1","node_id":"peer","models":[{"deployment_id":"remote-dep"}]}`
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_capability_manifests(
  peer_node_id,sequence,manifest_json,received_at,expires_at,revision,updated_at)
  VALUES('peer',100,?,?,?,1,?)`,manifest,now,now+30000,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(
  id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('model','example/pinned','["text"]','{}','user_trusted',?,?)`,now,now);err!=nil{t.Fatal(err)}
 cfg:=`{"federation":{"peer_node_id":"peer","remote_deployment_id":"remote-dep"}}`
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(
  id,model_id,node_id,runtime_name,runtime_config_json,status,
  deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('imported-deployment','model','peer','remote-node',?,'ready','fingerprint',1,?,?)`,
  cfg,now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},
  Identity{},"",30*time.Second)
 admit:=func(want bool){
  t.Helper()
  err:=svc.authorizeRemoteModelDispatch(ctx,"peer","imported-deployment","remote-dep")
  if want&&err!=nil{t.Fatalf("admitted remote capability rejected: %v",err)}
  if !want&&!errors.Is(err,ErrFederatedDispatchNotAdmitted){
   t.Fatalf("unadmitted remote capability was accepted: %v",err)
  }
 }
 edit:=func(q string,args ...any){
  t.Helper()
  if _,err:=db.SQL().ExecContext(ctx,q,args...);err!=nil{t.Fatal(err)}
 }
 admit(true)
 edit(`UPDATE harness_nodes SET endpoint_json='{"federation_url":"https://127.0.0.1:7443"}' WHERE id='peer'`)
 if _,_,err:=svc.pairedEndpoint(ctx,"peer");err!=nil{
  t.Fatalf("fully paired Node endpoint not available: %v",err)
 }
 edit(`UPDATE node_pairings SET local_confirmed=0 WHERE peer_node_id='peer'`)
 if _,_,err:=svc.pairedEndpoint(ctx,"peer");err==nil{
  t.Fatal("one-sided paired Node endpoint incorrectly allowed")
 }
 edit(`UPDATE node_pairings SET local_confirmed=1,peer_confirmed=0 WHERE peer_node_id='peer'`)
 if _,_,err:=svc.pairedEndpoint(ctx,"peer");err==nil{
  t.Fatal("reverse one-sided pairing incorrectly allowed endpoint")
 }
 edit(`UPDATE node_pairings SET peer_confirmed=1 WHERE peer_node_id='peer'`)
 if _,_,err:=svc.pairedEndpoint(ctx,"peer");err!=nil{
  t.Fatalf("restored two-way approved endpoint denied: %v",err)
 }
 admit(true)
 // An operator can disable remote compute after the model was imported,
 // verified and scheduled. The last transport must still stop pre-network.
 edit(`INSERT INTO remote_node_compute_policies(
   node_id,enabled,updated_by,created_at,updated_at)
   VALUES('peer',0,'operator',?,?)`,now,now)
 admit(false)
 edit(`UPDATE remote_node_compute_policies SET enabled=1 WHERE node_id='peer'`)
 admit(true)
 edit(`UPDATE remote_node_compute_policies SET enabled=0 WHERE node_id='peer'`)
 _,policyErr:=(RemoteTransport{Service:svc}).Dispatch(ctx,inference.DispatchRequest{
  RequestID:"policy-revoked-no-send",
  Deployment:inference.ModelDeployment{ID:"imported-deployment",
   RuntimeConfigJSON:json.RawMessage(cfg)},
  RequestJSON:json.RawMessage(`{"prompt":"never exported"}`),
 },nil)
 var policyDenied *inference.TransportError
 if !errors.As(policyErr,&policyDenied)||!policyDenied.OutcomeKnown||
  policyDenied.Code!="remote_node_not_admitted"{
  t.Fatalf("disabled Node compute policy was bypassed by transport: %v",policyErr)
 }
 edit(`UPDATE remote_node_compute_policies SET enabled=1 WHERE node_id='peer'`)
 admit(true)
 edit(`UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'`,now-1000)
 admit(false)
 // The remote transport itself must refuse before pairedEndpoint/network
 // dispatch; a missing endpoint is irrelevant when admission is denied.
 request:=inference.DispatchRequest{
  RequestID:"request-private-never-sent",
  Deployment:inference.ModelDeployment{ID:"imported-deployment",
   RuntimeConfigJSON:json.RawMessage(cfg)},
  RequestJSON:json.RawMessage(`{"prompt":"sensitive-federated-prompt"}`),
 }
 _,err:= (RemoteTransport{Service:svc}).Dispatch(ctx,request,nil)
 var denied *inference.TransportError
 if !errors.As(err,&denied)||!denied.OutcomeKnown||
  denied.Code!="remote_node_not_admitted"{
  t.Fatalf("remote transport did not fail pre-send on expired manifest: %v",err)
 }
 edit(`UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'`,now+3600000)
 admit(true)
 edit(`UPDATE node_pairings SET status='revoked' WHERE peer_node_id='peer'`)
 admit(false)
 edit(`UPDATE node_pairings SET status='paired' WHERE peer_node_id='peer'`)
 admit(true)
 // A stored "paired" label cannot substitute for BOTH confirmed consent
 // directions. A revoked confirmation must stop prompt dispatch pre-send.
 edit(`UPDATE node_pairings SET local_confirmed=0 WHERE peer_node_id='peer'`)
 admit(false)
 _,err=(RemoteTransport{Service:svc}).Dispatch(ctx,request,nil)
 if !errors.As(err,&denied)||!denied.OutcomeKnown||
  denied.Code!="remote_node_not_admitted"{
  t.Fatalf("unconfirmed Node got inference request: %v",err)
 }
 edit(`UPDATE node_pairings SET local_confirmed=1,peer_confirmed=0 WHERE peer_node_id='peer'`)
 admit(false)
 edit(`UPDATE node_pairings SET peer_confirmed=1 WHERE peer_node_id='peer'`)
 admit(true)
 edit(`UPDATE harness_nodes SET trust_state='unavailable' WHERE id='peer'`)
 admit(false)
 edit(`UPDATE harness_nodes SET trust_state='paired' WHERE id='peer'`)
 edit(`UPDATE node_capability_manifests SET manifest_json=? WHERE peer_node_id='peer'`,
  `{"protocol":"onepane-federation-v1","node_id":"peer","models":[]}`)
 admit(false)
 edit(`UPDATE node_capability_manifests SET manifest_json=? WHERE peer_node_id='peer'`,manifest)
 admit(true)
 edit(`UPDATE model_deployments SET status='unavailable' WHERE id='imported-deployment'`)
 admit(false)
 edit(`UPDATE model_deployments SET status='ready' WHERE id='imported-deployment'`)
 edit(`UPDATE models SET trust_state='revoked' WHERE id='model'`)
 admit(false)
 edit(`UPDATE models SET trust_state='user_trusted' WHERE id='model'`)
 edit(`UPDATE model_deployments SET runtime_config_json='{"federation":{"peer_node_id":"peer","remote_deployment_id":"different-dep"}}' WHERE id='imported-deployment'`)
 admit(false)
}
