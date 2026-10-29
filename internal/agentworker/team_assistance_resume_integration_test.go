//go:build integration

package agentworker

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

// Actual Worker human proposal -> immutable request. A generic Task resume
// must not stand in for the pending answer-bound explicit Continue protocol.
func TestTeamAssistanceGenericResumeRequiresExplicitContinue(t *testing.T) {
	for _, answered := range []bool{false, true} {
		name := "unanswered"
		if answered {
			name = "answered"
		}
		t.Run(name, func(t *testing.T) {
			w, ctx, run, teams, command := prepareTeamAssistanceAnswer(t)
			if answered {
				if _, err := teams.AnswerAssistance(ctx, command); err != nil {
					t.Fatal(err)
				}
			}
			// Reconstructed Task services cannot rely on in-memory assistance state.
			tasks := task.NewService(w.db, w.tx, w.clock)
			tasks.SetAdmissionGuard(teams)
			current, err := tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			_, err = tasks.Resume(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision})
			if err == nil {
				if syncErr := w.syncResumedRuns(ctx); syncErr != nil {
					t.Fatal(syncErr)
				}
				restored, getErr := w.getRun(ctx, run.ID)
				if getErr != nil {
					t.Fatal(getErr)
				}
				t.Fatalf("generic resume bypassed explicit Continue: Worker status=%s revision=%d", restored.Status, restored.Revision)
			}
			if !errors.Is(err, task.ErrInvalidTransition) {
				t.Fatalf("wrong generic resume refusal: %v", err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("denied generic resume changed original owner/history")
			}
			if err = w.syncResumedRuns(ctx); err != nil {
				t.Fatal(err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("reconciliation promoted denied original owner")
			}
		})
	}
}

func TestTeamAssistanceGenericResumeRetainsScopeDriftFence(t *testing.T) {
	for _, change := range []string{"artifact_scope", "task_workspace", "paused_projection"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, _ := prepareTeamAssistanceAnswer(t)
			switch change {
			case "artifact_scope":
				if _, err := w.db.ExecContext(ctx, `INSERT INTO artifact_sessions(id,workspace_id,format,status,title,created_by,created_at,updated_at) VALUES('resume-scope','tenant','text','active','Changed','proposal-human',1,1); UPDATE tasks SET artifact_session_id='resume-scope' WHERE id=? OR id=(SELECT parent_task_id FROM tasks WHERE id=?)`, run.TaskID, run.TaskID); err != nil {
					t.Fatal(err)
				}
			case "task_workspace":
				if _, err := w.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('resume-foreign','Changed','active',1,1); UPDATE tasks SET workspace_id='resume-foreign' WHERE id=?`, run.TaskID); err != nil {
					t.Fatal(err)
				}
			case "paused_projection":
				if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET state='paused' WHERE id=?`, run.TaskID); err != nil {
					t.Fatal(err)
				}
			}
			tasks := task.NewService(w.db, w.tx, w.clock)
			tasks.SetAdmissionGuard(teams)
			current, err := tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			if _, err = tasks.Resume(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision}); !errors.Is(err, task.ErrInvalidTransition) {
				t.Fatalf("scope drift weakened current assistance fence: %v", err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("refused scope-drift resume changed original owner/history")
			}
		})
	}
}

func TestTeamAssistanceGenericResumeIgnoresHistoricalHumanStep(t *testing.T) {
	w, ctx, run, _, _ := prepareTeamAssistanceAnswer(t)
	// Deliberately construct the durable projection after a later ordinary wait.
	// This is a historical-state control, not a synthetic authorization to
	// Continue the earlier request. The latest step is what owns this wait.
	if _, err := w.db.ExecContext(ctx, `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at)
 SELECT 'ordinary-after-assisted',id,(SELECT MAX(step_number)+1 FROM agent_worker_steps WHERE run_id=r.id),'wait','waiting','wait','{}',1,2 FROM agent_worker_runs r WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET state='paused' WHERE id=?`, run.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET continuation_json='{"wait_reason":"ordinary"}' WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	current, err := w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision}); err != nil {
		t.Fatalf("historical request blocked later ordinary wait: %v", err)
	}
	if err = w.syncResumedRuns(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := w.getRun(ctx, run.ID)
	if err != nil || restored.Status != RunRunning {
		t.Fatalf("ordinary owner not restored: %+v %v", restored, err)
	}
	var requests, attempts int
	if err = w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_assistance_requests WHERE task_id=?),(SELECT COUNT(*) FROM task_attempts WHERE task_id=?)`, run.TaskID, run.TaskID).Scan(&requests, &attempts); err != nil || requests != 1 || attempts != 1 {
		t.Fatalf("historical request/original Attempt changed: %d/%d %v", requests, attempts, err)
	}
}

func TestTeamAssistanceGenericResumeWithoutAssistanceSchema(t *testing.T) {
	for _, kind := range []string{"human", "wait"} {
		t.Run(kind, func(t *testing.T) {
			w, ctx, run := prepareProposalContinuation(t, kind)
			// Test-owned old-schema control. There are no Team requests or answers
			// in this ordinary Task fixture; retained user records are not removed.
			if _, err := w.db.ExecContext(ctx, `DROP TABLE team_assistance_answers; DROP TABLE team_assistance_request_scopes; DROP TABLE team_assistance_requests`); err != nil {
				t.Fatal(err)
			}
			if result := applyWorkerWait(t, w, ctx, run, kind); result.Status == "failed" {
				t.Fatalf("ordinary wait: %+v", result)
			}
			current, err := w.tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision}); err != nil {
				t.Fatalf("old-schema ordinary resume: %v", err)
			}
			if err = w.syncResumedRuns(ctx); err != nil {
				t.Fatal(err)
			}
			restored, err := w.getRun(ctx, run.ID)
			if err != nil || restored.Status != RunRunning {
				t.Fatalf("old-schema owner not restored: %+v %v", restored, err)
			}
		})
	}
}

func TestTeamAssistanceGenericResumePreexistingProjectionRequiresReconciliation(t *testing.T) {
	for _, answered := range []bool{false, true} {
		name := "unanswered"
		if answered {
			name = "answered"
		}
		t.Run(name, func(t *testing.T) {
			w, ctx, run, teams, command := prepareTeamAssistanceAnswer(t)
			if answered {
				if _, err := teams.AnswerAssistance(ctx, command); err != nil {
					t.Fatal(err)
				}
			}
			// Test-only projection of the previously demonstrated generic resume,
			// as left by an older source before its Worker reconciler ran. No new
			// answer/application receipt or purported Continue is manufactured.
			if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET state='running',revision=revision+1 WHERE id=?`, run.TaskID); err != nil {
				t.Fatal(err)
			}
			if _, err := w.db.ExecContext(ctx, `UPDATE task_attempts SET status='running' WHERE id=?`, run.AttemptID); err != nil {
				t.Fatal(err)
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			restarted := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			if err := restarted.syncResumedRuns(ctx); !errors.Is(err, ErrInvalidWorkerState) {
				t.Fatalf("preexisting generic-resumed Task did not retain operator-reconciliation refusal: %v", err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("preexisting projection was overwritten instead of retained for reconciliation")
			}
		})
	}
}

func TestTeamAssistanceGenericResumeReconciliationDoesNotStarveIndependentWait(t *testing.T) {
	w, ctx, assisted, _, _ := prepareTeamAssistanceAnswer(t)
	if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET state='running',revision=revision+1 WHERE id=?`, assisted.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE task_attempts SET status='running' WHERE id=?`, assisted.AttemptID); err != nil {
		t.Fatal(err)
	}
	created, err := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: assisted.WorkspaceID, Objective: "Independent ordinary wait"})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: created.ID, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, result := w.startRun(ctx, ready)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if err = w.suspendForResource(ctx, ordinary, json.RawMessage(`{}`), "Independent ordinary resource wait", "route", map[string]any{"kind": "resource"}, nil); err != nil {
		t.Fatal(err)
	}
	before, err := w.getRun(ctx, assisted.ID)
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err = restarted.syncResumedRuns(ctx); !errors.Is(err, ErrInvalidWorkerState) {
		t.Fatalf("assisted denial not visible: %v", err)
	}
	after, err := w.getRun(ctx, assisted.ID)
	if err != nil || after.Status != RunWaiting || after.Revision != before.Revision {
		t.Fatalf("assisted owner changed: %+v %v", after, err)
	}
	independent, err := w.getRun(ctx, ordinary.ID)
	if err != nil || independent.Status != RunRunning {
		t.Fatalf("independent wait starved: %+v %v", independent, err)
	}
	var attempts int
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, ordinary.TaskID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("independent wait replaced original Attempt: %d %v", attempts, err)
	}
}

func TestTeamAssistanceGenericResumeReplacementAttemptIsNotHistoricalRequest(t *testing.T) {
	w, ctx, old, _, _ := prepareTeamAssistanceAnswer(t)
	// Synthetic retained-history setup: the original owner is terminal and a
	// new ordinary waiting owner is started by the real Worker/Task services.
	if _, err := w.db.ExecContext(ctx, `UPDATE task_attempts SET status='failed',ended_at=2 WHERE id=?`, old.AttemptID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET status='failed' WHERE id=?`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET state='ready',revision=revision+1 WHERE id=?`, old.TaskID); err != nil {
		t.Fatal(err)
	}
	ready, err := w.tasks.Get(ctx, old.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	current, result := w.startRun(ctx, ready)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if current.AttemptID == old.AttemptID {
		t.Fatal("fixture did not replace the old Attempt")
	}
	if result := applyWorkerWait(t, w, ctx, current, "wait"); result.Status == "failed" {
		t.Fatalf("ordinary replacement wait: %+v", result)
	}
	waiting, err := w.tasks.Get(ctx, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: waiting.ID, ExpectedRevision: waiting.Revision}); err != nil {
		t.Fatalf("historical request blocked different original Attempt: %v", err)
	}
	if err = w.syncResumedRuns(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := w.getRun(ctx, current.ID)
	if err != nil || restored.Status != RunRunning {
		t.Fatalf("replacement ordinary owner not restored: %+v %v", restored, err)
	}
	prior, err := w.getRun(ctx, old.ID)
	if err != nil || prior.Status != RunFailed {
		t.Fatalf("old request owner changed: %+v %v", prior, err)
	}
}

func TestTeamAssistanceGenericResumeProductionPlan(t *testing.T) {
	w, ctx, run, _, command := prepareTeamAssistanceAnswer(t)
	// Schema-valid unrelated history is solely a query-plan input. It does not
	// represent approved execution, a Continue receipt or historical authority.
	for _, query := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<1024) INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'resume-history-'||i,'tenant','History','waiting_approval','{}',i,i FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,metadata_json) SELECT id,id,1,'waiting','system:agent-worker','{"agent_worker":"v1"}' FROM tasks WHERE id LIKE 'resume-history-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT t.id,r.workspace_id,t.id,t.id,r.worker_principal_id,'waiting',r.role_name,r.capability_id,r.protocol_level,r.max_steps,r.max_replans,r.max_escalations,r.route_policy_json,'{}',t.created_at,t.updated_at FROM tasks t JOIN agent_worker_runs r ON r.id=? WHERE t.id LIKE 'resume-history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id,id,1,'human','waiting','human','{}',started_at,updated_at FROM agent_worker_runs WHERE id LIKE 'resume-history-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT t.id,h.workspace_id,h.session_id,h.member_id,h.author_principal_id,'system','{}',1,t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'resume-history-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT t.id,h.workspace_id,h.session_id,h.plan_id,h.assignment_key,h.member_id,t.id,t.id,t.id,t.id,t.id,h.author_principal_id,'Historical question',t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'resume-history-%'`,
	} {
		var args []any
		if strings.Contains(query, "r.id=?") {
			args = []any{run.ID}
		} else if strings.Contains(query, "h.id=?") {
			args = []any{command.RequestID}
		}
		if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "task", "assistance_resume.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	query := ""
	ast.Inspect(syntax, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "currentTeamAssistanceWaitSQL" || len(spec.Values) != 1 {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatal("production assistance fence no longer a reviewed SQL literal")
		}
		query, err = strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		return false
	})
	if query == "" {
		t.Fatal("missing production assistance fence SQL")
	}
	for _, analyze := range []bool{false, true} {
		if analyze {
			if _, err = w.db.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		for _, probe := range []struct {
			taskID, attemptID string
			want              bool
		}{{run.TaskID, "", true}, {run.TaskID, run.AttemptID, true}, {"absent-resume-task", "", false}, {run.TaskID, "unrelated-attempt", false}} {
			args := []any{probe.taskID, probe.attemptID, probe.attemptID}
			var actual bool
			if err = w.db.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil || actual != probe.want {
				t.Fatalf("production fence after analyze=%v got=%v want=%v err=%v", analyze, actual, probe.want, err)
			}
			rows, err := w.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, args...)
			if err != nil {
				t.Fatal(err)
			}
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if err = rows.Close(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			if strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("ephemeral fence plan after analyze=%v: %s", analyze, plan)
			}
			for _, alias := range []string{"a", "r", "step", "latest", "h"} {
				found := false
				for _, detail := range details {
					if strings.HasPrefix(detail, "SCAN "+alias+" ") {
						t.Fatalf("history scan after analyze=%v: %s", analyze, plan)
					}
					if strings.HasPrefix(detail, "SEARCH "+alias+" USING ") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing persistent owner lookup %s after analyze=%v: %s", alias, analyze, plan)
				}
			}
		}
	}
}
