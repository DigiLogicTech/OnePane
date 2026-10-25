//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type assistanceFlowEndpoint string

func (e assistanceFlowEndpoint) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return string(e), nil
}

// Bootstrap uses the explicitly synthetic wait of the shared HTTP fixture. After
// its first authenticated Continue, every model response, new human wait,
// immutable request, answer, continuation and next model dispatch goes through
// the public production Worker and authenticated HTTP APIs. The loopback model
// is deterministic source-integration transport, not physical AI acceptance.
func TestTeamAssistanceFlowHTTPActualWorkerWaitAndNextModelContext(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			f, auth, bootstrap := newAssistanceContinueHTTPFixture(t, method, true)
			f.server.SetTasks(f.tasks)
			var calls atomic.Int64
			received := make(chan agentprotocol.Request, 2)
			const question = "ACTUAL_WORKER_QUESTION: Which reviewed implementation should continue?"
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var envelope struct {
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				var request agentprotocol.Request
				for _, message := range envelope.Messages {
					if message.Role == "user" {
						if err := json.Unmarshal([]byte(message.Content), &request); err != nil {
							http.Error(w, err.Error(), 400)
							return
						}
					}
				}
				received <- request
				proposal, _ := json.Marshal(map[string]string{"question": question})
				response, _ := json.Marshal(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: request.RequestID, ProposalType: agentprotocol.ProposalHuman, Message: "Deterministic transport asks for reviewed human input", Proposal: proposal})
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(response)}}}, "usage": map[string]any{"completion_tokens": 1}})
			}))
			defer model.Close()
			var localNodeID string
			if err := f.db.SQL().QueryRowContext(ctx, `SELECT id FROM harness_nodes WHERE local=1`).Scan(&localNodeID); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('flow-provider','tenant','synthetic','Deterministic source test','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
				`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('flow-model','synthetic-flow','["text"]','{}','user_trusted',1,1)`,
				`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('flow-deployment','flow-model','flow-provider','flow-transport','{}','ready','flow-deployment-fp',1,1,1)`,
				`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('flow-compatibility','flow-deployment','inference.general','L2',0,64000,'low','supported','{}','{}','flow-profile-fp',1,1,1)`,
			} {
				if _, err := f.db.SQL().ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			store, err := artifact.NewLocalStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			artifacts := artifact.NewService(f.db.SQL(), f.db, store, clock.Real{})
			budgets := budget.NewService(f.db.SQL(), f.db, clock.Real{})
			infer := inference.NewService(f.db.SQL(), f.db, clock.Real{})
			transports := inference.NewTransportRegistry()
			if err = transports.Register("flow-transport", inference.LocalOpenAITransport{Resolver: assistanceFlowEndpoint(model.URL), Client: model.Client()}); err != nil {
				t.Fatal(err)
			}
			if err = infer.ConfigureExecution(artifacts, transports, nil, localNodeID); err != nil {
				t.Fatal(err)
			}
			if err = infer.ConfigureRuntimeCoordinator(runtimecoord.NewService(f.db.SQL(), clock.Real{})); err != nil {
				t.Fatal(err)
			}
			if err = infer.ConfigureBudget(budgets); err != nil {
				t.Fatal(err)
			}
			workspace := "tenant"
			account, err := budgets.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &workspace, Name: "Source flow requests", Unit: "requests", LimitAmount: 2})
			if err != nil {
				t.Fatal(err)
			}
			route, _ := json.Marshal(map[string]any{"routing_enabled": true, "allow_remote": true, "allow_potential_monetary_spend": true, "compute_preference": "auto", "budget_account_id": account.ID, "budget_reserve_amount": 1, "include_candidate_ids": []string{"flow-deployment"}})
			if _, err = f.db.SQL().ExecContext(ctx, `UPDATE agent_worker_runs SET route_policy_json=? WHERE id=?`, string(route), bootstrap.RunID); err != nil {
				t.Fatal(err)
			}
			newWorker := func() *agentworker.Service {
				return agentworker.New(f.db.SQL(), f.db, clock.Real{}, localNodeID, f.tasks, authority.NewService(f.db.SQL(), f.db, clock.Real{}), budgets, scheduler.NewService(f.db.SQL(), f.db, clock.Real{}), infer, nil, artifacts, nil, nil, nil, verification.NewService(f.db.SQL(), f.db, clock.Real{}))
			}
			worker := newWorker()
			f.teams.SetAssistanceContinuationWorker(worker)
			raw, _ := json.Marshal(bootstrap)
			if response := postAssistanceContinuation(t, f, string(raw), auth); response.Code != http.StatusOK {
				t.Fatalf("bootstrap Continue=%d %s", response.Code, response.Body.String())
			}
			tick := func() {
				t.Helper()
				results, err := worker.Tick(ctx, 1)
				if err != nil || len(results) != 1 || results[0].TaskID != bootstrap.TaskID || results[0].RunID != bootstrap.RunID || results[0].Status != "waiting_human" {
					t.Fatalf("actual Tick=%+v err=%v", results, err)
				}
			}
			tick()
			if calls.Load() != 1 {
				t.Fatalf("first transport calls=%d", calls.Load())
			}
			first := <-received
			if first.TaskID != bootstrap.TaskID || first.AttemptID != bootstrap.AttemptID {
				t.Fatal("first dispatch replaced original owner")
			}
			list := getAssistanceDiscovery(t, f, auth)
			if list.Code != http.StatusOK {
				t.Fatalf("actual wait discovery=%d %s", list.Code, list.Body.String())
			}
			var discovered struct {
				Requests []team.AssistanceRequest `json:"requests"`
			}
			if err = json.Unmarshal(list.Body.Bytes(), &discovered); err != nil || len(discovered.Requests) != 1 {
				t.Fatalf("actual discovered requests=%+v err=%v", discovered, err)
			}
			current := discovered.Requests[0]
			if current.RequestID == f.requestID || current.WaitStepID == bootstrap.WaitStepID || current.Question != question || current.TaskID != bootstrap.TaskID || current.AttemptID != bootstrap.AttemptID || current.RunID != bootstrap.RunID {
				t.Fatalf("not actual new original-owner wait: %+v", current)
			}
			f.requestID = current.RequestID
			const reply = "AUTHENTICATED_HUMAN_REPLY: Use the bounded reviewed implementation <authored input>."
			answerInput := teamAssistanceAnswerInput{TaskID: current.TaskID, AttemptID: current.AttemptID, RunID: current.RunID, WaitStepID: current.WaitStepID, ExpectedTaskRevision: current.TaskRevision, ExpectedWorkerRevision: current.WorkerRevision, IdempotencyKey: "actual-wait-human-answer", Text: reply}
			raw, _ = json.Marshal(answerInput)
			before := answerHTTPExecutionState(t, f)
			answered := postAssistanceAnswer(t, f, string(raw), auth)
			if answered.Code != http.StatusOK {
				t.Fatalf("actual wait authenticated answer=%d %s", answered.Code, answered.Body.String())
			}
			if answerHTTPExecutionState(t, f) != before {
				t.Fatal("answer resumed execution")
			}
			var saved team.AssistanceAnswer
			if err = json.Unmarshal(answered.Body.Bytes(), &saved); err != nil || saved.Text != reply || saved.AuthorPrincipalID != "human" {
				t.Fatalf("wrong actual answer %+v err=%v", saved, err)
			}
			continuation := teamAssistanceContinueInput{AnswerID: saved.AnswerID, TaskID: current.TaskID, AttemptID: current.AttemptID, RunID: current.RunID, WaitStepID: current.WaitStepID, ExpectedTaskRevision: current.TaskRevision, ExpectedWorkerRevision: current.WorkerRevision}
			raw, _ = json.Marshal(continuation)
			continued := postAssistanceContinuation(t, f, string(raw), auth)
			if continued.Code != http.StatusOK {
				t.Fatalf("actual wait authenticated Continue=%d %s", continued.Code, continued.Body.String())
			}
			var receipt team.AssistanceContinuation
			if err = json.Unmarshal(continued.Body.Bytes(), &receipt); err != nil || receipt.ContinuationID == "" || receipt.ContinueStepID == "" || receipt.RequestID != current.RequestID || receipt.AnswerID != saved.AnswerID || receipt.ActorPrincipalID != "human" || receipt.TaskRevision != current.TaskRevision || receipt.WorkerRevision != current.WorkerRevision {
				t.Fatalf("wrong actual continuation receipt %+v err=%v", receipt, err)
			}
			// Reconstruct from durable state and evict generic room guidance. The
			// required request-specific answer must reach the actual next request.
			for i := 0; i < 12; i++ {
				text, _ := json.Marshal(map[string]string{"text": "Later unrelated room message"})
				if _, err = f.teams.PostMessage(ctx, team.PostMessageCommand{SessionID: f.sessionID, AuthorPrincipalID: "human", Kind: "human", Content: text}); err != nil {
					t.Fatal(err)
				}
			}
			worker = newWorker()
			f.teams.SetAssistanceContinuationWorker(worker)
			tick()
			if calls.Load() != 2 {
				t.Fatalf("next transport calls=%d", calls.Load())
			}
			next := <-received
			if next.TaskID != bootstrap.TaskID || next.AttemptID != bootstrap.AttemptID {
				t.Fatal("next dispatch replaced original owner")
			}
			found := map[string]bool{}
			for _, section := range next.Context {
				var value map[string]string
				if section.ID != "team-assistance-question" && section.ID != "team-assistance-answer" {
					continue
				}
				if err = json.Unmarshal(section.Content, &value); err != nil {
					t.Fatal(err)
				}
				if value["request_id"] != current.RequestID {
					t.Fatal("next context reused synthetic bootstrap assistance")
				}
				if section.ID == "team-assistance-question" {
					if section.Trust != "UNVERIFIED_DERIVED" || section.Authoritative || value["question"] != question || value["wait_step_id"] != current.WaitStepID || value["author_principal_id"] != agentworker.WorkerPrincipal {
						t.Fatalf("wrong model question provenance %s", section.Content)
					}
				} else {
					if section.Trust != "USER_INSTRUCTION" || !section.Authoritative || value["text"] != reply || value["answer_id"] != saved.AnswerID || value["continuation_id"] != receipt.ContinuationID || value["author_principal_id"] != "human" || value["continued_by_principal_id"] != "human" {
						t.Fatalf("wrong authenticated answer provenance %s", section.Content)
					}
				}
				found[section.ID] = true
			}
			if len(found) != 2 {
				t.Fatalf("required actual question/answer absent: %+v", next.Context)
			}
			var attempts, requests, continuations, succeeded int
			if err = f.db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM task_attempts WHERE task_id=?),(SELECT COUNT(*) FROM team_assistance_requests WHERE run_id=?),(SELECT COUNT(*) FROM team_assistance_continuations),(SELECT COUNT(*) FROM inference_requests WHERE status='succeeded')`, bootstrap.TaskID, bootstrap.RunID).Scan(&attempts, &requests, &continuations, &succeeded); err != nil || attempts != 1 || requests != 3 || continuations != 2 || succeeded != 2 {
				t.Fatalf("flow owner/history=%d/%d/%d/%d err=%v", attempts, requests, continuations, succeeded, err)
			}
			if results, err := worker.Tick(ctx, 1); err != nil || len(results) != 0 || calls.Load() != 2 {
				t.Fatalf("waiting owner dispatched without another approval: results=%+v calls=%d err=%v", results, calls.Load(), err)
			}
		})
	}
}
