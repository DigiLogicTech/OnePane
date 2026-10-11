//go:build integration

package nodefederation

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestPairedHeartbeatSequenceCannotReanimateWithdrawnRemoteCapabilities(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/federation-replay.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-fp','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at) VALUES(
  'pair-peer','peer','outbound','paired','token','hash','000000','hash',
  1,1,'sha256:peer','',?,?,1,?,?)`,
  now+60000,now,now,now);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},
  Identity{},"",30*time.Second)
 mk:=func(seq int64,dep,model string)Heartbeat{
  return Heartbeat{Protocol:ProtocolVersion,NodeID:"peer",
   Manifest:CapabilityManifest{
    Protocol:ProtocolVersion,NodeID:"peer",Sequence:seq,Generated:seq,
    Models:[]ModelCapability{{
     RemoteDeploymentID:dep,ModelRef:model,ContextMax:4096,
     Qualification:"verified",ProtocolLevel:"L1",
     Capabilities:[]string{"inference.general"},
    }},
   },
  }
 }
 old:=mk(100,"old-dep","example/withdrawn")
 latest:=mk(200,"current-dep","example/current")
 for _,h:=range []Heartbeat{old,latest}{
  if err:=svc.ReceiveHeartbeat(ctx,"peer",h);err!=nil{t.Fatal(err)}
 }
 oldID:=stableID("feddeploy","peer","old-dep")
 currentID:=stableID("feddeploy","peer","current-dep")
 status:=func(id string)string{
  t.Helper()
  var state string
  if e:=db.SQL().QueryRowContext(ctx,`SELECT status FROM model_deployments
   WHERE id=?`,id).Scan(&state);e!=nil{t.Fatal(e)}
  return state
 }
 if status(oldID)!="unavailable"||status(currentID)!="ready"{
  t.Fatal("latest manifest did not withdraw old remote model")
 }
 var rev,seen int64
 var raw string
 if err=db.SQL().QueryRowContext(ctx,`SELECT revision,manifest_json FROM
  node_capability_manifests WHERE peer_node_id='peer'`).Scan(&rev,&raw);err!=nil{
  t.Fatal(err)
 }
 if err=db.SQL().QueryRowContext(ctx,`SELECT last_seen_at FROM harness_nodes
  WHERE id='peer'`).Scan(&seen);err!=nil{t.Fatal(err)}
 if err=svc.ReceiveHeartbeat(ctx,"peer",old);!errors.Is(err,ErrHeartbeatReplay){
  t.Fatalf("obsolete manifest unexpectedly restored Node capabilities: %v",err)
 }
 conflicting:=mk(200,"forged-dep","example/forged")
 if err=svc.ReceiveHeartbeat(ctx,"peer",conflicting);!errors.Is(err,ErrHeartbeatReplay){
  t.Fatalf("equivocated equal sequence was accepted: %v",err)
 }
 var afterRev,afterSeen int64
 var afterJSON string
 if err=db.SQL().QueryRowContext(ctx,`SELECT revision,manifest_json
  FROM node_capability_manifests WHERE peer_node_id='peer'`).
  Scan(&afterRev,&afterJSON);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,`SELECT last_seen_at FROM harness_nodes
  WHERE id='peer'`).Scan(&afterSeen);err!=nil{t.Fatal(err)}
 if afterRev!=rev||afterSeen!=seen||afterJSON!=raw||
  status(oldID)!="unavailable"||status(currentID)!="ready"{
  t.Fatal("replayed heartbeat modified live model availability or manifest")
 }
 // Exact idempotent keepalive can refresh TTL without re-importing models.
 if err=svc.ReceiveHeartbeat(ctx,"peer",latest);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,`SELECT revision FROM node_capability_manifests
  WHERE peer_node_id='peer'`).Scan(&afterRev);err!=nil{t.Fatal(err)}
 if afterRev!=rev||status(oldID)!="unavailable"{
  t.Fatal("duplicate manifest re-imported stale model capability")
 }
 // A delayed exact duplicate cannot revive an expired peer whose models
 // the expiry sweep already disabled; only a new manifest may re-import.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET expires_at=? WHERE peer_node_id='peer'`,now-1000);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE harness_nodes
  SET trust_state='unavailable' WHERE id='peer'`);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE model_deployments
  SET status='unavailable' WHERE id=?`,currentID);err!=nil{t.Fatal(err)}
 if err=svc.ReceiveHeartbeat(ctx,"peer",latest);!errors.Is(err,ErrHeartbeatReplay){
  t.Fatalf("expired duplicate incorrectly resurrected peer: %v",err)
 }
 if status(currentID)!="unavailable"{t.Fatal("expired duplicate restored remote model")}
 var unavailable string
 if err=db.SQL().QueryRowContext(ctx,`SELECT trust_state FROM harness_nodes WHERE id='peer'`).Scan(&unavailable);
  err!=nil||unavailable!="unavailable"{t.Fatalf("expired duplicate restored Node: %v %s",err,unavailable)}
 // Pairing revocation is also enforced inside the import transaction.
 renewed:=mk(300,"current-dep","example/current")
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_pairings
  SET status='revoked' WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=svc.ReceiveHeartbeat(ctx,"peer",renewed);!errors.Is(err,ErrNodeNotPaired){
  t.Fatalf("revoked pairing was permitted to recover Node: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_pairings
  SET status='paired' WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=svc.ReceiveHeartbeat(ctx,"peer",renewed);err!=nil{
  t.Fatalf("new monotonic manifest could not recover paired Node: %v",err)
 }
 var trust string
 if err=db.SQL().QueryRowContext(ctx,`SELECT trust_state FROM harness_nodes
  WHERE id='peer'`).Scan(&trust);err!=nil||trust!="paired"{
  t.Fatalf("monotonic recovery did not restore paired trust state: %s %v",trust,err)
 }
 if status(currentID)!="ready"{t.Fatal("new manifest did not requalify remote model")}
}
