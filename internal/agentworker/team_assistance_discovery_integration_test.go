//go:build integration

package agentworker

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

func TestTeamAssistanceDiscoveryActualWaitReadOnlyAndHistory(t *testing.T) {
	w, ctx, run, teams, c := prepareTeamAssistanceAnswer(t)
	// No answer is written. Listing must discover the actual waiting original
	// owner without changing any execution, assistance, room or accounting rows.
	before := assistanceAnswerReadSnapshot(t, w, ctx)
	out, err := teams.AssistanceRequests(ctx, c.WorkspaceID, c.SessionID, c.ActorPrincipalID)
	if err != nil || len(out) != 1 || out[0].RequestID != c.RequestID || out[0].WaitStepID != c.WaitStepID || out[0].TaskID != run.TaskID || out[0].AttemptID != run.AttemptID || out[0].RunID != run.ID || out[0].Question != "Review the recorded contribution?" {
		t.Fatalf("listing %+v %v", out, err)
	}
	if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
		t.Fatal("discovery mutated retained state")
	}
	if _, err = w.db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	readonly, readErr := teams.AssistanceRequests(ctx, c.WorkspaceID, c.SessionID, c.ActorPrincipalID)
	if _, err = w.db.ExecContext(ctx, `PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	if readErr != nil || !reflect.DeepEqual(out, readonly) {
		t.Fatalf("discovery requires database writes %+v %v", readonly, readErr)
	}
	// Physical history fixtures are deliberately unrelated to an accepted owner.
	// They prove discovery is not a global newest-request window; no claim of
	// valid historical execution or model acceptance is made by these rows.
	for _, query := range []string{
		`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<1024) INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'discovery-history-'||i,'tenant','Unrelated history','created','{}',i,i FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,metadata_json) SELECT id,id,1,'waiting','system:agent-worker','{}' FROM tasks WHERE id LIKE 'discovery-history-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT id,'tenant',id,id,'system:agent-worker','waiting','general','inference.general','L1',8,2,2,'{}','{}',1,1 FROM tasks WHERE id LIKE 'discovery-history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id,id,1,'human','waiting','human','{}',1,1 FROM agent_worker_runs WHERE id LIKE 'discovery-history-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT step.id,h.workspace_id,h.session_id,h.member_id,h.author_principal_id,'system','{}',0,2000000000000+step.step_number FROM agent_worker_steps step JOIN team_assistance_requests h ON h.id=? WHERE step.id LIKE 'discovery-history-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT step.id,h.workspace_id,h.session_id,h.plan_id,h.assignment_key,h.member_id,r.task_id,r.attempt_id,r.id,step.id,step.id,h.author_principal_id,'Unrelated history',2000000000000+step.step_number FROM agent_worker_steps step JOIN agent_worker_runs r ON r.id=step.run_id JOIN team_assistance_requests h ON h.id=? WHERE step.id LIKE 'discovery-history-%'`,
	} {
		var args []any
		if strings.Contains(query, "h.id=?") {
			args = []any{c.RequestID}
		}
		if _, err = w.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	before = assistanceAnswerReadSnapshot(t, w, ctx)
	// Extract the actual production candidate SQL, rather than a copied query.
	syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "team", "assistance_discovery.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	candidateSQL := ""
	ast.Inspect(syntax, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "assistanceDiscoveryCandidateSQL" || len(spec.Values) != 1 {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatal("production discovery SQL is no longer a reviewed literal")
		}
		candidateSQL, err = strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		return false
	})
	if candidateSQL == "" {
		t.Fatal("missing production discovery candidate SQL")
	}
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			if _, err = w.db.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		after, err := teams.AssistanceRequests(ctx, c.WorkspaceID, c.SessionID, c.ActorPrincipalID)
		if err != nil || !reflect.DeepEqual(out, after) {
			t.Fatalf("history evicted current owner analyzed=%v %+v %v", analyzed, after, err)
		}
		for _, taskID := range []string{run.TaskID, "missing-task"} {
			plan := assistanceContextPlan(t, w, ctx, candidateSQL, taskID)
			for _, alias := range []string{"attempt", "r", "step", "h"} {
				if !strings.Contains(plan, "SEARCH "+alias+" USING ") {
					t.Fatalf("unbounded actual discovery %s analyzed=%v: %s", alias, analyzed, plan)
				}
			}
			if strings.Contains(plan, "SCAN ") || strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("unbounded discovery plan analyzed=%v: %s", analyzed, plan)
			}
		}
	}
	if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
		t.Fatal("history discovery changed retained state")
	}
	request := out[0]
	root, err := w.tasks.Get(ctx, request.ParentTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: c.SessionID, PlanID: request.PlanID, ActorPrincipalID: c.ActorPrincipalID, ExpectedTaskRevision: root.Revision, Reason: "Assistance remains readable under a hold"}); err != nil {
		t.Fatal(err)
	}
	before = assistanceAnswerReadSnapshot(t, w, ctx)
	held, err := teams.AssistanceRequests(ctx, c.WorkspaceID, c.SessionID, c.ActorPrincipalID)
	if err != nil || len(held) != 1 || (held[0].TaskState != "paused" && held[0].TaskState != "waiting_approval") {
		t.Fatalf("held discovery %+v %v", held, err)
	}
	if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
		t.Fatal("discovery released hold")
	}
}

func TestTeamAssistanceDiscoveryEmptyStillValidatesAcceptedScope(t *testing.T) {
	for _, change := range []string{"none", "reader", "membership", "tenant", "root_seal", "child_seal", "accepted_plan", "manifest", "member", "profile", "scope"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
			query := map[string]string{
				"reader":        `UPDATE principals SET status='disabled' WHERE id='proposal-human'`,
				"membership":    `UPDATE workspace_memberships SET status='suspended' WHERE principal_id='proposal-human'`,
				"tenant":        `UPDATE workspaces SET status='archived' WHERE id='tenant'`,
				"root_seal":     `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=(SELECT task_id FROM team_sessions)`,
				"child_seal":    `UPDATE tasks SET completion_json='{}' WHERE parent_task_id IS NOT NULL`,
				"accepted_plan": `UPDATE team_sessions SET accepted_plan_id=NULL`,
				"manifest":      `UPDATE team_session_manifests SET snapshot_sha256='0000000000000000000000000000000000000000000000000000000000000000'`,
				"member":        `UPDATE team_members SET status='disabled'`,
				"profile":       `UPDATE task_execution_profiles SET execution_mode='direct' WHERE task_id=?`,
			}[change]
			if change == "scope" {
				if _, err := w.db.ExecContext(ctx, `INSERT INTO artifact_sessions(id,workspace_id,format,status,title,created_by,created_at,updated_at) VALUES('discovery-scope','tenant','text','archived','Inactive','proposal-human',1,1)`); err != nil {
					t.Fatal(err)
				}
				query = `UPDATE tasks SET artifact_session_id='discovery-scope'`
			}
			if query != "" {
				var args []any
				if strings.Contains(query, "?") {
					args = []any{run.TaskID}
				}
				if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			out, err := teams.AssistanceRequests(ctx, run.WorkspaceID, session, "proposal-human")
			if change == "none" {
				if err != nil || out == nil || len(out) != 0 {
					t.Fatalf("empty=%+v %v", out, err)
				}
			} else if err == nil || out != nil {
				t.Fatalf("invalid empty scope exposed %+v %v", out, err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("empty discovery mutated state")
			}
		})
	}
}

func TestTeamAssistanceDiscoveryRejectsChangedPendingOwnerAndUnavailableReads(t *testing.T) {
	for _, change := range []string{"foreign_reader", "foreign_session", "invalid_input", "blank_input", "cancelled", "missing_scope", "worker_tenant", "task_running", "new_step", "oversized_step", "missing_table", "database_closed", "ninth_assignment"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, c := prepareTeamAssistanceAnswer(t)
			query := map[string]string{
				"missing_scope": `DROP TRIGGER team_assistance_request_scope_retained; DELETE FROM team_assistance_request_scopes`,
				"worker_tenant": `UPDATE agent_worker_runs SET task_id=(SELECT task_id FROM team_sessions) WHERE id=?`,
				"task_running":  `UPDATE tasks SET state='running' WHERE id=?`,
				"missing_table": `ALTER TABLE team_assistance_requests RENAME TO unavailable_requests`,
			}[change]
			reader, session := c.ActorPrincipalID, c.SessionID
			switch change {
			case "foreign_reader":
				reader = "foreign"
			case "foreign_session":
				session = "foreign"
			case "invalid_input":
				reader = strings.Repeat("x", 257)
			case "blank_input":
				reader = "  "
			case "database_closed":
				if err := w.db.Close(); err != nil {
					t.Fatal(err)
				}
			case "ninth_assignment":
				for _, statement := range []string{
					`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<8) INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,created_at,updated_at) SELECT 'discovery-extra-'||i,'tenant',s.task_id,'Extra assignment','created','{}',i,i FROM n JOIN team_sessions s`,
					`INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) SELECT a.session_id,a.plan_id,t.id,a.workspace_id,a.member_id,t.id,a.parent_task_id,'[]',a.completion_sha256,a.profile_id,a.profile_revision,1 FROM tasks t JOIN team_execution_assignments a ON a.task_id=? WHERE t.id LIKE 'discovery-extra-%'`,
				} {
					var args []any
					if strings.Contains(statement, "?") {
						args = []any{run.TaskID}
					}
					if _, err := w.db.ExecContext(ctx, statement, args...); err != nil {
						t.Fatal(err)
					}
				}
			case "new_step":
				if err := w.journal(ctx, run.ID, "route", "succeeded", nil, nil, nil, nil, map[string]any{"observation": "newer nonhuman step"}); err != nil {
					t.Fatal(err)
				}
			case "oversized_step":
				if _, err := w.db.ExecContext(ctx, `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES(?,?,999,'human','waiting','human','{}',1,1)`, strings.Repeat("x", 257), run.ID); err != nil {
					t.Fatal(err)
				}
			}
			if query != "" {
				var args []any
				if strings.Contains(query, "?") {
					arg := run.ID
					if change == "task_running" {
						arg = run.TaskID
					}
					args = []any{arg}
				}
				if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			callCtx := ctx
			if change == "cancelled" {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := teams.AssistanceRequests(callCtx, c.WorkspaceID, session, reader)
			if change == "new_step" {
				if err != nil || out == nil || len(out) != 0 {
					t.Fatalf("new ordinary step listed old wait %+v %v", out, err)
				}
			} else if err == nil || out != nil {
				t.Fatalf("changed pending owner listed %+v %v", out, err)
			}
			if change == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation %v", err)
			}
			// Generic Task resume must still deny the current assisted human wait.
			if change == "foreign_reader" {
				owner, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: owner.ID, ExpectedRevision: owner.Revision}); err == nil {
					t.Fatal("discovery granted generic execution authority")
				}
			}
		})
	}
}
