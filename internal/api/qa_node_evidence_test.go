package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "path/filepath"
 "strings"
 "testing"

 _ "modernc.org/sqlite"
)

func qaNodeFixture(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"node-report.db"))
 if err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `CREATE TABLE harness_nodes(id TEXT PRIMARY KEY,local INTEGER,trust_state TEXT,
    last_seen_at INTEGER,updated_at INTEGER,name TEXT,endpoint_json TEXT)`,
  `CREATE TABLE node_capability_manifests(peer_node_id TEXT PRIMARY KEY,sequence INTEGER,
    received_at INTEGER,expires_at INTEGER,manifest_json TEXT)`,
  `CREATE TABLE node_pairings(peer_node_id TEXT PRIMARY KEY,status TEXT,
    pairing_token TEXT,pairing_code TEXT,peer_certificate_pem TEXT)`,
  `CREATE TABLE node_wake_attempts(id TEXT PRIMARY KEY,node_id TEXT,state TEXT,
    requested_at TEXT,failure TEXT,task_id TEXT,wake_targets_json TEXT)`,
  `CREATE TABLE node_federated_inference_receipts(id TEXT PRIMARY KEY,peer_node_id TEXT,
    status TEXT,error_code TEXT,response_json TEXT,remote_request_id TEXT)`,
 }{if _,err:=db.Exec(q);err!=nil{_ = db.Close();t.Fatal(err)}}
 return db
}

func TestQANodeEvidenceOnlyContainsAdminSelectedNodeMetadata(t *testing.T){
 ctx:=context.Background()
 db:=qaNodeFixture(t);defer db.Close()
 const secret="PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"
 for _,q:=range []string{
  `INSERT INTO harness_nodes VALUES
   ('node-a',0,'paired',1000,1500,'PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','{"url":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}'),
   ('node-b',0,'revoked',1100,1600,'PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','{"url":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}')`,
  `INSERT INTO node_capability_manifests VALUES
   ('node-a',5,1200,1400,'{"gpu":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}'),
   ('node-b',9,1500,1800,'{"gpu":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}')`,
  `INSERT INTO node_pairings VALUES
   ('node-a','paired','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY'),
   ('node-b','revoked','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY')`,
  `INSERT INTO node_wake_attempts VALUES
   ('wake-a','node-a','failed','2026-10-10T01:00:00Z','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY'),
   ('wake-b','node-a','ready','2026-10-10T02:00:00Z','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY'),
   ('wake-c','node-b','timed_out','2026-10-10T03:00:00Z','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY')`,
  `INSERT INTO node_federated_inference_receipts VALUES
   ('r1','node-a','succeeded','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','{"answer":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY'),
   ('r2','node-a','failed','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','{"answer":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY'),
   ('r3','node-b','unknown','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY','{"answer":"PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY"}','PRIVATE_PAIR_CERT_ENDPOINT_PAYLOAD_CANARY')`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 got,err:=loadQANodeEvidence(ctx,db,"node-a")
 if err!=nil{t.Fatal(err)}
 if got.SchemaVersion!=2||got.NodeRef==""||got.NodeRef=="node-a"||
  got.TrustState!="paired"||got.IsLocal||got.LastSeenAt==nil||*got.LastSeenAt!=1000||
  got.ManifestState!="recorded_expiry_unverified"||
  got.ManifestSequence==nil||*got.ManifestSequence!=5||
  got.PairingStatus!="paired"||got.ServiceState!="not_collected"{
  t.Fatalf("Node evidence did not match selected Node %+v",got)
 }
 if got.WakeAttempts.TotalRecorded!=2||got.WakeAttempts.Ready!=1||
  got.WakeAttempts.Failed!=1||got.WakeAttempts.TimedOut!=0||
  got.WakeAttempts.LastKnownState!="ready"{
  t.Fatalf("wrong Node wake summary %+v",got.WakeAttempts)
 }
 if got.InferenceReceipts.TotalRecorded!=2||got.InferenceReceipts.Failed!=1||
  got.InferenceReceipts.Succeeded!=1||got.InferenceReceipts.Unknown!=0{
  t.Fatalf("other Node's receipts leaked into result %+v",got.InferenceReceipts)
 }
 raw,err:=json.Marshal(got)
 if err!=nil{t.Fatal(err)}
 for _,s:=range []string{secret,"node-a","node-b","wake-a","wake-c","response_json","pairing_token","endpoint_json",
  "peer_certificate_pem","failure","error_code","task_id","node_name"}{
  if strings.Contains(string(raw),s){t.Fatalf("sensitive Node detail leaked: %s, report: %s",s,raw)}
 }
 none,err:=loadQANodeEvidence(ctx,db,"node-nonexistent")
 if !errors.Is(err,sql.ErrNoRows){t.Fatalf("missing Node must not enumerate inventory: %+v %v",none,err)}
}

func TestQANodeEvidenceMissingManifestAndServicesAreExplicit(t *testing.T){
 ctx:=context.Background()
 db:=qaNodeFixture(t);defer db.Close()
 if _,err:=db.ExecContext(ctx,`INSERT INTO harness_nodes(id,local,trust_state,updated_at,name)
 VALUES('local',1,'local',100,'machine')`);err!=nil{t.Fatal(err)}
 got,err:=loadQANodeEvidence(ctx,db,"local")
 if err!=nil{t.Fatal(err)}
 if !got.IsLocal||got.LastSeenAt!=nil||got.PairingStatus!="not_recorded"||
  got.ManifestState!="not_recorded"||got.ManifestSequence!=nil||
  got.ServiceState!="not_collected"||got.WakeAttempts.LastKnownState!="not_recorded"{
  t.Fatalf("missing Node evidence misreported as healthy: %+v",got)
 }
 if got.InferenceReceipts.TotalRecorded!=0||got.WakeAttempts.TotalRecorded!=0{
  t.Fatal("missing metrics fabricated")
 }
 if qaNodeTrust("PRIVATE_API_KEY")!="unavailable"||qaNodePairing("TOKEN")!="not_recorded"||
  qaNodeWakeState("shell-failed")!="not_recorded"{
  t.Fatal("free-text state was not allowlisted")
 }
}
