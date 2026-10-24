//go:build integration

package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestRuntimePostDispatchBudgetCommitFailureRetainsUnknownQuota(t *testing.T) {
	for _, tc := range []struct {
		name, code   string
		transportErr error
		status       InvocationStatus
	}{{name: "success", code: "budget_commit_failed", status: InvocationUnknown}, {name: "known_transport_error", code: "budget_commit_failed", transportErr: &RuntimeTransportError{Code: "known_failure", OutcomeKnown: true}, status: InvocationUnknown}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db := migratedRuntimeDispatchDatabase(t, ctx)
			var err error
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
			var httpCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpCalls.Add(1)
				var request agentprotocol.Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				_ = json.NewEncoder(w).Encode(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: request.RequestID, ProposalType: agentprotocol.ProposalComplete, Proposal: json.RawMessage(`{"summary":"done"}`)})
			}))
			defer server.Close()
			connection, err := svc.Register(ctx, RegisterCommand{WorkspaceID: &ws, NodeID: &node, RuntimeKind: "hermes", DisplayName: "Disposable local bridge", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{"base_url":"` + server.URL + `"}`), AuthType: "none", TrustState: TrustUserTrusted, OperatingMode: ProposalOnly, DataPolicyJSON: json.RawMessage(`{"max_confidentiality":"secret","allowed_residency":["origin_node"],"destination_kind":"origin_node","allow_raw_secrets":false}`)})
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
			tr := &outcomeRuntimeTransport{err: tc.transportErr}
			var transport RuntimeTransport = AgentProtocolHTTPTransport{}
			if tc.transportErr != nil {
				transport = tr
			}
			transports := NewRuntimeTransportRegistry()
			if err = transports.Register("builtin.agent_protocol_http", "1", transport); err != nil {
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
			if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_runtime_budget_commit BEFORE INSERT ON events WHEN NEW.event_type='budget.committed' BEGIN SELECT RAISE(ABORT,'budget commit unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			result, err := svc.Invoke(ctx, InvokeCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", ConnectionID: connection.ID, Objective: "Inspect task", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: node}, BudgetReservationID: &reservation.ID})
			if err == nil || !strings.Contains(err.Error(), "budget commit unavailable") {
				t.Fatal("unusable dispatch outcome returned success")
			}
			if tr.calls+int(httpCalls.Load()) != 1 {
				t.Fatalf("transport dispatch calls=%d, want one", tr.calls+int(httpCalls.Load()))
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
			if closed.Status != budget.ReservationReserved || closed.AgentRuntimeInvocationID == nil || *closed.AgentRuntimeInvocationID != stored.ID {
				t.Fatalf("spent reservation not retained at original invocation: %+v", closed)
			}
			account, getErr = budgets.Account(ctx, account.ID)
			if getErr != nil || account.CommittedAmount != 0 || account.ReservedAmount != 1 {
				t.Fatalf("quota accounting=%+v err=%v", account, getErr)
			}
			_, _ = svc.Invoke(ctx, InvokeCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", ConnectionID: connection.ID, Objective: "Inspect task", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: node}, BudgetReservationID: &reservation.ID})
			var refunds, artifacts int
			if err = db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM artifacts)`).Scan(&refunds, &artifacts); err != nil || refunds != 0 || artifacts != 0 || tr.calls+int(httpCalls.Load()) != 1 {
				t.Fatalf("replay/refund/output: calls=%d refunds=%d artifacts=%d error=%v", tr.calls+int(httpCalls.Load()), refunds, artifacts, err)
			}
		})
	}
}
