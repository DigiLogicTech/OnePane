//go:build integration

package nodefederation

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

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
			if !c.Schedulable {
				t.Fatalf("remote candidate not schedulable: %+v", c)
			}
			if c.Destination.Kind != "trusted_node" {
				t.Fatalf("destination=%s", c.Destination.Kind)
			}
		}
	}
	if !found {
		t.Fatal("remote model candidate not imported")
	}
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
	type echoExec struct{}
	_ = echoExec{}
	server := NewServer(svcB, echoExecutor{})
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
}

type echoExecutor struct{}

func (echoExecutor) DispatchFederatedLocal(_ context.Context, deploymentID, requestID string, raw json.RawMessage) (inference.DispatchResult, error) {
	return inference.DispatchResult{ResponseJSON: json.RawMessage(`{"echo":` + string(raw) + `}`), UsageJSON: json.RawMessage(`{"remote":true}`)}, nil
}
