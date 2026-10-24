//go:build integration

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestRuntimePreDispatchErrorsPreserveCleanupAndInvocationIdentity(t *testing.T) {
	for _, outcome := range []string{"missing", "authorized", "dispatched", "executing", "late_identity"} {
		for _, rejectRelease := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/release_rejected_%t", outcome, rejectRelease), func(t *testing.T) {
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
				transports := NewRuntimeTransportRegistry()
				if outcome != "missing" {
					if err = transports.Register("builtin.agent_protocol_http", "1", AgentProtocolHTTPTransport{}); err != nil {
						t.Fatal(err)
					}
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
				if rejectRelease {
					if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_preflight_release BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'cleanup release unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				}
				if outcome != "missing" && outcome != "late_identity" {
					if _, err = db.SQL().ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER reject_preflight_transition BEFORE INSERT ON events WHEN NEW.event_type='agent_runtime_invocation.%s' BEGIN SELECT RAISE(ABORT,'transition journal unavailable'); END`, outcome)); err != nil {
						t.Fatal(err)
					}
				}
				if outcome == "late_identity" {
					if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER revoke_preflight_identity AFTER INSERT ON events WHEN NEW.event_type='agent_runtime_invocation.executing' BEGIN UPDATE agent_runtime_connections SET status='disabled',revision=revision+1; END`); err != nil {
						t.Fatal(err)
					}
				}
				command := InvokeCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", ConnectionID: connection.ID, Objective: "Inspect task", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: node}, BudgetReservationID: &reservation.ID}
				result, err := svc.Invoke(ctx, command)
				if err == nil || result.Invocation.ID == "" || httpCalls.Load() != 0 || result.Response != nil || result.Invocation.ResponseArtifactID != nil {
					t.Fatalf("preflight returned false success/dispatched: result=%+v err=%v calls=%d", result, err, httpCalls.Load())
				}
				if strings.Contains(err.Error(), "cleanup release unavailable") != rejectRelease {
					t.Errorf("cleanup error omitted/invented: %v", err)
				}
				expectedStatus := map[string]InvocationStatus{"missing": InvocationFailed, "authorized": InvocationCreated, "dispatched": InvocationAuthorized, "executing": InvocationDispatched, "late_identity": InvocationFailed}[outcome]
				if result.Invocation.Status != expectedStatus {
					t.Errorf("pretransport outcome changed: got=%s want=%s", result.Invocation.Status, expectedStatus)
				}
				if outcome == "missing" && !errors.Is(err, ErrAdapterUnavailable) {
					t.Errorf("lost original preflight error: %v", err)
				}
				if outcome != "missing" && outcome != "late_identity" && !strings.Contains(err.Error(), "transition journal unavailable") {
					t.Errorf("lost original transition error: %v", err)
				}
				held, getErr := budgets.Reservation(ctx, reservation.ID)
				wantStatus, wantReserved := budget.ReservationReleased, int64(0)
				if rejectRelease {
					wantStatus, wantReserved = budget.ReservationReserved, 1
				}
				if getErr != nil || held.Status != wantStatus || held.AgentRuntimeInvocationID == nil || *held.AgentRuntimeInvocationID != result.Invocation.ID {
					t.Fatalf("preflight quota binding=%+v err=%v", held, getErr)
				}
				balance, getErr := budgets.Account(ctx, account.ID)
				if getErr != nil || balance.ReservedAmount != wantReserved || balance.CommittedAmount != 0 {
					t.Fatalf("preflight accounting=%+v err=%v", balance, getErr)
				}
				var releases, artifacts int
				if err = db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM artifacts)`).Scan(&releases, &artifacts); err != nil || artifacts != 0 || releases != int(1-wantReserved) {
					t.Fatalf("preflight effects releases=%d artifacts=%d err=%v", releases, artifacts, err)
				}
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				original := errors.New("original transition rejected")
				attemptID := "original-attempt"
				command.AttemptID = &attemptID
				unreadable, readErr := svc.preDispatchInvocationTransitionError(cancelled, result.Invocation.ID, original, command)
				if unreadable.Invocation.ID != result.Invocation.ID || unreadable.Invocation.AttemptID == nil || *unreadable.Invocation.AttemptID != attemptID || !errors.Is(readErr, original) || !errors.Is(readErr, context.Canceled) {
					t.Fatalf("transition/read errors or original identity lost: %+v %v", unreadable, readErr)
				}
			})
		}
	}
}
