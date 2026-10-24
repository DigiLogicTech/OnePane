//go:build integration

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type outcomeRuntimeTransport struct {
	calls   int
	invalid bool
	err     error
}

func (tr *outcomeRuntimeTransport) Invoke(_ context.Context, _ Connection, req agentprotocol.Request, _ SecretResolver) (agentprotocol.Response, error) {
	tr.calls++
	if tr.err != nil {
		return agentprotocol.Response{}, tr.err
	}
	response := agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalComplete, Proposal: json.RawMessage(`{"summary":"done"}`)}
	if tr.invalid {
		response.RequestID = "different-invocation"
	}
	return response, nil
}

// These exercise the service's durable outcome classification, including custom
// transports that do not use the built-in HTTP response validation. No external
// Agent runtime or physical inference is represented by this fixture.
func TestRuntimeDispatchUnusableOutcomesRemainDurablyUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, code              string
		invalid, persistFailure bool
		transportErr            error
		status                  InvocationStatus
	}{
		{name: "invalid custom response", code: "invalid_agent_response", invalid: true, status: InvocationUnknown},
		{name: "untyped transport error", code: "runtime_transport_error", transportErr: errors.New("connection lost after dispatch"), status: InvocationUnknown},
		{name: "known transport preflight rejection", code: "secret_unavailable", transportErr: &RuntimeTransportError{Code: "secret_unavailable", OutcomeKnown: true}, status: InvocationFailed},
		{name: "response persistence failure", code: "response_persist_failed", persistFailure: true, status: InvocationUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db, err := sqliteStore.Open(root + "/outcome.db")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Outcome test','active',1,1,1)`,
				`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1,1)`,
				`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',1,1)`,
				`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
				`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','test','running','normal_task',0,'{}',1,1,1)`,
			} {
				if _, err = db.SQL().ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			registry := NewRegistry()
			if err = RegisterBuiltinAdapters(registry); err != nil {
				t.Fatal(err)
			}
			svc := NewService(db.SQL(), db, registry, clock.Real{})
			ws, node, taskID := "ws", "node", "task"
			connection, err := svc.Register(ctx, RegisterCommand{WorkspaceID: &ws, NodeID: &node, RuntimeKind: "hermes", DisplayName: "Disposable local bridge", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:9000"}`), AuthType: "none", TrustState: TrustUserTrusted, OperatingMode: ProposalOnly, DataPolicyJSON: json.RawMessage(`{"max_confidentiality":"secret","allowed_residency":["origin_node"],"destination_kind":"origin_node","allow_raw_secrets":false}`)})
			if err != nil {
				t.Fatal(err)
			}
			connection, err = svc.SetStatus(ctx, SetStatusCommand{ConnectionID: connection.ID, ExpectedRevision: connection.Revision, Status: StatusConnected})
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifact.NewLocalStore(root + "/artifacts")
			if err != nil {
				t.Fatal(err)
			}
			tr := &outcomeRuntimeTransport{invalid: tc.invalid, err: tc.transportErr}
			transports := NewRuntimeTransportRegistry()
			if err = transports.Register("builtin.agent_protocol_http", "1", tr); err != nil {
				t.Fatal(err)
			}
			if err = svc.ConfigureExecution(artifact.NewService(db.SQL(), db, store, clock.Real{}), transports, nil, node); err != nil {
				t.Fatal(err)
			}
			budgets := budget.NewService(db.SQL(), db, clock.Real{})
			if err = svc.ConfigureBudget(budgets); err != nil {
				t.Fatal(err)
			}
			account, err := budgets.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &ws, Name: "Runtime quota", Unit: "requests", LimitAmount: 5})
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := budgets.Reserve(ctx, budget.ReserveCommand{WorkspaceID: ws, AccountID: account.ID, TaskID: &taskID, Amount: 1, TTLMillis: 60000})
			if err != nil {
				t.Fatal(err)
			}
			if tc.persistFailure {
				if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER fail_runtime_response_artifact BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT,'response persistence unavailable'); END`); err != nil {
					t.Fatal(err)
				}
			}
			result, err := svc.Invoke(ctx, InvokeCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", ConnectionID: connection.ID, Objective: "Inspect task", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: node}, BudgetReservationID: &reservation.ID})
			if err == nil {
				t.Fatal("unusable dispatch outcome returned success")
			}
			if tr.calls != 1 {
				t.Fatalf("transport dispatch calls=%d, want one", tr.calls)
			}
			if result.Response != nil || result.Invocation.ResponseArtifactID != nil {
				t.Fatalf("unusable response escaped service: %+v", result)
			}
			stored, getErr := svc.Invocation(ctx, result.Invocation.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if stored.Status != tc.status || stored.ErrorCode == nil || *stored.ErrorCode != tc.code {
				t.Fatalf("durable outcome=%+v, want status=%s code=%s (invoke error %v)", stored, tc.status, tc.code, err)
			}
			if result.Invocation.Status != stored.Status {
				t.Fatalf("returned outcome differs from durable outcome: %+v", result)
			}
			closed, getErr := budgets.Reservation(ctx, reservation.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if closed.Status != budget.ReservationCommitted || closed.AgentRuntimeInvocationID == nil || *closed.AgentRuntimeInvocationID != stored.ID {
				t.Fatalf("dispatch quota not durably committed to invocation: %+v", closed)
			}
			account, getErr = budgets.Account(ctx, account.ID)
			if getErr != nil || account.CommittedAmount != 1 || account.ReservedAmount != 0 {
				t.Fatalf("quota accounting=%+v err=%v", account, getErr)
			}
		})
	}
}
