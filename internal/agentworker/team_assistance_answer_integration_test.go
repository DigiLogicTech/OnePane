//go:build integration

package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

// Actual human wait handler -> retained request -> domain answer. No HTTP
// credential, model transport, tool, physical inference or Continue is exercised.
func prepareTeamAssistanceAnswer(t *testing.T, configure ...func(*Service, context.Context, Run)) (*Service, context.Context, Run, *team.Service, team.AnswerAssistanceCommand) {
	t.Helper()
	w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
	for _, setup := range configure {
		setup(w, ctx, run)
	}
	if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
		t.Fatalf("wait %+v", result)
	}
	var request string
	if err := w.db.QueryRowContext(ctx, `SELECT id FROM team_assistance_requests WHERE run_id=?`, run.ID).Scan(&request); err != nil {
		t.Fatal(err)
	}
	observed, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human")
	if err != nil {
		t.Fatal(err)
	}
	return w, ctx, run, teams, team.AnswerAssistanceCommand{
		WorkspaceID: run.WorkspaceID, SessionID: session, RequestID: request,
		TaskID: run.TaskID, AttemptID: run.AttemptID, RunID: run.ID, WaitStepID: observed.WaitStepID,
		ActorPrincipalID: "proposal-human", IdempotencyKey: "review-submission", Text: "Use the bounded reviewed approach <authored input>.",
		ExpectedTaskRevision: observed.TaskRevision, ExpectedWorkerRevision: observed.WorkerRevision,
	}
}

// Compare every stored field, not just status, across all execution journals,
// holds and accounting rows. Answer writes are not permission to change them.
func assistanceAnswerExecutionSnapshot(t *testing.T, w *Service, ctx context.Context) string {
	t.Helper()
	tables := []string{"tasks", "task_attempts", "agent_worker_runs", "agent_worker_steps", "team_execution_holds", "team_execution_held_tasks", "team_messages", "team_assistance_requests", "team_assistance_request_scopes", "budget_reservations"}
	all := make(map[string][]any)
	for _, table := range tables {
		rows, err := w.db.QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			scan := make([]any, len(columns))
			for i := range scan {
				scan[i] = &values[i]
			}
			if err := rows.Scan(scan...); err != nil {
				t.Fatal(err)
			}
			all[table] = append(all[table], values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assistanceAnswerCounts(t *testing.T, w *Service, ctx context.Context, request string) (int, int) {
	t.Helper()
	var answers, audits int
	if err := w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_assistance_answers WHERE request_id=?),
 (SELECT COUNT(*) FROM events WHERE event_type='team.assistance_answered' AND json_extract(payload_json,'$.request_id')=?)`, request, request).Scan(&answers, &audits); err != nil {
		t.Fatal(err)
	}
	return answers, audits
}

func TestTeamAssistanceAnswerActualWaitAndReconstructedRetry(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("held_%v", held), func(t *testing.T) {
			w, ctx, run, teams, command := prepareTeamAssistanceAnswer(t)
			if held {
				request, err := teams.AssistanceRequest(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID)
				if err != nil {
					t.Fatal(err)
				}
				root, err := w.tasks.Get(ctx, request.ParentTaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: command.SessionID, PlanID: request.PlanID, ActorPrincipalID: command.ActorPrincipalID, ExpectedTaskRevision: root.Revision, Reason: "answer while held"}); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceAnswerExecutionSnapshot(t, w, ctx)
			answer, err := teams.AnswerAssistance(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			if answer.AnswerID == "" || answer.RequestID != command.RequestID || answer.AuthorPrincipalID != command.ActorPrincipalID || answer.Text != command.Text || answer.TaskRevision != command.ExpectedTaskRevision || answer.WorkerRevision != command.ExpectedWorkerRevision || answer.CreatedAt <= 0 {
				t.Fatalf("wrong authored answer %+v", answer)
			}
			restarted := team.NewService(w.db, w.tx, w.clock, w.tasks)
			retry, err := restarted.AnswerAssistance(ctx, command)
			if err != nil || !reflect.DeepEqual(answer, retry) {
				t.Fatalf("reconstructed retry %+v %v", retry, err)
			}
			if after := assistanceAnswerExecutionSnapshot(t, w, ctx); after != before {
				t.Fatal("answer or retry changed original execution, hold, accounting or room history")
			}
			if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != 1 || audits != 1 {
				t.Fatalf("answer/audit counts %d/%d", answers, audits)
			}
			var auditActor, auditRun, auditAnswer, auditRaw string
			if err := w.db.QueryRowContext(ctx, `SELECT actor_principal_id,json_extract(payload_json,'$.run_id'),json_extract(payload_json,'$.answer_id'),payload_json FROM events WHERE event_type='team.assistance_answered'`).Scan(&auditActor, &auditRun, &auditAnswer, &auditRaw); err != nil {
				t.Fatal(err)
			}
			if auditActor != command.ActorPrincipalID || auditRun != run.ID || auditAnswer != answer.AnswerID || strings.Contains(auditRaw, command.Text) {
				t.Fatal("audit lost human/owner provenance or copied authored answer text")
			}
			for _, query := range []string{`UPDATE team_assistance_answers SET answer='replacement' WHERE request_id=?`, `DELETE FROM team_assistance_answers WHERE request_id=?`, `INSERT INTO team_assistance_answers SELECT request_id,id||'duplicate',author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at FROM team_assistance_answers WHERE request_id=?`} {
				if _, err := w.db.ExecContext(ctx, query, command.RequestID); err == nil {
					t.Fatal("final answer mutation/duplicate accepted")
				}
			}
		})
	}
}

func TestTeamAssistanceAnswerDeniesChangedAuthority(t *testing.T) {
	for _, change := range []string{"foreign_tenant", "foreign_session", "foreign_request", "wrong_task", "wrong_attempt", "wrong_run", "wrong_step", "stale_task_revision", "stale_worker_revision", "revoked_human", "nonhuman", "human_membership", "tenant_suspended", "worker_revoked", "worker_membership", "root_seal", "child_seal", "plan_body", "accepted_plan", "member_disabled", "attempt_terminal", "worker_terminal", "original_scope", "oversize_answer", "empty_answer", "invalid_utf8_answer", "oversize_key", "empty_key", "invalid_utf8_key", "unsafe_revision"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, command := prepareTeamAssistanceAnswer(t)
			query := ""
			switch change {
			case "foreign_tenant":
				command.WorkspaceID = "foreign"
			case "foreign_session":
				command.SessionID = "foreign"
			case "foreign_request":
				command.RequestID = "foreign"
			case "wrong_task":
				command.TaskID = "foreign"
			case "wrong_attempt":
				command.AttemptID = "foreign"
			case "wrong_run":
				command.RunID = "foreign"
			case "wrong_step":
				command.WaitStepID = "foreign"
			case "stale_task_revision":
				command.ExpectedTaskRevision--
			case "stale_worker_revision":
				command.ExpectedWorkerRevision--
			case "revoked_human":
				query = `UPDATE principals SET status='disabled' WHERE id='proposal-human'`
			case "nonhuman":
				query = `UPDATE principals SET principal_type='system' WHERE id='proposal-human'`
			case "human_membership":
				query = `UPDATE workspace_memberships SET status='suspended' WHERE principal_id='proposal-human'`
			case "tenant_suspended":
				query = `UPDATE workspaces SET status='suspended' WHERE id='tenant'`
			case "worker_revoked":
				query = `UPDATE principals SET status='disabled' WHERE id='system:agent-worker'`
			case "worker_membership":
				query = `UPDATE workspace_memberships SET status='suspended' WHERE principal_id='system:agent-worker'`
			case "root_seal":
				query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=(SELECT parent_task_id FROM tasks WHERE id=?)`
			case "child_seal":
				query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=?`
			case "plan_body":
				query = `UPDATE team_plans SET plan_json='{}'`
			case "accepted_plan":
				query = `UPDATE team_sessions SET accepted_plan_id=NULL`
			case "member_disabled":
				query = `UPDATE team_members SET status='disabled' WHERE id=(SELECT member_id FROM team_execution_assignments WHERE task_id=?)`
			case "attempt_terminal":
				query = `UPDATE task_attempts SET status='failed',ended_at=3 WHERE task_id=?`
			case "worker_terminal":
				query = `UPDATE agent_worker_runs SET status='failed' WHERE task_id=?`
			case "original_scope":
				query = `UPDATE tasks SET artifact_session_id='replacement' WHERE id=? OR id=(SELECT parent_task_id FROM tasks WHERE id=?)`
				if _, err := w.db.ExecContext(ctx, `INSERT INTO artifact_sessions(id,workspace_id,format,status,title,created_by,created_at,updated_at) VALUES('replacement','tenant','text','active','Replacement','proposal-human',1,1)`); err != nil {
					t.Fatal(err)
				}
			case "oversize_answer":
				command.Text = strings.Repeat("界", 1366)
			case "empty_answer":
				command.Text = " \n\t"
			case "invalid_utf8_answer":
				command.Text = string([]byte{0xff})
			case "oversize_key":
				command.IdempotencyKey = strings.Repeat("界", 43)
			case "empty_key":
				command.IdempotencyKey = " \n"
			case "invalid_utf8_key":
				command.IdempotencyKey = string([]byte{0xff})
			case "unsafe_revision":
				command.ExpectedWorkerRevision = 1 << 53
			}
			if query != "" {
				var args []any
				for range strings.Count(query, "?") {
					args = append(args, run.TaskID)
				}
				if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceAnswerExecutionSnapshot(t, w, ctx)
			if out, err := teams.AnswerAssistance(ctx, command); err == nil || out.AnswerID != "" {
				t.Fatalf("changed authority succeeded: %+v %v", out, err)
			}
			if after := assistanceAnswerExecutionSnapshot(t, w, ctx); after != before {
				t.Fatal("denied answer changed execution")
			}
			if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != 0 || audits != 0 {
				t.Fatalf("denied answer left %d answers/%d audits", answers, audits)
			}
		})
	}
}

func TestTeamAssistanceAnswerConflictingRetryDenied(t *testing.T) {
	for _, change := range []string{"body", "key", "actor", "task_revision", "worker_revision"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
			want, err := teams.AnswerAssistance(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "body":
				command.Text = "Different final answer"
			case "key":
				command.IdempotencyKey = "new-submission"
			case "actor":
				if _, err := w.db.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('other-human','human','Other','active',1,1); INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','other-human','active',1,1)`); err != nil {
					t.Fatal(err)
				}
				command.ActorPrincipalID = "other-human"
			case "task_revision":
				command.ExpectedTaskRevision++
			case "worker_revision":
				command.ExpectedWorkerRevision++
			}
			if _, err := teams.AnswerAssistance(ctx, command); err == nil {
				t.Fatal("conflicting final answer accepted")
			}
			var text, id string
			if err := w.db.QueryRowContext(ctx, `SELECT id,answer FROM team_assistance_answers WHERE request_id=?`, command.RequestID).Scan(&id, &text); err != nil {
				t.Fatal(err)
			}
			if id != want.AnswerID || text != want.Text {
				t.Fatal("conflicting retry rewrote final answer")
			}
			if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != 1 || audits != 1 {
				t.Fatalf("conflicting retry left %d/%d", answers, audits)
			}
		})
	}
}

func TestTeamAssistanceAnswerByteBoundariesAndStoredIdentity(t *testing.T) {
	w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
	command.Text = strings.Repeat("界", 1365) + "a"
	command.IdempotencyKey = strings.Repeat("界", 42) + "ab"
	want, err := teams.AnswerAssistance(ctx, command)
	if err != nil || len(want.Text) != 4096 || len(want.IdempotencyKey) != 128 {
		t.Fatalf("UTF8 exact byte budget rejected: %+v %v", want, err)
	}
	if retry, err := teams.AnswerAssistance(ctx, command); err != nil || retry != want {
		t.Fatalf("bounded final answer retry changed: %+v %v", retry, err)
	}
	// Deliberately corrupt a test-owned retained row to exercise read budgets;
	// production never rewrites immutable answers or trusts malformed identity.
	if _, err := w.db.ExecContext(ctx, `DROP TRIGGER team_assistance_answer_immutable`); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{strings.Repeat("x", 257), string([]byte{0xff})} {
		if _, err := w.db.ExecContext(ctx, `UPDATE team_assistance_answers SET id=? WHERE request_id=?`, identity, command.RequestID); err != nil {
			t.Fatal(err)
		}
		if out, err := teams.AnswerAssistance(ctx, command); err == nil || out.AnswerID != "" {
			t.Fatalf("malformed stored answer returned: %+v %v", out, err)
		}
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE team_assistance_answers SET id=?,created_at=? WHERE request_id=?`, want.AnswerID, int64(1<<53), command.RequestID); err != nil {
		t.Fatal(err)
	}
	if out, err := teams.AnswerAssistance(ctx, command); err == nil || out.AnswerID != "" {
		t.Fatalf("unsafe stored answer timestamp returned: %+v %v", out, err)
	}
	if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != 1 || audits != 1 {
		t.Fatalf("corrupt retry altered final answer/audit count: %d/%d", answers, audits)
	}
}

func TestTeamAssistanceAnswerRequiresTrustedTransactionAuthorization(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprintf("replay_%v", replay), func(t *testing.T) {
			w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
			if replay {
				if _, err := teams.AnswerAssistance(ctx, command); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceAnswerExecutionSnapshot(t, w, ctx)
			denied := errors.New("trusted credential revoked")
			calls := 0
			command.AuthorizeInTransaction = func(ctx context.Context, tx storage.Tx, request team.AssistanceRequest) error {
				calls++
				if request.RequestID != command.RequestID || request.TaskID != command.TaskID || request.TaskRevision != command.ExpectedTaskRevision || request.WorkerRevision != command.ExpectedWorkerRevision {
					t.Fatal("trusted authorization did not receive the current exact request")
				}
				var revision int64
				if err := tx.QueryRowContext(ctx, `SELECT revision FROM tasks WHERE id=?`, command.TaskID).Scan(&revision); err != nil || revision != request.TaskRevision {
					t.Fatalf("authorization lacks live transaction: %d %v", revision, err)
				}
				return denied
			}
			if out, err := teams.AnswerAssistance(ctx, command); !errors.Is(err, denied) || out.AnswerID != "" || calls != 1 {
				t.Fatalf("trusted denial did not propagate: %+v %v calls=%d", out, err, calls)
			}
			if after := assistanceAnswerExecutionSnapshot(t, w, ctx); after != before {
				t.Fatal("trusted denial changed execution")
			}
			expected := 0
			if replay {
				expected = 1
			}
			if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != expected || audits != expected {
				t.Fatalf("trusted denial changed answer/audit history: %d/%d expected%d", answers, audits, expected)
			}
		})
	}
}

func TestTeamAssistanceAnswerProductionLookupPlan(t *testing.T) {
	w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
	want, err := teams.AnswerAssistance(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	// These schema-valid history rows are deliberately synthetic lookup inputs,
	// not accepted execution graphs. No historical request is treated as a valid
	// contribution or used to authorize a write. Actual current-owner admission
	// and answer behavior are exercised by the handler tests above.
	for _, query := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<1024) INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'answer-history-'||i,'tenant','history','created','{}',i,i FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,metadata_json) SELECT id,id,1,'waiting','system:agent-worker','{}' FROM tasks WHERE id LIKE 'answer-history-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT t.id,r.workspace_id,t.id,t.id,r.worker_principal_id,'waiting',r.role_name,r.capability_id,r.protocol_level,r.max_steps,r.max_replans,r.max_escalations,r.route_policy_json,'{}',t.created_at,t.updated_at FROM tasks t JOIN agent_worker_runs r ON r.id=? WHERE t.id LIKE 'answer-history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id,id,1,'human','waiting','human','{}',started_at,updated_at FROM agent_worker_runs WHERE id LIKE 'answer-history-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT t.id,h.workspace_id,h.session_id,h.member_id,h.author_principal_id,'system','{}',1,t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'answer-history-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT t.id,h.workspace_id,h.session_id,h.plan_id,h.assignment_key,h.member_id,t.id,t.id,t.id,t.id,t.id,h.author_principal_id,'history question',t.created_at FROM tasks t JOIN team_assistance_requests h ON h.id=? WHERE t.id LIKE 'answer-history-%'`,
		`INSERT INTO team_assistance_answers(request_id,id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at) SELECT id,id,'proposal-human','history','historical authored input',1,1,created_at FROM team_assistance_requests WHERE id LIKE 'answer-history-%'`,
	} {
		var args []any
		if strings.Contains(query, "r.id=?") {
			args = []any{command.RunID}
		} else if strings.Contains(query, "h.id=?") {
			args = []any{command.RequestID}
		}
		if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Extract the literal from the production source instead of testing a copied
	// query that could silently diverge from the actual domain answer lookup.
	syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "team", "assistance_answers.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	query := ""
	ast.Inspect(syntax, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "assistanceAnswerReadSQL" || len(spec.Values) != 1 {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatal("production answer query is no longer a reviewed literal")
		}
		query, err = strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		return false
	})
	if query == "" {
		t.Fatal("missing production answer lookup")
	}
	for _, analyze := range []bool{false, true} {
		if analyze {
			if _, err := w.db.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		for _, key := range []string{command.RequestID, "absent-answer-request"} {
			rows, err := w.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, key)
			if err != nil {
				t.Fatal(err)
			}
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			if !strings.Contains(plan, "SEARCH team_assistance_answers USING INDEX ") || !strings.Contains(plan, "(request_id=?)") || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("unbounded production answer lookup after analyze=%v: %s", analyze, plan)
			}
			var actual team.AssistanceAnswer
			err = w.db.QueryRowContext(ctx, query, key).Scan(&actual.AnswerID, &actual.RequestID, &actual.AuthorPrincipalID, &actual.IdempotencyKey, &actual.Text, &actual.TaskRevision, &actual.WorkerRevision, &actual.CreatedAt)
			if key == command.RequestID {
				if err != nil || actual != want {
					t.Fatalf("wrong exact production answer after analyze=%v: %+v %v", analyze, actual, err)
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("absent answer leaked history: %+v %v", actual, err)
			}
		}
	}
}

type assistanceAnswerCancelBeforeCommit struct {
	storage.Transactor
	cancel context.CancelFunc
}

func (c assistanceAnswerCancelBeforeCommit) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	return c.Transactor.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		c.cancel()
		return ctx.Err()
	})
}

func TestTeamAssistanceAnswerRequiresCommittedAudit(t *testing.T) {
	for _, failure := range []string{"answer", "audit", "cancel_before", "cancel_after_prepared"} {
		t.Run(failure, func(t *testing.T) {
			w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
			before := assistanceAnswerExecutionSnapshot(t, w, ctx)
			callCtx := ctx
			if failure == "answer" || failure == "audit" {
				query := `CREATE TRIGGER reject_help_answer BEFORE INSERT ON team_assistance_answers BEGIN SELECT RAISE(ABORT,'answer rejected'); END`
				if failure == "audit" {
					query = `CREATE TRIGGER reject_help_answer BEFORE INSERT ON events WHEN NEW.event_type='team.assistance_answered' BEGIN SELECT RAISE(ABORT,'answer audit rejected'); END`
				}
				if _, err := w.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			} else {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				if failure == "cancel_before" {
					cancel()
				} else {
					teams = team.NewService(w.db, assistanceAnswerCancelBeforeCommit{Transactor: w.tx, cancel: cancel}, w.clock, w.tasks)
				}
			}
			out, err := teams.AnswerAssistance(callCtx, command)
			if err == nil || out.AnswerID != "" {
				t.Fatalf("uncommitted answer reported success %+v %v", out, err)
			}
			if strings.HasPrefix(failure, "cancel_") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation concealed: %v", err)
			}
			if after := assistanceAnswerExecutionSnapshot(t, w, ctx); after != before {
				t.Fatal("failed answer changed original execution")
			}
			if answers, audits := assistanceAnswerCounts(t, w, ctx, command.RequestID); answers != 0 || audits != 0 {
				t.Fatalf("failed answer left %d/%d", answers, audits)
			}
		})
	}
}
