//go:build integration

package nodefederation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// The test fixture exercises only inference admission, not the separately
// governed remote install/management surface. Production uses LocalAI.
type admittedLocalFederationModel struct{LocalModelManager}
func(admittedLocalFederationModel)FederatedInferenceAllowed(_ context.Context, deploymentID string)bool{
 return deploymentID=="local-dep"
}

type noSecrets struct{}

func (noSecrets) Resolve(context.Context, string) (string, error) { return "", nil }

func seedNodeDB(t *testing.T, nodeID, fp string) (*sqliteStore.DB, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/onepane.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES(?,?,1,?,'local','LOCAL_TRUSTED','{}','{}',?,1,?,?)`, nodeID, nodeID, fp, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return db, now
}

func TestPairedNodeManifestBecomesSchedulerCandidate(t *testing.T) {
	ctx := context.Background()
	db, now := seedNodeDB(t, "origin", "origin-fp")
	defer db.Close()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,endpoint_json,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('peer','peer',0,'peer-fp','paired','PAIRED_MTLS','{"federation_url":"https://127.0.0.1:9","tls_fingerprint":"sha256:dead"}','{}','{}',?,1,?,?)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	// A discovered Node is not eligible for remote compute until there is
	// an explicitly confirmed pairing, not merely a TRUST_STATE string.
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,revision,created_at,updated_at) VALUES('pair-peer','peer','outbound','paired','token','hashed','000000','hashed',1,1,'sha256:dead','',?,?,1,?,?)`, now+60000, now, now, now); err != nil {
		t.Fatal(err)
	}
	ident, err := EnsureIdentity(t.TempDir(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, clock.Real{}, NodeView{ID: "origin", Name: "origin", Local: true, IdentityFingerprint: "origin-fp", TrustState: "local"}, ident, "https://127.0.0.1:18443", 30*time.Second)
	manifest := CapabilityManifest{Protocol: ProtocolVersion, NodeID: "peer", Sequence: 1, Generated: now, Models: []ModelCapability{{RemoteDeploymentID: "dep-peer", ModelRef: "example/remote", ContextMax: 32768, Qualification: "verified", ProtocolLevel: "L1", Capabilities: []string{"reasoning"}}}}
	if err := svc.ReceiveHeartbeat(ctx, "peer", Heartbeat{Protocol: ProtocolVersion, NodeID: "peer", Manifest: manifest}); err != nil {
		t.Fatal(err)
	}
	var ws = "ws"
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?, 'active',1,?,?)`, ws, "Test", now, now)
	if err != nil {
		t.Fatal(err)
	}
	cat := scheduler.NewCatalog(db.SQL())
	cands, err := cat.Candidates(ctx, ws, "reasoning", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cands {
		if c.Kind == scheduler.CandidateModel && c.DisplayName == "example/remote" {
			found = true
			if !c.Schedulable || !c.TrustedNodeVerified {
				t.Fatalf("paired remote candidate has no verified trust: %+v", c)
			}
			if c.Destination.Kind != "trusted_node" {
				t.Fatalf("destination=%s", c.Destination.Kind)
			}
		}
	}
	if !found {
		t.Fatal("remote model candidate not imported")
	}
	// A READY deployment is only a stored historical record. Eligibility
	// requires a live paired peer with a nonexpired capability manifest.
	check := func(label string, schedulable bool) {
		t.Helper()
		items, err := cat.Candidates(ctx, ws, "reasoning", "")
		if err != nil { t.Fatal(err) }
		for _, candidate := range items {
			if candidate.DisplayName == "example/remote" {
				if candidate.Schedulable != schedulable || candidate.TrustedNodeVerified != schedulable {
					t.Fatalf("%s: remote status=%t verified=%t, want %t", label,
						candidate.Schedulable, candidate.TrustedNodeVerified, schedulable)
				}
				return
			}
		}
		t.Fatalf("%s: imported remote missing", label)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'`, now-1000); err != nil { t.Fatal(err) }
	check("expired heartbeat", false)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'`, now+3600000); err != nil { t.Fatal(err) }
	check("fresh paired heartbeat", true)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE node_pairings SET status='revoked' WHERE peer_node_id='peer'`); err != nil { t.Fatal(err) }
	check("revoked pairing", false)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE node_pairings SET status='paired' WHERE peer_node_id='peer'`); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `UPDATE harness_nodes SET trust_state='unavailable' WHERE id='peer'`); err != nil { t.Fatal(err) }
	check("unavailable peer", false)
}

func TestRemoteTransportUsesMTLSAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbA, now := seedNodeDB(t, "node-a", "seed-a")
	defer dbA.Close()
	dbB, _ := seedNodeDB(t, "node-b", "seed-b")
	defer dbB.Close()
	identA, err := EnsureIdentity(t.TempDir(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbB.SQL().ExecContext(ctx, `INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model-local','fake/local','["text"]','{}','user_trusted',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := dbB.SQL().ExecContext(ctx, `INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,residency_state,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('local-dep','model-local','node-b','fake','{}','ready','resident','local-fp',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	identB, err := EnsureIdentity(t.TempDir(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	// B has one trusted local fake model.
	infB := inference.NewService(dbB.SQL(), dbB, clock.Real{})
	reg := inference.NewTransportRegistry()
	if err := inference.RegisterBuiltinTransports(reg); err != nil {
		t.Fatal(err)
	}
	if err := infB.ConfigureExecution(nil, reg, noSecrets{}, "node-b"); err == nil {
		t.Fatal("expected artifact requirement")
	}
	// Federated local dispatch only needs transports; configure with a tiny real artifact service via normal integration setup is intentionally exercised elsewhere. Set internals through ConfigureExecution requires it, so seed through public catalog then use a separately configured service is covered by inference integration tests.
	_ = infB
	// Validate pinned pairing state and server client-certificate authentication independently of model execution.
	svcA := NewService(dbA.SQL(), dbA, clock.Real{}, NodeView{ID: "node-a", Name: "A", Local: true, IdentityFingerprint: "seed-a", TrustState: "local"}, identA, "https://127.0.0.1:1", 30*time.Second)
	svcB := NewService(dbB.SQL(), dbB, clock.Real{}, NodeView{ID: "node-b", Name: "B", Local: true, IdentityFingerprint: "seed-b", TrustState: "local"}, identB, "https://127.0.0.1:1", 30*time.Second)
	for _, x := range []struct {
		db       *sqliteStore.DB
		peer, id string
		ident    Identity
	}{{dbA, "node-b", "pair-a", identB}, {dbB, "node-a", "pair-b", identA}} {
		_, err := x.db.SQL().ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,endpoint_json,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES(?,?,0,?,'paired','PAIRED_MTLS','{}','{}','{}',?,1,?,?)`, x.peer, x.peer, "remote-"+x.peer, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		_, err = x.db.SQL().ExecContext(ctx, `INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,revision,created_at,updated_at) VALUES(?,?, 'outbound','paired','t','h','000000','h',1,1,?,?,?, ?,1,?,?)`, x.id, x.peer, x.ident.Fingerprint, string(x.ident.CertPEM), now+60000, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	manifestJSON:=`{"protocol":"onepane-federation-v1","node_id":"node-a","models":[]}`
	if _,err:=dbB.SQL().ExecContext(ctx,`INSERT INTO node_capability_manifests(
	 peer_node_id,sequence,manifest_json,received_at,expires_at,revision,updated_at)
	 VALUES('node-a',1,?,?,?,1,?)`,manifestJSON,now,now+3600000,now);err!=nil{t.Fatal(err)}
	type echoExec struct{}
	_ = echoExec{}
	server := NewServer(svcB, echoExecutor{}, admittedLocalFederationModel{})
	ts := httptest.NewUnstartedServer(server.Handler())
	ts.TLS = server.TLSConfig()
	ts.StartTLS()
	defer ts.Close()
	// Point A's peer record at B's actual test endpoint while retaining B's pinned cert fingerprint.
	ep, _ := json.Marshal(map[string]any{"federation_url": ts.URL, "tls_fingerprint": identB.Fingerprint, "protocol": ProtocolVersion})
	_, err = dbA.SQL().ExecContext(ctx, `UPDATE harness_nodes SET endpoint_json=? WHERE id='node-b'`, string(ep))
	if err != nil {
		t.Fatal(err)
	}
	var out RemoteInferenceResponse
	in := RemoteInferenceRequest{Protocol: ProtocolVersion, RequestID: "same-request", DeploymentID: "local-dep", RequestJSON: json.RawMessage(`{"prompt":"hello"}`)}
	if err := postJSON(ctx, svcA.pinnedClient(identB.Fingerprint, true), ts.URL+"/federation/v1/inference", in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.ResponseJSON), "hello") {
		t.Fatalf("response=%s", out.ResponseJSON)
	}
	var out2 RemoteInferenceResponse
	if err := postJSON(ctx, svcA.pinnedClient(identB.Fingerprint, true), ts.URL+"/federation/v1/inference", in, &out2); err != nil {
		t.Fatal(err)
	}
	if string(out.ResponseJSON) != string(out2.ResponseJSON) {
		t.Fatal("idempotent replay changed response")
	}
	// Complete local scheduling -> source imported model -> pinned mTLS ->
	// receiver admission -> original one-shot SQLite receipt -> reply decode.
	// This remains hosted loopback, not an independently possessed device.
	if _,err:=dbA.SQL().ExecContext(ctx,`INSERT INTO models(id,model_ref,
	 modalities_json,static_metadata_json,trust_state,created_at,updated_at)
	 VALUES('imported-model','example/remote','["text"]','{}','user_trusted',?,?)`,
	 now,now);err!=nil{t.Fatal(err)}
	cfg:=`{"federation":{"peer_node_id":"node-b","remote_deployment_id":"local-dep"}}`
	if _,err:=dbA.SQL().ExecContext(ctx,`INSERT INTO model_deployments(
	 id,model_id,node_id,runtime_name,runtime_config_json,status,
	 deployment_fingerprint,revision,discovered_at,updated_at)
	 VALUES('imported-dep','imported-model','node-b','remote-node',?,
	 'ready','imported-fp',1,?,?)`,cfg,now,now);err!=nil{t.Fatal(err)}
	if _,err:=dbA.SQL().ExecContext(ctx,`INSERT INTO node_capability_manifests(
	 peer_node_id,sequence,manifest_json,received_at,expires_at,revision,updated_at)
	 VALUES('node-b',1,?,?,?,1,?)`,
	 `{"node_id":"node-b","protocol":"onepane-federation-v1","models":[{"deployment_id":"local-dep"}]}`,
	 now,now+3600000,now);err!=nil{t.Fatal(err)}
	sent:=inference.DispatchRequest{
	 RequestID:"routed-through-transport",
	 Deployment:inference.ModelDeployment{ID:"imported-dep",
	  RuntimeConfigJSON:json.RawMessage(cfg)},
	 RequestJSON:json.RawMessage(`{"prompt":"private once"}`),
	}
	modelTransport:=RemoteTransport{Service:svcA}
	first,err:=modelTransport.Dispatch(ctx,sent,nil)
	if err!=nil||!strings.Contains(string(first.ResponseJSON),"private once"){
	 t.Fatalf("model transport failed genuine dual Node loopback: %v %+v",err,first)
	}
	cached,err:=modelTransport.Dispatch(ctx,sent,nil)
	if err!=nil||string(cached.ResponseJSON)!=string(first.ResponseJSON){
	 t.Fatalf("remote replay did not preserve exact result: %v %+v",err,cached)
	}
	var receipts int
	if err:=dbB.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM
	 node_federated_inference_receipts WHERE peer_node_id='node-a'
	 AND remote_request_id='routed-through-transport'`).Scan(&receipts);err!=nil{
	 t.Fatal(err)
	}
	if receipts!=1{t.Fatalf("replayed remote call made %d receipts",receipts)}
	// In-flight routing decisions are not consent. The last outbound
	// transport must recheck CURRENT pairing before sending a new prompt.
	if _,err:=dbA.SQL().ExecContext(ctx,`UPDATE node_pairings
	 SET local_confirmed=0 WHERE peer_node_id='node-b'`);err!=nil{t.Fatal(err)}
	revokedRequest:=sent
	revokedRequest.RequestID="withdrawn-consent-never-sent"
	_,err=modelTransport.Dispatch(ctx,revokedRequest,nil)
	var transportDenied *inference.TransportError
	if !errors.As(err,&transportDenied)||!transportDenied.OutcomeKnown||
	 transportDenied.Code!="remote_node_not_admitted"{
	 t.Fatalf("withdrawn local pairing did not block pre-network: %v",err)
	}
	if _,err:=dbA.SQL().ExecContext(ctx,`UPDATE node_pairings
	 SET local_confirmed=1 WHERE peer_node_id='node-b'`);err!=nil{t.Fatal(err)}
	if err:=dbB.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM
	 node_federated_inference_receipts
	 WHERE remote_request_id='withdrawn-consent-never-sent'`).Scan(&receipts);
	 err!=nil||receipts!=0{
	 t.Fatalf("revoked transfer unexpectedly reached remote execution: count=%d err=%v",receipts,err)
	}
	// A live paired mTLS peer must not gain inference authority if the
	// receiver's local admission manager disappears after startup.
	// The receiver must reject a lapsed heartbeat even if TLS is paired.
	if _,err:=dbB.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
	 SET expires_at=? WHERE peer_node_id='node-a'`,now-1000);err!=nil{t.Fatal(err)}
	stale:=in
	stale.RequestID="expired-capability"
	var unused RemoteInferenceResponse
	if err:=postJSON(ctx,svcA.pinnedClient(identB.Fingerprint,true),
	 ts.URL+"/federation/v1/inference",stale,&unused);err==nil{
	 t.Fatal("stale heartbeat accepted by the inference receiver")
	}
	if _,err:=dbB.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
	 SET expires_at=? WHERE peer_node_id='node-a'`,now+3600000);err!=nil{t.Fatal(err)}
	if _,err:=dbB.SQL().ExecContext(ctx,`UPDATE node_pairings SET peer_confirmed=0
	 WHERE peer_node_id='node-a'`);err!=nil{t.Fatal(err)}
	oneSided:=in
	oneSided.RequestID="one-sided-pairing"
	var unusedPair RemoteInferenceResponse
	if err:=postJSON(ctx,svcA.pinnedClient(identB.Fingerprint,true),
	 ts.URL+"/federation/v1/inference",oneSided,&unusedPair);err==nil{
	 t.Fatal("one-sided pairing unexpectedly authorized a model request")
	}
	if _,err:=dbB.SQL().ExecContext(ctx,`UPDATE node_pairings SET peer_confirmed=1
	 WHERE peer_node_id='node-a'`);err!=nil{t.Fatal(err)}
	server.models=nil
	newReq:=in
	newReq.RequestID="missing-local-model-admission"
	var denied RemoteInferenceResponse
	if err:=postJSON(ctx,svcA.pinnedClient(identB.Fingerprint,true),
		ts.URL+"/federation/v1/inference",newReq,&denied);err==nil{
		t.Fatal("federated inference accepted without an admission authority")
	}
	var requests int
	if err:=dbB.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
	 FROM node_federated_inference_receipts
	 WHERE remote_request_id='missing-local-model-admission'`).Scan(&requests);err!=nil{t.Fatal(err)}
	if requests!=0{
		t.Fatal("unadmitted inference payload reached execution receipt storage")
	}
}

type echoExecutor struct{}

func (echoExecutor) DispatchFederatedLocal(_ context.Context, deploymentID, requestID string, raw json.RawMessage) (inference.DispatchResult, error) {
	return inference.DispatchResult{ResponseJSON: json.RawMessage(`{"echo":` + string(raw) + `}`), UsageJSON: json.RawMessage(`{"remote":true}`)}, nil
}
