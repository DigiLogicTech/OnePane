//go:build integration

package inference

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type integrationNoSecretResolver struct{}

func (integrationNoSecretResolver) Resolve(context.Context, string) (string, error) {
	return "", errors.New("secret resolution not expected in this integration test")
}

func TestSQLiteModelProviderDeploymentLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',?,1,?,?)`, now, now, now); err != nil {
		t.Fatal(err)
	}

	svc := NewService(db.SQL(), db, clock.Real{})
	ws := "ws"
	provider, err := svc.RegisterProvider(ctx, RegisterProviderCommand{WorkspaceID: &ws, Provider: "openai_compatible", DisplayName: "Local API", AuthType: "secret_ref", SecretRef: strptr("secret/provider"), ConnectionJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:8080/v1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err = svc.SetProviderStatus(ctx, SetProviderStatusCommand{ConnectionID: provider.ID, ExpectedRevision: provider.Revision, Status: ProviderConnected})
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.RegisterModel(ctx, RegisterModelCommand{ModelRef: "example/model", ModalitiesJSON: json.RawMessage(`["text"]`), TrustState: ModelQuarantined})
	if err != nil {
		t.Fatal(err)
	}
	node := "node"
	deployment, err := svc.RegisterDeployment(ctx, RegisterDeploymentCommand{ModelID: model.ID, NodeID: &node, ProviderConnectionID: &provider.ID, RuntimeName: strptr("openai-compatible"), RuntimeConfigJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:8080/v1"}`), ContextMaxReported: i64ptr(65536)})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: deployment.ID, ExpectedRevision: deployment.Revision, Status: DeploymentQualifying})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: deployment.ID, ExpectedRevision: deployment.Revision, Status: DeploymentReady}); !errors.Is(err, ErrModelQuarantined) {
		t.Fatalf("expected quarantined denial, got %v", err)
	}
	model, err = svc.SetModelTrust(ctx, SetModelTrustCommand{ModelID: model.ID, ExpectedTrustState: ModelQuarantined, TrustState: ModelUserTrusted})
	if err != nil {
		t.Fatal(err)
	}
	if model.TrustState != ModelUserTrusted {
		t.Fatalf("trust=%s", model.TrustState)
	}
	deployment, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: deployment.ID, ExpectedRevision: deployment.Revision, Status: DeploymentReady, ResidencyState: residencyptr(ResidencyResident)})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ContextMaxVerified != nil {
		t.Fatal("M9 generic catalog asserted verified context")
	}
	provider, err = svc.SetProviderStatus(ctx, SetProviderStatusCommand{ConnectionID: provider.ID, ExpectedRevision: provider.Revision, Status: ProviderRevoked})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Status != ProviderRevoked {
		t.Fatalf("provider status=%s", provider.Status)
	}
}

func TestProviderRegistrationRejectsRawSecret(t *testing.T) {
	svc := &Service{}
	_ = svc
	raw, err := normalizeJSON(json.RawMessage(`{"api_key":"plaintext"}`), "{}")
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectSecretLikeJSON(raw); !errors.Is(err, ErrUnsafeConfig) {
		t.Fatalf("err=%v", err)
	}
}

func strptr(v string) *string                       { return &v }
func i64ptr(v int64) *int64                         { return &v }
func residencyptr(v ResidencyState) *ResidencyState { return &v }

func TestSQLiteFakeInferenceStoresResponseArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := sqliteStore.Open(root + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)`, []any{now, now}},
		{`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',?,1,?,?)`, []any{now, now, now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(root + "/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	transports := NewTransportRegistry()
	if err := RegisterBuiltinTransports(transports); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	if err := svc.ConfigureExecution(artifacts, transports, integrationNoSecretResolver{}, "node"); err != nil {
		t.Fatal(err)
	}
	model, err := svc.RegisterModel(ctx, RegisterModelCommand{ModelRef: "builtin/fake", TrustState: ModelUserTrusted})
	if err != nil {
		t.Fatal(err)
	}
	node := "node"
	runtime := "fake"
	dep, err := svc.RegisterDeployment(ctx, RegisterDeploymentCommand{ModelID: model.ID, NodeID: &node, RuntimeName: &runtime})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: DeploymentQualifying})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: DeploymentReady, ResidencyState: residencyptr(ResidencyResident)})
	if err != nil {
		t.Fatal(err)
	}
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: "node"}
	r, err := svc.Execute(ctx, ExecuteCommand{WorkspaceID: "ws", PrincipalID: "agent", DeploymentID: dep.ID, InputLabel: label, RequestJSON: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RequestSucceeded || r.ResponseArtifactID == nil {
		t.Fatalf("request=%+v", r)
	}
	if err := artifacts.VerifyContent(ctx, *r.ResponseArtifactID); err != nil {
		t.Fatal(err)
	}
	a, err := artifacts.Get(ctx, *r.ResponseArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Label.Trust != policy.TrustUnverifiedDerived || a.Label.Confidentiality != policy.ConfidentialityInternal || a.Label.Residency != policy.ResidencyOriginNode {
		t.Fatalf("output label=%+v", a.Label)
	}
}

func TestSQLiteCloudInformationFlowDenialIsDurable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := sqliteStore.Open(root + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)`, []any{now, now}},
		{`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',?,1,?,?)`, []any{now, now, now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(root + "/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	arts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	tr := NewTransportRegistry()
	if err := RegisterBuiltinTransports(tr); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	if err := svc.ConfigureExecution(arts, tr, integrationNoSecretResolver{}, "node"); err != nil {
		t.Fatal(err)
	}
	ws := "ws"
	p, err := svc.RegisterProvider(ctx, RegisterProviderCommand{WorkspaceID: &ws, Provider: "openai_compatible", DisplayName: "Cloud", AuthType: "none", ConnectionJSON: json.RawMessage(`{"base_url":"https://example.invalid"}`)})
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.SetProviderStatus(ctx, SetProviderStatusCommand{ConnectionID: p.ID, ExpectedRevision: p.Revision, Status: ProviderConnected})
	if err != nil {
		t.Fatal(err)
	}
	m, err := svc.RegisterModel(ctx, RegisterModelCommand{ModelRef: "cloud/model", TrustState: ModelUserTrusted})
	if err != nil {
		t.Fatal(err)
	}
	runtime := "openai-compatible"
	dep, err := svc.RegisterDeployment(ctx, RegisterDeploymentCommand{ModelID: m.ID, ProviderConnectionID: &p.ID, RuntimeName: &runtime})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: DeploymentQualifying})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = svc.SetDeploymentStatus(ctx, SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: DeploymentReady})
	if err != nil {
		t.Fatal(err)
	}
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	r, execErr := svc.Execute(ctx, ExecuteCommand{WorkspaceID: "ws", PrincipalID: "agent", DeploymentID: dep.ID, InputLabel: label, RequestJSON: json.RawMessage(`{"messages":[]}`)})
	if !errors.Is(execErr, ErrInformationFlow) {
		t.Fatalf("err=%v", execErr)
	}
	if r.Status != RequestFailed || r.ErrorCode == nil || *r.ErrorCode != "information_flow_denied" {
		t.Fatalf("request=%+v", r)
	}
}
