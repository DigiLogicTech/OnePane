//go:build integration

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteHermesRuntimeRegistrationAndEligibility(t *testing.T) {
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
	reg := NewRegistry()
	if err := RegisterBuiltinAdapters(reg); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, reg, clock.Real{})
	ws := "ws"
	c, err := svc.Register(ctx, RegisterCommand{WorkspaceID: &ws, RuntimeKind: "hermes", DisplayName: "Hermes", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:9000"}`), AuthType: "none", TrustState: TrustUserTrusted, OperatingMode: ProposalOnly})
	if err != nil {
		t.Fatal(err)
	}
	var p DataPolicy
	if err := json.Unmarshal(c.DataPolicyJSON, &p); err != nil {
		t.Fatal(err)
	}
	if p.MaxConfidentiality != "public" || p.AllowRawSecrets {
		t.Fatalf("unsafe default: %+v", p)
	}
	c, err = svc.SetStatus(ctx, SetStatusCommand{ConnectionID: c.ID, ExpectedRevision: c.Revision, Status: StatusConnected})
	if err != nil {
		t.Fatal(err)
	}
	if e := c.Eligibility(); !e.Schedulable || e.ToolCallback {
		t.Fatalf("eligibility=%+v", e)
	}
}

func TestHermesRegistrationRejectsPlaintextSecret(t *testing.T) {
	raw, _ := canonicalJSON(json.RawMessage(`{"base_url":"http://x","token":"bad"}`), "{}")
	if err := rejectSecretLikeJSON(raw); !errors.Is(err, ErrUnsafeConfig) {
		t.Fatalf("err=%v", err)
	}
}

type fixedRuntimeTransport struct{}

func (fixedRuntimeTransport) Invoke(_ context.Context, _ Connection, req agentprotocol.Request, _ SecretResolver) (agentprotocol.Response, error) {
	return agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalComplete, Proposal: json.RawMessage(`{"summary":"done"}`)}, nil
}

func TestSQLiteHermesCompleteProposalDoesNotCompleteAuthoritativeTask(t *testing.T) {
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
		{`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','test','running','normal_task',0,'{}',1,?,?)`, []any{now, now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewRegistry()
	if err := RegisterBuiltinAdapters(reg); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, reg, clock.Real{})
	ws := "ws"
	node := "node"
	data := json.RawMessage(`{"max_confidentiality":"secret","allowed_residency":["origin_node"],"destination_kind":"origin_node","allow_raw_secrets":false}`)
	c, err := svc.Register(ctx, RegisterCommand{WorkspaceID: &ws, NodeID: &node, RuntimeKind: "hermes", DisplayName: "Hermes local", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:9000"}`), AuthType: "none", TrustState: TrustUserTrusted, OperatingMode: ProposalOnly, DataPolicyJSON: data})
	if err != nil {
		t.Fatal(err)
	}
	c, err = svc.SetStatus(ctx, SetStatusCommand{ConnectionID: c.ID, ExpectedRevision: c.Revision, Status: StatusConnected})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewLocalStore(root + "/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	arts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	trs := NewRuntimeTransportRegistry()
	if err := trs.Register("builtin.agent_protocol_http", "1", fixedRuntimeTransport{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureExecution(arts, trs, nil, "node"); err != nil {
		t.Fatal(err)
	}
	task := "task"
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: "node"}
	res, err := svc.Invoke(ctx, InvokeCommand{WorkspaceID: "ws", TaskID: &task, PrincipalID: "agent", ConnectionID: c.ID, Objective: "inspect task", InputLabel: label})
	if err != nil {
		t.Fatal(err)
	}
	if res.Invocation.Status != InvocationSucceeded || res.Response == nil || res.Response.ProposalType != agentprotocol.ProposalComplete {
		t.Fatalf("res=%+v", res)
	}
	var state string
	if err := db.SQL().QueryRowContext(ctx, `SELECT state FROM tasks WHERE id='task'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatalf("external harness mutated task state to %s", state)
	}
	if res.Invocation.ResponseArtifactID == nil {
		t.Fatal("missing response artifact")
	}
	if err := arts.VerifyContent(ctx, *res.Invocation.ResponseArtifactID); err != nil {
		t.Fatal(err)
	}
}
