//go:build integration

package agentworker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
)

func TestBudgetCommitFailurePreservesOriginalWorkerAndSpentQuota(t *testing.T) {
	for _, stepKind := range []string{"model", "agent_runtime"} {
		t.Run(stepKind, func(t *testing.T) {
			w, ctx, run, accountID := prepareBudgetJournal(t, 1)
			if _, err := w.db.ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node-local','Local',1,'response-journal-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				var body json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					http.Error(rw, "bad request", http.StatusBadRequest)
					return
				}
				var req agentprotocol.Request
				if stepKind == "model" {
					var envelope struct {
						Messages []struct{ Role, Content string } `json:"messages"`
					}
					if err := json.Unmarshal(body, &envelope); err != nil {
						http.Error(rw, "bad envelope", http.StatusBadRequest)
						return
					}
					for _, m := range envelope.Messages {
						if m.Role == "user" {
							if err := json.Unmarshal([]byte(m.Content), &req); err != nil {
								http.Error(rw, "bad AgentRequest", http.StatusBadRequest)
								return
							}
						}
					}
				} else if err := json.Unmarshal(body, &req); err != nil {
					http.Error(rw, "bad runtime AgentRequest", http.StatusBadRequest)
					return
				}
				response := agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalComplete, Message: "Synthetic completion", Proposal: json.RawMessage(`{"result":{"declared":true}}`)}
				raw, _ := json.Marshal(response)
				rw.Header().Set("Content-Type", "application/json")
				if stepKind == "agent_runtime" {
					_ = json.NewEncoder(rw).Encode(response)
					return
				}
				_ = json.NewEncoder(rw).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}, "usage": map[string]any{"completion_tokens": 1}})
			}))
			defer server.Close()
			store, err := artifact.NewLocalStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
			w.inference = inference.NewService(w.db, w.tx, clock.Real{})
			transports := inference.NewTransportRegistry()
			if err := transports.Register("no-transport", inference.LocalOpenAITransport{Resolver: responseJournalEndpoint(server.URL), Client: server.Client()}); err != nil {
				t.Fatal(err)
			}
			if err := w.inference.ConfigureExecution(w.artifacts, transports, nil, "node-local"); err != nil {
				t.Fatal(err)
			}
			if err := w.inference.ConfigureRuntimeCoordinator(runtimecoord.NewService(w.db, clock.Real{})); err != nil {
				t.Fatal(err)
			}
			if err := w.inference.ConfigureBudget(w.budgets); err != nil {
				t.Fatal(err)
			}
			if stepKind == "agent_runtime" {
				adapters := agentruntime.NewRegistry()
				if err := agentruntime.RegisterBuiltinAdapters(adapters); err != nil {
					t.Fatal(err)
				}
				w.runtimes = agentruntime.NewService(w.db, w.tx, adapters, clock.Real{})
				transports := agentruntime.NewRuntimeTransportRegistry()
				if err := transports.Register("builtin.agent_protocol_http", "1", agentruntime.AgentProtocolHTTPTransport{Client: server.Client()}); err != nil {
					t.Fatal(err)
				}
				if err := w.runtimes.ConfigureExecution(w.artifacts, transports, nil, "node-local"); err != nil {
					t.Fatal(err)
				}
				if err := w.runtimes.ConfigureBudget(w.budgets); err != nil {
					t.Fatal(err)
				}
				workspace, node := run.WorkspaceID, "node-local"
				endpoint, _ := json.Marshal(map[string]string{"base_url": server.URL})
				connection, err := w.runtimes.Register(ctx, agentruntime.RegisterCommand{WorkspaceID: &workspace, NodeID: &node, RuntimeKind: "hermes", DisplayName: "Synthetic runtime", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: endpoint, AuthType: "none", TrustState: agentruntime.TrustUserTrusted, OperatingMode: agentruntime.ProposalOnly, CapabilitiesJSON: json.RawMessage(`{"capabilities":["inference.general"],"protocol_level":"L2","context_max":64000,"scheduling":{"cost_class":"paid"}}`), DataPolicyJSON: json.RawMessage(`{"max_confidentiality":"internal","allowed_residency":["any"],"destination_kind":"origin_node","allow_raw_secrets":false}`)})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.runtimes.SetStatus(ctx, agentruntime.SetStatusCommand{ConnectionID: connection.ID, ExpectedRevision: connection.Revision, Status: agentruntime.StatusConnected}); err != nil {
					t.Fatal(err)
				}
				if _, err := w.db.ExecContext(ctx, `UPDATE model_deployments SET status='unavailable'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_dispatch_budget_commit BEFORE INSERT ON events WHEN NEW.event_type='budget.committed' BEGIN SELECT RAISE(ABORT,'budget commit unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			prepared := w.prepareStep(ctx, run)
			if prepared.dispatch == nil {
				t.Fatalf("actual transport not prepared: %+v", prepared.result)
			}
			result := prepared.dispatch()()
			if result.Status != "blocked" || result.Proposal != "" || w.dispatchReconciliationFailed {
				t.Fatalf("uncommitted spent quota permitted proposal/fallback: %+v halted=%v", result, w.dispatchReconciliationFailed)
			}
			assertOriginal := func() {
				t.Helper()
				var state, attempt, worker, requestState, code, requestID string
				var attempts, checkpoints, completed, refunds, successJournals, unknownJournals int
				if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=t.id) FROM tasks t JOIN task_attempts a ON a.id=? AND a.task_id=t.id JOIN agent_worker_runs r ON r.id=? AND r.task_id=t.id AND r.attempt_id=a.id WHERE t.id=?`, run.AttemptID, run.ID, run.TaskID).Scan(&state, &attempt, &worker, &attempts); err != nil || state != "blocked" || attempt != "interrupted" || worker != "interrupted" || attempts != 1 {
					t.Fatalf("original recovery identity changed: %s/%s/%s attempts=%d err=%v", state, attempt, worker, attempts, err)
				}
				query := `SELECT id,status,error_code FROM inference_requests WHERE task_id=?`
				if stepKind == "agent_runtime" {
					query = `SELECT id,status,error_code FROM agent_runtime_invocations WHERE task_id=?`
				}
				if err := w.db.QueryRowContext(ctx, query, run.TaskID).Scan(&requestID, &requestState, &code); err != nil || requestState != "unknown" || code != "budget_commit_failed" {
					t.Fatalf("unknown spent request lost: id=%s status=%s code=%s err=%v", requestID, requestState, code, err)
				}
				if err := w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM checkpoints WHERE task_id=?),(SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.completed'),(SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind=? AND status='succeeded'),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind=? AND status='unknown' AND result_ref=?)`, run.TaskID, run.TaskID, run.ID, stepKind, run.ID, stepKind, requestID).Scan(&checkpoints, &completed, &refunds, &successJournals, &unknownJournals); err != nil || checkpoints != 0 || completed != 0 || refunds != 0 || successJournals != 0 || unknownJournals != 1 {
					t.Fatalf("false completion/refund/journal: checkpoints=%d completed=%d refunds=%d successes=%d unknown=%d err=%v", checkpoints, completed, refunds, successJournals, unknownJournals, err)
				}
				account, err := w.budgets.Account(ctx, accountID)
				if err != nil || account.ReservedAmount != 1 || account.CommittedAmount != 0 {
					t.Fatalf("spent quota refunded: %+v %v", account, err)
				}
				var reservationID string
				if err = w.db.QueryRowContext(ctx, `SELECT id FROM budget_reservations WHERE budget_account_id=?`, accountID).Scan(&reservationID); err != nil {
					t.Fatal(err)
				}
				held, err := w.budgets.Reservation(ctx, reservationID)
				if err != nil || held.Status != budget.ReservationReserved || held.TaskID == nil || *held.TaskID != run.TaskID {
					t.Fatalf("held quota lost: %+v %v", held, err)
				}
				if stepKind == "model" && (held.InferenceRequestID == nil || *held.InferenceRequestID != requestID) {
					t.Fatalf("model quota owner lost: %+v", held)
				}
				if stepKind == "agent_runtime" && (held.AgentRuntimeInvocationID == nil || *held.AgentRuntimeInvocationID != requestID) {
					t.Fatalf("runtime quota owner lost: %+v", held)
				}
				if calls.Load() != 1 {
					t.Fatalf("spent dispatch replayed: calls=%d", calls.Load())
				}
			}
			assertOriginal()
			if _, err := w.Tick(ctx, 1); err != nil {
				t.Fatal(err)
			}
			assertOriginal()
			reconstructed := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, w.verification)
			if count, err := reconstructed.RecoverLostRuns(ctx); err != nil || count != 0 {
				t.Fatalf("reconstructed recovery replayed original: count=%d err=%v", count, err)
			}
			if _, err := reconstructed.Tick(ctx, 1); err != nil {
				t.Fatal(err)
			}
			assertOriginal()
			// Expiry maintenance is another execution owner. Advancing the
			// reservation past its TTL cannot turn unresolved spent quota into
			// fresh admission capacity after the Worker has been interrupted.
			if _, err := w.db.ExecContext(ctx, `UPDATE budget_reservations SET reserved_at=1,expires_at=2 WHERE budget_account_id=?`, accountID); err != nil {
				t.Fatal(err)
			}
			if n, err := w.budgets.ExpireDue(ctx, 10); err != nil || n != 0 {
				t.Fatalf("maintenance refunded unresolved transport accounting: count=%d err=%v", n, err)
			}
			assertOriginal()
		})
	}
}
