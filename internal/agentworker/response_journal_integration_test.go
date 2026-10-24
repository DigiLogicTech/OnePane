//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
)

type responseJournalEndpoint string

func (e responseJournalEndpoint) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return string(e), nil
}

func TestResponseJournalFailureDoesNotApplySuccessfulProposal(t *testing.T) {
	var cases []struct{ kind, failure string }
	for _, kind := range []string{"model", "agent_runtime"} {
		for _, failure := range []string{"none", "step", "audit", "recovery"} {
			cases = append(cases, struct{ kind, failure string }{kind, failure})
		}
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.failure, func(t *testing.T) {
			failure, stepKind := tc.failure, tc.kind
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
			statement := fmt.Sprintf(`CREATE TRIGGER reject_response_journal BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='%s' AND NEW.status='succeeded' BEGIN SELECT RAISE(ABORT,'response journal rejected'); END`, stepKind)
			if failure == "audit" {
				statement = fmt.Sprintf(`CREATE TRIGGER reject_response_journal BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.kind')='%s' AND json_extract(NEW.payload_json,'$.status')='succeeded' BEGIN SELECT RAISE(ABORT,'response audit rejected'); END`, stepKind)
			}
			if failure == "recovery" {
				statement = fmt.Sprintf(`CREATE TRIGGER reject_response_journal BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='%s' BEGIN SELECT RAISE(ABORT,'response and recovery journal rejected'); END`, stepKind)
			}
			if failure != "none" {
				if _, err := w.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			prepared := w.prepareStep(ctx, run)
			if prepared.dispatch == nil {
				t.Fatalf("no real dispatch prepared: %+v", prepared.result)
			}
			result := prepared.dispatch()()
			var taskState, attemptState, workerState, requestState string
			var completeEvents, checkpoints, successJournals int
			if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, run.ID).Scan(&taskState, &attemptState, &workerState); err != nil {
				t.Fatal(err)
			}
			requestQuery := `SELECT status FROM inference_requests WHERE task_id=?`
			if stepKind == "agent_runtime" {
				requestQuery = `SELECT status FROM agent_runtime_invocations WHERE task_id=?`
			}
			if err := w.db.QueryRowContext(ctx, requestQuery, run.TaskID).Scan(&requestState); err != nil {
				t.Fatal(err)
			}
			if err := w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.completed'),(SELECT COUNT(*) FROM checkpoints WHERE task_id=?),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind=? AND status='succeeded')`, run.TaskID, run.TaskID, run.ID, stepKind).Scan(&completeEvents, &checkpoints, &successJournals); err != nil {
				t.Fatal(err)
			}
			account, err := w.budgets.Account(ctx, accountID)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || requestState != "succeeded" {
				t.Fatalf("not actual successful loopback inference: calls=%d state=%s result=%+v", calls.Load(), requestState, result)
			}
			var responseID string
			responseQuery := `SELECT a.id FROM artifacts a JOIN inference_requests q ON q.id=json_extract(a.metadata_json,'$.inference_request_id') WHERE q.task_id=? AND a.media_type='application/vnd.onepane.agent-response+json'`
			if stepKind == "agent_runtime" {
				responseQuery = `SELECT response_artifact_id FROM agent_runtime_invocations WHERE task_id=?`
			}
			if err := w.db.QueryRowContext(ctx, responseQuery, run.TaskID).Scan(&responseID); err != nil {
				t.Fatalf("successful canonical response missing: %v", err)
			}
			if err := w.artifacts.VerifyContent(ctx, responseID); err != nil {
				t.Fatalf("retained response content: %v", err)
			}
			if failure == "none" {
				if result.Status != "complete" || result.Proposal != "complete" || taskState != "complete" || attemptState != "succeeded" || workerState != "succeeded" || completeEvents != 1 || checkpoints != 1 || successJournals != 1 || account.CommittedAmount != 1 {
					t.Fatalf("normal successful proposal regressed: result=%+v Task/Attempt/Worker=%s/%s/%s completed=%d checkpoints=%d response_journals=%d account=%+v", result, taskState, attemptState, workerState, completeEvents, checkpoints, successJournals, account)
				}
				return
			}
			if failure == "recovery" {
				if result.Status != "failed" || !w.dispatchReconciliationFailed || taskState != "running" || attemptState != "running" || workerState != "running" || completeEvents != 0 || checkpoints != 0 || successJournals != 0 || account.CommittedAmount != 1 || result.Proposal != "" {
					t.Fatalf("failed recovery partially committed or applied proposal: result=%+v Task/Attempt/Worker=%s/%s/%s completed=%d checkpoints=%d response_journals=%d account=%+v", result, taskState, attemptState, workerState, completeEvents, checkpoints, successJournals, account)
				}
				if _, err := w.Tick(ctx, 1); !errors.Is(err, ErrDispatchRequiresReconciliation) || calls.Load() != 1 {
					t.Fatalf("failed reconciliation permitted replay: calls=%d err=%v", calls.Load(), err)
				}
				if n, err := w.RecoverLostRuns(ctx); err != nil || n != 1 || w.dispatchReconciliationFailed {
					t.Fatalf("startup recovery: count=%d err=%v halted=%v", n, err, w.dispatchReconciliationFailed)
				}
				var state, attempt, worker string
				var attempts int
				if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=t.id) FROM tasks t JOIN task_attempts a ON a.id=? AND a.task_id=t.id JOIN agent_worker_runs r ON r.id=? AND r.task_id=t.id AND r.attempt_id=a.id WHERE t.id=?`, run.AttemptID, run.ID, run.TaskID).Scan(&state, &attempt, &worker, &attempts); err != nil || state != "blocked" || attempt != "interrupted" || worker != "interrupted" || attempts != 1 || calls.Load() != 1 {
					t.Fatalf("startup recovery lost original owner/replayed response: %s/%s/%s attempts=%d calls=%d err=%v", state, attempt, worker, attempts, calls.Load(), err)
				}
				return
			}
			if result.Status != "blocked" || result.Proposal != "" || taskState != "blocked" || attemptState != "interrupted" || workerState != "interrupted" || completeEvents != 0 || checkpoints != 0 || successJournals != 0 || account.CommittedAmount != 1 {
				t.Fatalf("response-journal failure applied proposal or lost original execution: result=%+v Task/Attempt/Worker=%s/%s/%s completed=%d checkpoints=%d response_journals=%d account=%+v", result, taskState, attemptState, workerState, completeEvents, checkpoints, successJournals, account)
			}
			var ref, refKind, outcome string
			expectedRef, expectedKind := responseID, "agent_response_artifact"
			if stepKind == "agent_runtime" {
				expectedKind = "agent_runtime_invocation"
				if err := w.db.QueryRowContext(ctx, `SELECT id FROM agent_runtime_invocations WHERE task_id=?`, run.TaskID).Scan(&expectedRef); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.db.QueryRowContext(ctx, `SELECT result_ref,json_extract(detail_json,'$.result_ref_kind'),json_extract(detail_json,'$.outcome') FROM agent_worker_steps WHERE run_id=? AND step_kind=? AND status='unknown'`, run.ID, stepKind).Scan(&ref, &refKind, &outcome); err != nil || ref != expectedRef || refKind != expectedKind || outcome != "response_journal_uncommitted" {
				t.Fatalf("uncommitted response lost typed successful reference: %s/%s/%s want=%s err=%v", ref, refKind, outcome, expectedRef, err)
			}
			if _, err := w.Tick(ctx, 1); err != nil || calls.Load() != 1 {
				t.Fatalf("held response retried: calls=%d err=%v", calls.Load(), err)
			}
			if n, err := w.RecoverLostRuns(ctx); err != nil || n != 0 {
				t.Fatalf("committed reconciliation repeated on restart: count=%d err=%v", n, err)
			}
		})
	}
}

func TestResponseJournalRecoveryRejectsChangedWorkspace(t *testing.T) {
	for _, changed := range []string{"worker", "task", "supplied_run"} {
		t.Run(changed, func(t *testing.T) {
			w, ctx, run, cleanup := prepareUnknownDispatch(t)
			defer cleanup()
			if _, err := w.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "worker":
				if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET workspace_id='foreign' WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "task":
				if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET workspace_id='foreign' WHERE id=?`, run.TaskID); err != nil {
					t.Fatal(err)
				}
			case "supplied_run":
				run.WorkspaceID = "foreign"
			}
			result := w.interruptDispatchForReconciliation(ctx, run, TickResult{TaskID: run.TaskID, RunID: run.ID}, "model", "model_deployment", "candidate", "response-artifact", errResponseJournalUncommitted)
			var taskState, attemptState, workerState string
			var audits int
			if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM events WHERE aggregate_id=r.id AND event_type='agent_worker.step') FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, run.ID).Scan(&taskState, &attemptState, &workerState, &audits); err != nil {
				t.Fatal(err)
			}
			if result.Status != "failed" || !w.dispatchReconciliationFailed || taskState != "running" || attemptState != "running" || workerState != "running" || audits != 0 {
				t.Fatalf("foreign recovery changed ownership: result=%+v Task/Attempt/Worker=%s/%s/%s audits=%d halted=%v", result, taskState, attemptState, workerState, audits, w.dispatchReconciliationFailed)
			}
		})
	}
}
