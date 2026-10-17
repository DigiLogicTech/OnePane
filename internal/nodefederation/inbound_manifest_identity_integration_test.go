//go:build integration
package nodefederation

import (
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
)

func TestInboundInferenceRequiresExactPairedPeerManifest(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"origin","origin-fp")
 defer db.Close()
 for _,q:=range []struct{statement string;args []any}{
  {`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
   trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('peer','Peer',0,'peer','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,[]any{now,now,now}},
  {`INSERT INTO node_pairings(id,peer_node_id,direction,status,
   pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,
   local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,
   expires_at,paired_at,revision,created_at,updated_at)
   VALUES('pair','peer','inbound','paired','token','hash','012345','hash',
   1,1,'sha256:peer','',?,?,1,?,?)`,[]any{now+60000,now,now,now}},
  {`INSERT INTO node_capability_manifests(peer_node_id,sequence,manifest_json,
   received_at,expires_at,revision,updated_at)
   VALUES('peer',1,?,?,?,1,?)`,[]any{
   `{"node_id":"peer","protocol":"onepane-federation-v1","models":[]}`,now,now+30000,now}},
 }{
  if _,err:=db.SQL().ExecContext(ctx,q.statement,q.args...);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},
  Identity{},"",30*time.Second)
 check:=func(name string,want bool){
  t.Helper()
  got:=svc.peerAdmittedForInboundInference(ctx,"peer")
  if got!=want{t.Fatalf("%s: admitted=%v, want=%v",name,got,want)}
 }
 set:=func(sql string,args ...any){
  t.Helper()
  if _,err:=db.SQL().ExecContext(ctx,sql,args...);err!=nil{t.Fatal(err)}
 }
 check("matching live paired manifest",true)
 set(`UPDATE node_capability_manifests SET manifest_json=?
      WHERE peer_node_id='peer'`,
      `{"node_id":"another-peer","protocol":"onepane-federation-v1","models":[]}`)
 check("wrong origin",false)
 set(`UPDATE node_capability_manifests SET manifest_json=?
      WHERE peer_node_id='peer'`,
      `{"node_id":"peer","protocol":"other","models":[]}`)
 check("wrong protocol",false)
 set(`UPDATE node_capability_manifests SET manifest_json='{"models":[]}'
      WHERE peer_node_id='peer'`)
 check("missing origin and protocol",false)
 set(`UPDATE node_capability_manifests SET manifest_json=?
      WHERE peer_node_id='peer'`,
      `{"node_id":"peer","protocol":"onepane-federation-v1","models":[]}`)
 set(`UPDATE node_capability_manifests SET sequence=0 WHERE peer_node_id='peer'`)
 check("zero sequence",false)
 set(`UPDATE node_capability_manifests SET sequence=1,received_at=0 WHERE peer_node_id='peer'`)
 check("no reception timestamp",false)
 set(`UPDATE node_capability_manifests SET received_at=?,expires_at=? WHERE peer_node_id='peer'`,now,now-1000)
 check("expired",false)
 set(`UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'`,now+30000)
 check("restored",true)
 set(`UPDATE node_pairings SET peer_confirmed=0 WHERE peer_node_id='peer'`)
 check("withdrawn human pairing",false)
 set(`UPDATE node_pairings SET peer_confirmed=1 WHERE peer_node_id='peer'`)
 set(`UPDATE harness_nodes SET trust_state='unavailable' WHERE id='peer'`)
 check("unavailable peer",false)
}
