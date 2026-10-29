//go:build integration

package inference

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

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestInferencePreDispatchErrorsPreserveCleanupAndRequestIdentity(t *testing.T) {
	for _, outcome := range []string{"missing", "routed", "budget_reserved", "dispatched", "executing", "late_identity"} {
		for _, rejectRelease := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/release_rejected_%t", outcome, rejectRelease), func(t *testing.T) {
				ctx := context.Background()
				db := migratedInferenceDispatchDatabase(t, ctx)
				var err error
				for _, q := range []string{
					`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('ws','Test','active',1,1)`,
					`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1)`,
					`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',1,1)`,
					`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
					`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES('task','ws','test','running','normal_task',0,'{}',1,1)`,
					`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','test/model','["text"]','{}','user_trusted',1,1)`,
					`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,discovered_at,updated_at) VALUES('deployment','model','node','local-openai','{}','ready','fp',1,1)`,
				} {
					if _, err = db.SQL().ExecContext(ctx, q); err != nil {
						t.Fatal(err)
					}
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					switch outcome {
					case "rate_limit":
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte(`{"error":"limited"}`))
					case "invalid_response":
						_, _ = w.Write([]byte(`invalid`))
					default:
						_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}],"usage":{"total_tokens":1}}`))
					}
				}))
				defer server.Close()
				registry := NewTransportRegistry()
				if outcome != "missing" {
					if err = registry.Register("local-openai", LocalOpenAITransport{Resolver: endpointResolverFunc(func(context.Context, string) (string, error) { return server.URL, nil })}); err != nil {
						t.Fatal(err)
					}
				}
				store, err := artifact.NewLocalStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				svc := NewService(db.SQL(), db, clock.Real{})
				if err = svc.ConfigureExecution(artifact.NewService(db.SQL(), db, store, clock.Real{}), registry, nil, "node"); err != nil {
					t.Fatal(err)
				}
				quotas := budget.NewService(db.SQL(), db, clock.Real{})
				if err = svc.ConfigureBudget(quotas); err != nil {
					t.Fatal(err)
				}
				ws, taskID := "ws", "task"
				account, err := quotas.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &ws, Name: "Quota", Unit: "requests", LimitAmount: 5})
				if err != nil {
					t.Fatal(err)
				}
				reservation, err := quotas.Reserve(ctx, budget.ReserveCommand{WorkspaceID: ws, AccountID: account.ID, TaskID: &taskID, Amount: 1, TTLMillis: 60000})
				if err != nil {
					t.Fatal(err)
				}
				if rejectRelease {
					if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_preflight_release BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'cleanup release unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				}
				if outcome != "missing" && outcome != "late_identity" {
					if _, err = db.SQL().ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER reject_preflight_transition BEFORE INSERT ON events WHEN NEW.event_type='inference_request.%s' BEGIN SELECT RAISE(ABORT,'transition journal unavailable'); END`, outcome)); err != nil {
						t.Fatal(err)
					}
				}
				if outcome == "late_identity" {
					if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER revoke_preflight_identity AFTER INSERT ON events WHEN NEW.event_type='inference_request.executing' BEGIN UPDATE model_deployments SET status='unavailable' WHERE id='deployment'; END`); err != nil {
						t.Fatal(err)
					}
				}
				command := ExecuteCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", DeploymentID: "deployment", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: "node"}, RequestJSON: json.RawMessage(`{"messages":[{"role":"user","content":"test"}]}`), BudgetReservationID: &reservation.ID}
				result, err := svc.Execute(ctx, command)
				if err == nil || result.ID == "" || calls.Load() != 0 || result.ResponseArtifactID != nil {
					t.Fatalf("preflight returned false success/dispatched: result=%+v err=%v calls=%d", result, err, calls.Load())
				}
				if strings.Contains(err.Error(), "cleanup release unavailable") != rejectRelease {
					t.Errorf("cleanup error omitted/invented: %v", err)
				}
				expectedStatus := map[string]RequestStatus{"missing": RequestFailed, "routed": RequestCreated, "budget_reserved": RequestRouted, "dispatched": RequestBudgetReserved, "executing": RequestDispatched, "late_identity": RequestFailed}[outcome]
				if result.Status != expectedStatus {
					t.Errorf("pretransport outcome changed: got=%s want=%s", result.Status, expectedStatus)
				}
				if outcome == "missing" && !errors.Is(err, ErrTransportUnavailable) {
					t.Errorf("lost original preflight error: %v", err)
				}
				if outcome == "late_identity" {
					var transportErr *TransportError
					if !errors.As(err, &transportErr) || !transportErr.OutcomeKnown || transportErr.Code != "inference_route_identity_revoked" {
						t.Errorf("known pretransport error wrapping changed: %v", err)
					}
				}
				if outcome != "missing" && outcome != "late_identity" && !strings.Contains(err.Error(), "transition journal unavailable") {
					t.Errorf("lost original transition error: %v", err)
				}
				held, getErr := quotas.Reservation(ctx, reservation.ID)
				wantStatus, wantReserved := budget.ReservationReleased, int64(0)
				if rejectRelease {
					wantStatus, wantReserved = budget.ReservationReserved, 1
				}
				if getErr != nil || held.Status != wantStatus || held.InferenceRequestID == nil || *held.InferenceRequestID != result.ID {
					t.Fatalf("preflight quota binding=%+v err=%v", held, getErr)
				}
				balance, getErr := quotas.Account(ctx, account.ID)
				if getErr != nil || balance.ReservedAmount != wantReserved || balance.CommittedAmount != 0 {
					t.Fatalf("preflight accounting=%+v err=%v", balance, getErr)
				}
				var releases, artifacts int
				if err = db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM artifacts)`).Scan(&releases, &artifacts); err != nil || artifacts != 0 || releases != int(1-wantReserved) {
					t.Fatalf("preflight effects releases=%d artifacts=%d err=%v", releases, artifacts, err)
				}
				// A cancelled read cannot erase the failed transition or known request identity.
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				original := errors.New("original transition rejected")
				unreadable, readErr := svc.preDispatchTransitionError(cancelled, result.ID, original, command)
				if unreadable.ID != result.ID || !errors.Is(readErr, original) || !errors.Is(readErr, context.Canceled) {
					t.Fatalf("transition/read errors or original identity lost: %+v %v", unreadable, readErr)
				}
			})
		}
	}
}
