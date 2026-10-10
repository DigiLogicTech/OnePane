//go:build integration

package nodefederation

import (
 "context"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
)

func TestExpiredNodeSweepCannotUndoRecoveredHeartbeat(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","local-fingerprint")
 defer db.Close()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-fp','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 manifest:=`{"protocol":"onepane-federation-v1","node_id":"peer","models":[{"deployment_id":"remote-dep"}]}`
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO node_capability_manifests(
  peer_node_id,sequence,manifest_json,received_at,expires_at,revision,updated_at)
  VALUES('peer',10,?,?,?,1,?)`,manifest,now,now-1000,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO models(
  id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('model-remote','example/remote','["text"]','{}','user_trusted',?,?)`,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_deployments(
  id,model_id,node_id,runtime_name,runtime_config_json,status,residency_state,
  deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('model-remote-deploy','model-remote','peer','remote-node',
  '{"federation":{"remote_deployment_id":"remote-dep","peer_node_id":"peer"}}',
  'ready','resident','fingerprint',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},
  Identity{},"",30*time.Second)
 // Simulate the outer stale candidate scan finding a peer, followed by
 // an authenticated heartbeat extending the manifest before the write.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET expires_at=? WHERE peer_node_id='peer'`,now+3600000);err!=nil{t.Fatal(err)}
 changed,err:=svc.expireStalePeer(ctx,"peer",now)
 if err!=nil||changed{t.Fatalf("fresh heartbeat was overridden: %t %v",changed,err)}
 var trust,status string
 get:=func(){
  t.Helper()
  if e:=db.SQL().QueryRowContext(ctx,`SELECT trust_state FROM harness_nodes
   WHERE id='peer'`).Scan(&trust);e!=nil{t.Fatal(e)}
  if e:=db.SQL().QueryRowContext(ctx,`SELECT status FROM model_deployments
   WHERE id='model-remote-deploy'`).Scan(&status);e!=nil{t.Fatal(e)}
 }
 get()
 if trust!="paired"||status!="ready"{
  t.Fatalf("live peer was accidentally disabled: %s %s",trust,status)
 }
 // Actually stale, the Node and its imported model change together.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET expires_at=? WHERE peer_node_id='peer'`,now-1000);err!=nil{t.Fatal(err)}
 changed,err=svc.expireStalePeer(ctx,"peer",now)
 if err!=nil||!changed{t.Fatalf("actual stale peer not disabled: %t %v",changed,err)}
 get()
 if trust!="unavailable"||status!="unavailable"{
  t.Fatalf("peer/model expiry not atomic: %s %s",trust,status)
 }
 // No revisions or extra sweeps when already unavailable.
 again,err:=svc.expireStalePeer(ctx,"peer",now)
 if err!=nil||again{t.Fatalf("duplicate unavailable transition: %t %v",again,err)}
 count,err:=svc.ExpireStale(ctx)
 if err!=nil||count!=0{t.Fatalf("repeated sweep wasn't idempotent: %d %v",count,err)}
 // Restore with an authorised fresh manifest but leave the existing
 // pairing intact; expiration is not equivalent to permanent revocation.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE harness_nodes
  SET trust_state='paired' WHERE id='peer'`);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET expires_at=? WHERE peer_node_id='peer'`,time.Now().Add(time.Hour).UnixMilli());err!=nil{t.Fatal(err)}
 changed,err=svc.expireStalePeer(ctx,"peer",now)
 if err!=nil||changed{t.Fatalf("restored Node expired again: %t %v",changed,err)}
 get()
 if trust!="paired"{t.Fatal("restored paired peer was lost")}
}
