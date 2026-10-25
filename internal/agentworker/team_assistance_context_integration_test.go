//go:build integration

package agentworker

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
	"github.com/DigiLogicTech/OnePane/internal/contextcompiler"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func continuedAssistanceContextFixture(t *testing.T) (*Service, context.Context, Run, team.ContinueAssistanceCommand) {
	t.Helper()
	w, ctx, run, teams, c := prepareTeamAssistanceContinue(t)
	if _, err := teams.ContinueAssistance(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the Worker to prove the applied answer does not depend on
	// transient service state, the last room messages, or continuation_json.
	w = New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, verification.NewService(w.db, w.tx, w.clock))
	var err error
	run, err = w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return w, ctx, run, c
}

func requireAssistanceContextSections(t *testing.T, sections []agentprotocol.ContextSection, c team.ContinueAssistanceCommand) {
	t.Helper()
	found := map[string]bool{}
	for _, section := range sections {
		switch section.ID {
		case "team-assistance-question":
			var question map[string]string
			if err := json.Unmarshal(section.Content, &question); err != nil {
				t.Fatal(err)
			}
			if section.Trust != "UNVERIFIED_DERIVED" || section.Authoritative || question["request_id"] != c.RequestID || question["wait_step_id"] != c.WaitStepID || question["author_principal_id"] != WorkerPrincipal || question["question"] != "Review the recorded contribution?" {
				t.Fatalf("question provenance %+v %s", section, section.Content)
			}
			found[section.ID] = true
		case "team-assistance-answer":
			var answer map[string]string
			if err := json.Unmarshal(section.Content, &answer); err != nil {
				t.Fatal(err)
			}
			if section.Trust != "USER_INSTRUCTION" || !section.Authoritative || answer["request_id"] != c.RequestID || answer["answer_id"] != c.AnswerID || answer["author_principal_id"] != "proposal-human" || answer["continued_by_principal_id"] != c.ActorPrincipalID || answer["continuation_id"] == "" || answer["text"] != "Use the bounded reviewed approach <authored input>." {
				t.Fatalf("answer provenance %+v %s", section, section.Content)
			}
			found[section.ID] = true
		}
	}
	if len(found) != 2 {
		t.Fatalf("required request-specific question/answer absent: %+v", sections)
	}
}

func TestTeamAssistanceContextSurvivesRoomTrafficAndContinuationReplacement(t *testing.T) {
	w, ctx, run, c := continuedAssistanceContextFixture(t)
	teams := team.NewService(w.db, w.tx, w.clock, w.tasks)
	for i := 0; i < 12; i++ {
		raw, _ := json.Marshal(map[string]string{"text": fmt.Sprintf("Later unrelated message %d", i)})
		if _, err := teams.PostMessage(ctx, team.PostMessageCommand{SessionID: c.SessionID, AuthorPrincipalID: "proposal-human", Kind: "human", Content: raw}); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the actual resource suspension and dependency resume path. The
	// receipt must survive both replacement of continuation_json and new journals.
	result := w.waitForLocalModel(ctx, run, TickResult{TaskID: run.TaskID, RunID: run.ID}, "Synthetic model resource wait")
	if result.Status != "waiting_model" {
		t.Fatalf("resource wait %+v", result)
	}
	owner, err := w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: owner.ID, ExpectedRevision: owner.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = w.syncResumedRuns(ctx); err != nil {
		t.Fatal(err)
	}
	run, err = w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := w.compileContext(ctx, run, owner)
	if err != nil {
		t.Fatal(err)
	}
	requireAssistanceContextSections(t, compiled.Sections, c)
	for _, entry := range compiled.Manifest.Entries {
		if strings.HasPrefix(entry.ID, "team-assistance-") && !entry.Included {
			t.Fatalf("answer silently evicted %+v", entry)
		}
	}
	w.cfg.ContextMaxBytes = 1
	if _, err = w.compileContext(ctx, run, owner); !errors.Is(err, contextcompiler.ErrRequiredOverflow) {
		t.Fatalf("required overflow did not fail closed: %v", err)
	}
}

func TestTeamAssistanceContextRejectsRevokedOrMissingAuthority(t *testing.T) {
	for _, change := range []string{"human", "membership", "scope", "original_scope", "receipt", "new_unanswered_wait", "oversized_latest_wait"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, _ := continuedAssistanceContextFixture(t)
			query := map[string]string{
				"human":      `UPDATE principals SET status='disabled' WHERE id='proposal-human'`,
				"membership": `UPDATE workspace_memberships SET status='suspended' WHERE workspace_id='tenant' AND principal_id='proposal-human'`,
				"scope":      `UPDATE workspaces SET status='archived' WHERE id='tenant'`,
				"receipt":    `DROP TRIGGER team_assistance_continuation_retained; DELETE FROM team_assistance_continuations`,
			}[change]
			if change == "original_scope" {
				if _, err := w.db.ExecContext(ctx, `INSERT INTO artifact_sessions(id,workspace_id,format,status,title,created_by,created_at,updated_at) VALUES('moved-scope','tenant','text','active','Moved','proposal-human',1,1)`); err != nil {
					t.Fatal(err)
				}
				if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET artifact_session_id='moved-scope' WHERE id=?`, run.TaskID); err != nil {
					t.Fatal(err)
				}
			} else if change == "oversized_latest_wait" {
				if _, err := w.db.ExecContext(ctx, `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES(?,?,999,'human','waiting','human','{}',1,1)`, strings.Repeat("x", 257), run.ID); err != nil {
					t.Fatal(err)
				}
			} else if change == "new_unanswered_wait" {
				if err := w.journal(ctx, run.ID, "model", "succeeded", strPtr("model_deployment"), strPtr("prior"), strPtr("human"), nil, map[string]any{"message": "new question"}); err != nil {
					t.Fatal(err)
				}
				if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
					t.Fatalf("new wait %+v", result)
				}
			} else if _, err := w.db.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
			if sections, err := assistanceContext(ctx, w.db, run); err == nil || len(sections) != 0 {
				t.Fatalf("revoked/latest missing input reused older answer: %v %+v", err, sections)
			}
		})
	}
}

// This is real source integration through the production Worker, inference
// gateway, local HTTP transport, artifacts and journals. The endpoint is
// deterministic; it does not establish physical model or hardware acceptance.
func TestTeamAssistanceContextNextModelRequestAndRevocationBoundaries(t *testing.T) {
	for _, revoke := range []string{"none", "before_dispatch", "during_transport"} {
		t.Run(revoke, func(t *testing.T) {
			w, ctx, run, c := continuedAssistanceContextFixture(t)
			var calls atomic.Int64
			received := make(chan agentprotocol.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var envelope struct {
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
					http.Error(rw, err.Error(), 400)
					return
				}
				var req agentprotocol.Request
				for _, m := range envelope.Messages {
					if m.Role == "user" {
						if err := json.Unmarshal([]byte(m.Content), &req); err != nil {
							http.Error(rw, err.Error(), 400)
							return
						}
					}
				}
				received <- req
				if revoke == "during_transport" {
					if _, err := w.db.ExecContext(ctx, `UPDATE principals SET status='disabled' WHERE id='proposal-human'`); err != nil {
						http.Error(rw, err.Error(), 500)
						return
					}
				}
				response := agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalHuman, Message: "Synthetic next model", Proposal: json.RawMessage(`{"question":"Next reviewed question?"}`)}
				raw, _ := json.Marshal(response)
				rw.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(rw).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}, "usage": map[string]any{"completion_tokens": 1}})
			}))
			defer server.Close()
			configureAssistanceContextTransport(t, w, ctx, &run, server)
			prepared := w.prepareStep(ctx, run)
			if prepared.dispatch == nil {
				t.Fatalf("no next model prepared %+v", prepared.result)
			}
			if revoke == "before_dispatch" {
				if _, err := w.db.ExecContext(ctx, `UPDATE principals SET status='disabled' WHERE id='proposal-human'`); err != nil {
					t.Fatal(err)
				}
			}
			result := prepared.dispatch()()
			if revoke == "before_dispatch" {
				if calls.Load() != 0 || result.Status != "blocked" {
					t.Fatalf("revoked preparation dispatched calls=%d %+v", calls.Load(), result)
				}
				return
			}
			if calls.Load() != 1 {
				t.Fatalf("actual transport calls=%d %+v", calls.Load(), result)
			}
			req := <-received
			if req.TaskID != run.TaskID || req.AttemptID != run.AttemptID {
				t.Fatalf("owner replaced %+v", req)
			}
			requireAssistanceContextSections(t, req.Context, c)
			var requests int
			if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_requests WHERE run_id=?`, run.ID).Scan(&requests); err != nil {
				t.Fatal(err)
			}
			if revoke == "none" {
				if result.Status != "waiting_human" || requests != 2 {
					t.Fatalf("next proposal not applied %+v requests=%d", result, requests)
				}
			} else if result.Status != "blocked" || requests != 1 {
				t.Fatalf("revoked response applied %+v requests=%d", result, requests)
			}
		})
	}
}

func configureAssistanceContextTransport(t *testing.T, w *Service, ctx context.Context, run *Run, server *httptest.Server) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node-local','Local',1,'assistance-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('context-provider','tenant','synthetic','Synthetic','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
		`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('context-model','synthetic-context','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('context-deployment','context-model','context-provider','context-transport','{}','ready','context-fp',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('context-compatibility','context-deployment','inference.general','L2',0,64000,'low','supported','{}','{}','context-cp',1,1,1)`,
	} {
		if _, err := w.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
	w.budgets = budget.NewService(w.db, w.tx, w.clock)
	w.scheduler = scheduler.NewService(w.db, w.tx, w.clock)
	w.inference = inference.NewService(w.db, w.tx, w.clock)
	transports := inference.NewTransportRegistry()
	if err = transports.Register("context-transport", inference.LocalOpenAITransport{Resolver: responseJournalEndpoint(server.URL), Client: server.Client()}); err != nil {
		t.Fatal(err)
	}
	if err = w.inference.ConfigureExecution(w.artifacts, transports, nil, "node-local"); err != nil {
		t.Fatal(err)
	}
	if err = w.inference.ConfigureRuntimeCoordinator(runtimecoord.NewService(w.db, w.clock)); err != nil {
		t.Fatal(err)
	}
	if err = w.inference.ConfigureBudget(w.budgets); err != nil {
		t.Fatal(err)
	}
	workspace := run.WorkspaceID
	account, err := w.budgets.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &workspace, Name: "Context test", Unit: "requests", LimitAmount: 2})
	if err != nil {
		t.Fatal(err)
	}
	rp := defaultRoutePolicy()
	rp.AllowPotentialMonetarySpend = true
	rp.BudgetAccountID = account.ID
	rp.BudgetReserveAmount = 1
	raw, _ := json.Marshal(rp)
	if _, err = w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET route_policy_json=? WHERE id=?`, string(raw), run.ID); err != nil {
		t.Fatal(err)
	}
	*run, err = w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTeamAssistanceContextProductionLookupPlansWithHistory(t *testing.T) {
	w, ctx, run, c := continuedAssistanceContextFixture(t)
	// These unrelated historical rows exercise the physical lookup plans only.
	// They deliberately have no accepted execution profiles and are not claimed
	// to be valid accepted contribution graphs or executable continuation evidence.
	const sequence = `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<1024) `
	queries := []string{
		sequence + `INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'ctx-history-'||i,'tenant','Unrelated history','created','{}',i,i FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,started_at,metadata_json) SELECT id,id,1,'waiting','system:agent-worker',1,'{"agent_worker":"v1"}' FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT id,'tenant',id,id,'system:agent-worker','waiting','general','inference.general','L1',8,2,2,'{}','{}',1,1 FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id||'-wait',id,1,'human','waiting','human','{}',1,1 FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id||'-continued',id,2,'human','succeeded','human','{}',2,2 FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT t.id,'tenant',h.session_id,h.member_id,h.author_principal_id,'system','{}',0,t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'ctx-history-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT t.id,'tenant',h.session_id,h.plan_id,h.assignment_key,h.member_id,t.id,t.id,t.id,t.id||'-wait',t.id,h.author_principal_id,'Historical question',t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'ctx-history-%'`,
		`INSERT INTO team_assistance_request_scopes(request_id) SELECT id FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO team_assistance_answers(request_id,id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at) SELECT id,id,'proposal-human','history','Historical answer',1,1,1 FROM tasks WHERE id LIKE 'ctx-history-%'`,
		`INSERT INTO team_assistance_continuations(request_id,id,answer_id,actor_principal_id,continue_step_id,task_revision,worker_revision,created_at) SELECT id,id,id,'proposal-human',id||'-continued',1,1,2 FROM tasks WHERE id LIKE 'ctx-history-%'`,
		sequence + `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,detail_json,started_at,completed_at) SELECT 'ctx-route-'||i,?,i+100,'route','succeeded','{}',i,i FROM n`,
	}
	for _, query := range queries {
		var args []any
		if strings.Contains(query, "h.id=?") {
			args = []any{c.RequestID}
		}
		if strings.Contains(query, "'ctx-route-'") {
			args = []any{run.ID}
		}
		if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	latest := assistanceLatestWaitSQL
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			if _, err := w.db.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		for _, queryRun := range []string{run.ID, "missing-run", "ctx-history-1"} {
			plan := assistanceContextPlan(t, w, ctx, latest, queryRun)
			if !strings.Contains(plan, "SEARCH agent_worker_steps USING INDEX idx_agent_worker_human_wait") || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("latest lookup analyzed=%v: %s", analyzed, plan)
			}
		}
		var step string
		if err := w.db.QueryRowContext(ctx, latest, run.ID).Scan(&step); err != nil || step != c.WaitStepID {
			t.Fatalf("latest exact wait=%q err=%v", step, err)
		}
		for _, scenario := range []string{"present", "missing", "foreign_attempt"} {
			stepID, attemptID := c.WaitStepID, run.AttemptID
			if scenario == "missing" {
				stepID = "missing-wait"
			}
			if scenario == "foreign_attempt" {
				attemptID = "ctx-history-1"
			}
			args := []any{stepID, run.ID, run.WorkspaceID, run.TaskID, attemptID}
			plan := assistanceContextPlan(t, w, ctx, assistanceContextReadSQL, args...)
			for _, alias := range []string{"h", "a", "c", "waiting", "continued", "r", "attempt"} {
				if !strings.Contains(plan, "SEARCH "+alias+" USING ") || strings.Contains(plan, "SCAN "+alias+"\n") || strings.Contains(plan, "SEARCH "+alias+" USING AUTOMATIC") {
					t.Fatalf("unbounded %s %s analyzed=%v: %s", scenario, alias, analyzed, plan)
				}
			}
			rows, err := w.db.QueryContext(ctx, assistanceContextReadSQL, args...)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if err = rows.Close(); err != nil {
				t.Fatal(err)
			}
			want := 0
			if scenario == "present" {
				want = 1
			}
			if count != want {
				t.Fatalf("%s analyzed=%v rows=%d want=%d", scenario, analyzed, count, want)
			}
		}
	}
}

func assistanceContextPlan(t *testing.T, w *Service, ctx context.Context, query string, args ...any) string {
	t.Helper()
	rows, err := w.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func TestTeamAssistanceContextPreservesDistinctAuthorAndContinuer(t *testing.T) {
	for _, revoke := range []string{"none", "answer_author", "continuer"} {
		t.Run(revoke, func(t *testing.T) {
			w, ctx, run, teams, c := prepareTeamAssistanceContinue(t)
			for _, query := range []string{
				`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('continuing-human','human','Another operator','active',1,1,1)`,
				`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','continuing-human','active',1,1)`,
			} {
				if _, err := w.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			c.ActorPrincipalID = "continuing-human"
			if _, err := teams.ContinueAssistance(ctx, c); err != nil {
				t.Fatal(err)
			}
			var err error
			run, err = w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			sections, err := assistanceContext(ctx, w.db, run)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := contextcompiler.Compile(64<<10, sections)
			if err != nil {
				t.Fatal(err)
			}
			requireAssistanceContextSections(t, compiled.Sections, c)
			if revoke != "none" {
				principal := "proposal-human"
				if revoke == "continuer" {
					principal = "continuing-human"
				}
				if _, err = w.db.ExecContext(ctx, `UPDATE principals SET status='disabled' WHERE id=?`, principal); err != nil {
					t.Fatal(err)
				}
				if sections, err = assistanceContext(ctx, w.db, run); err == nil || len(sections) != 0 {
					t.Fatalf("distinct %s revocation escaped %v %+v", revoke, err, sections)
				}
			}
		})
	}
}
