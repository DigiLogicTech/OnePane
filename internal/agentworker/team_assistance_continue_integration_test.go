//go:build integration

package agentworker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

// Actual Worker human handler -> immutable request/answer -> explicit original
// owner continuation. Credential transport and physical AI are separate tests.
func prepareTeamAssistanceContinue(t *testing.T, configure ...func(*Service, context.Context, Run)) (*Service, context.Context, Run, *team.Service, team.ContinueAssistanceCommand) {
	t.Helper()
	w, ctx, run, teams, c := prepareTeamAssistanceAnswer(t, configure...)
	answer, err := teams.AnswerAssistance(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	teams.SetAssistanceContinuationWorker(w)
	return w, ctx, run, teams, team.ContinueAssistanceCommand{
		WorkspaceID: c.WorkspaceID, SessionID: c.SessionID, RequestID: c.RequestID, AnswerID: answer.AnswerID,
		TaskID: c.TaskID, AttemptID: c.AttemptID, RunID: c.RunID, WaitStepID: c.WaitStepID, ActorPrincipalID: c.ActorPrincipalID,
		ExpectedTaskRevision: c.ExpectedTaskRevision, ExpectedWorkerRevision: c.ExpectedWorkerRevision,
		AuthorizeInTransaction: func(ctx context.Context, tx storage.Tx, r team.AssistanceRequest) error {
			// This trusted-domain test uses a real transaction read as an explicit
			// boundary. Production HTTP verifies original human credentials here.
			var id string
			return tx.QueryRowContext(ctx, `SELECT id FROM principals WHERE id='proposal-human' AND status='active' AND principal_type='human'`).Scan(&id)
		},
	}
}

func assistanceContinueSnapshot(t *testing.T, w *Service, ctx context.Context) string {
	t.Helper()
	var receipt string
	if err := w.db.QueryRowContext(ctx, `SELECT COALESCE(group_concat(quote(request_id)||quote(id)||quote(answer_id)||quote(actor_principal_id)||quote(continue_step_id)||quote(task_revision)||quote(worker_revision)||quote(created_at)),'') FROM team_assistance_continuations`).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	return receipt + "/" + assistanceAnswerReadSnapshot(t, w, ctx)
}

func TestTeamAssistanceContinueOriginalOwnerAndDuplicateRefusal(t *testing.T) {
	w, ctx, run, teams, c := prepareTeamAssistanceContinue(t)
	var attempts int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, c.TaskID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	out, err := teams.ContinueAssistance(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if out.ContinuationID == "" || out.RequestID != c.RequestID || out.AnswerID != c.AnswerID || out.ActorPrincipalID != c.ActorPrincipalID || out.ContinueStepID == "" || out.TaskRevision != c.ExpectedTaskRevision || out.WorkerRevision != c.ExpectedWorkerRevision || out.CreatedAt < 1 {
		t.Fatalf("wrong receipt %+v", out)
	}
	current, err := w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := w.tasks.Get(ctx, c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var afterAttempts int
	if err = w.db.QueryRowContext(ctx, `SELECT status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=?) FROM task_attempts WHERE id=? AND task_id=?`, c.TaskID, c.AttemptID, c.TaskID).Scan(&status, &afterAttempts); err != nil {
		t.Fatal(err)
	}
	if current.Status != RunRunning || current.AttemptID != run.AttemptID || current.Revision != c.ExpectedWorkerRevision+1 || owner.State != task.StateRunning || owner.Revision != c.ExpectedTaskRevision+1 || status != "running" || afterAttempts != attempts {
		t.Fatal("continuation replaced or failed to resume original owner")
	}
	var count int
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE id=? AND run_id=? AND step_kind='human' AND status='succeeded' AND proposal_type='human' AND json_extract(detail_json,'$.action')='assistance_continued' AND json_extract(detail_json,'$.request_id')=? AND json_extract(detail_json,'$.answer_id')=? AND json_extract(detail_json,'$.wait_step_id')=? AND json_extract(detail_json,'$.actor_principal_id')=?`, out.ContinueStepID, c.RunID, c.RequestID, c.AnswerID, c.WaitStepID, c.ActorPrincipalID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing exact journal %d %v", count, err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_type='team.assistance_continued' AND actor_principal_id=? AND json_extract(payload_json,'$.continuation_id')=?`, c.ActorPrincipalID, out.ContinuationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing human audit %d %v", count, err)
	}
	before := assistanceContinueSnapshot(t, w, ctx)
	restarted := team.NewService(w.db, w.tx, w.clock, w.tasks)
	restarted.SetAssistanceContinuationWorker(w)
	if duplicate, err := restarted.ContinueAssistance(ctx, c); err == nil || duplicate.ContinuationID != "" {
		t.Fatal("duplicate approval reexecuted")
	}
	if after := assistanceContinueSnapshot(t, w, ctx); after != before {
		t.Fatal("duplicate approval changed durable state")
	}
}

func TestTeamAssistanceContinueDeniesChangedAuthority(t *testing.T) {
	for _, change := range []string{"foreign_workspace", "foreign_session", "foreign_request", "foreign_answer", "foreign_task", "foreign_attempt", "foreign_run", "foreign_step", "stale_task", "stale_worker", "unsafe_revision", "missing_authorizer", "denied_authorizer", "human_disabled", "nonhuman", "membership", "worker_disabled", "worker_membership", "child_seal", "root_seal", "accepted_plan", "member_disabled", "manual_pause", "root_manual_pause", "recovery", "team_hold", "autonomy_revoked", "original_scope", "missing_answer"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, c := prepareTeamAssistanceContinue(t, func(w *Service, ctx context.Context, run Run) {
				if change != "autonomy_revoked" {
					return
				}
				// Capture the original scope before the actual human wait. Later
				// policy revocation must fail admission without scope drift.
				if _, err := w.db.ExecContext(ctx, `INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('policy-project','tenant','Policy','active','{}','{}','proposal-human',1,1)`); err != nil {
					t.Fatal(err)
				}
				if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET project_id='policy-project' WHERE id=? OR id=(SELECT parent_task_id FROM tasks WHERE id=?)`, run.TaskID, run.TaskID); err != nil {
					t.Fatal(err)
				}
			})
			var query string
			switch change {
			case "foreign_workspace":
				c.WorkspaceID = "foreign"
			case "foreign_session":
				c.SessionID = "foreign"
			case "foreign_request":
				c.RequestID = "foreign"
			case "foreign_answer":
				c.AnswerID = "foreign"
			case "foreign_task":
				c.TaskID = "foreign"
			case "foreign_attempt":
				c.AttemptID = "foreign"
			case "foreign_run":
				c.RunID = "foreign"
			case "foreign_step":
				c.WaitStepID = "foreign"
			case "stale_task":
				c.ExpectedTaskRevision++
			case "stale_worker":
				c.ExpectedWorkerRevision++
			case "unsafe_revision":
				c.ExpectedWorkerRevision = 1 << 53
			case "missing_authorizer":
				c.AuthorizeInTransaction = nil
			case "denied_authorizer":
				c.AuthorizeInTransaction = func(context.Context, storage.Tx, team.AssistanceRequest) error {
					return errors.New("credential revoked")
				}
			case "human_disabled":
				query = `UPDATE principals SET status='disabled' WHERE id='proposal-human'`
			case "nonhuman":
				query = `UPDATE principals SET principal_type='agent' WHERE id='proposal-human'`
			case "membership":
				query = `UPDATE workspace_memberships SET status='suspended' WHERE principal_id='proposal-human'`
			case "worker_disabled":
				query = `UPDATE principals SET status='disabled' WHERE id='system:agent-worker'`
			case "worker_membership":
				query = `UPDATE workspace_memberships SET status='suspended' WHERE principal_id='system:agent-worker'`
			case "child_seal":
				query = `UPDATE tasks SET completion_json='{}' WHERE id=?`
			case "root_seal":
				query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=(SELECT parent_task_id FROM tasks WHERE id=?)`
			case "accepted_plan":
				query = `UPDATE team_sessions SET accepted_plan_id=NULL`
			case "member_disabled":
				query = `UPDATE team_members SET status='disabled' WHERE id=(SELECT member_id FROM team_execution_assignments WHERE task_id=?)`
			case "manual_pause":
				query = `UPDATE tasks SET state='paused' WHERE id=?`
			case "root_manual_pause":
				query = `UPDATE tasks SET state='paused' WHERE id=(SELECT parent_task_id FROM tasks WHERE id=?)`
			case "recovery":
				query = `INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('uncertain','tenant','task.attempt_interrupted','task',?,'{}',1)`
			case "team_hold":
				request, err := teams.AssistanceRequest(ctx, c.WorkspaceID, c.SessionID, c.RequestID, c.ActorPrincipalID)
				if err != nil {
					t.Fatal(err)
				}
				root, err := w.tasks.Get(ctx, request.ParentTaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: c.SessionID, PlanID: request.PlanID, ActorPrincipalID: c.ActorPrincipalID, ExpectedTaskRevision: root.Revision, Reason: "hold must persist"}); err != nil {
					t.Fatal(err)
				}
			case "autonomy_revoked":
				query = `UPDATE projects SET project_policy_json='{"feature_policy":{"AutonomousRoutines":"disabled"}}' WHERE id='policy-project'`
			case "original_scope":
				if _, err := w.db.ExecContext(ctx, `INSERT INTO artifact_sessions(id,workspace_id,format,status,title,created_by,created_at,updated_at) VALUES('replacement','tenant','text','active','Replacement','proposal-human',1,1)`); err != nil {
					t.Fatal(err)
				}
				query = `UPDATE tasks SET artifact_session_id='replacement' WHERE id=? OR id=(SELECT parent_task_id FROM tasks WHERE id=?)`
			case "missing_answer":
				// Simulate retained-store damage explicitly; production retention
				// prevents deletion and is tested in the migration suite.
				query = `DROP TRIGGER team_assistance_answer_retained; DELETE FROM team_assistance_answers`
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
			before := assistanceContinueSnapshot(t, w, ctx)
			if out, err := teams.ContinueAssistance(ctx, c); err == nil || out.ContinuationID != "" {
				t.Fatalf("changed authority continued: %+v %v", out, err)
			}
			if after := assistanceContinueSnapshot(t, w, ctx); after != before {
				t.Fatal("denied continuation changed owner/history/holds/accounting")
			}
		})
	}
}

func TestTeamAssistanceContinueRollsBackEveryWrite(t *testing.T) {
	for _, failure := range []string{"task", "attempt", "worker", "step", "task_audit", "worker_audit", "receipt", "team_audit", "cancel_after_prepared"} {
		t.Run(failure, func(t *testing.T) {
			w, ctx, _, teams, c := prepareTeamAssistanceContinue(t)
			callCtx := ctx
			query := map[string]string{
				"task":         `CREATE TRIGGER reject_continue BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'Task rejected'); END`,
				"attempt":      `CREATE TRIGGER reject_continue BEFORE UPDATE ON task_attempts BEGIN SELECT RAISE(ABORT,'Attempt rejected'); END`,
				"worker":       `CREATE TRIGGER reject_continue BEFORE UPDATE ON agent_worker_runs BEGIN SELECT RAISE(ABORT,'Worker rejected'); END`,
				"step":         `CREATE TRIGGER reject_continue BEFORE INSERT ON agent_worker_steps BEGIN SELECT RAISE(ABORT,'step rejected'); END`,
				"task_audit":   `CREATE TRIGGER reject_continue BEFORE INSERT ON events WHEN NEW.event_type='task.resumed' BEGIN SELECT RAISE(ABORT,'Task audit rejected'); END`,
				"worker_audit": `CREATE TRIGGER reject_continue BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' BEGIN SELECT RAISE(ABORT,'Worker audit rejected'); END`,
				"receipt":      `CREATE TRIGGER reject_continue BEFORE INSERT ON team_assistance_continuations BEGIN SELECT RAISE(ABORT,'receipt rejected'); END`,
				"team_audit":   `CREATE TRIGGER reject_continue BEFORE INSERT ON events WHEN NEW.event_type='team.assistance_continued' BEGIN SELECT RAISE(ABORT,'Team audit rejected'); END`,
			}[failure]
			if query != "" {
				if _, err := w.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			} else {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				teams = team.NewService(w.db, assistanceAnswerCancelBeforeCommit{Transactor: w.tx, cancel: cancel}, w.clock, w.tasks)
				teams.SetAssistanceContinuationWorker(w)
			}
			before := assistanceContinueSnapshot(t, w, ctx)
			if out, err := teams.ContinueAssistance(callCtx, c); err == nil || out.ContinuationID != "" {
				t.Fatalf("uncommitted continuation reported success %+v %v", out, err)
			} else if failure == "cancel_after_prepared" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if after := assistanceContinueSnapshot(t, w, ctx); after != before {
				t.Fatal("failed continuation left partial writes")
			}
		})
	}
}

func TestTeamAssistanceContinueDeniesMalformedRetainedAnswer(t *testing.T) {
	for _, change := range []string{"key_blank", "answer_blank", "answer_utf8", "identity_oversized", "timestamp_unsafe"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, _, teams, c := prepareTeamAssistanceContinue(t)
			if _, err := w.db.ExecContext(ctx, `DROP TRIGGER team_assistance_answer_immutable`); err != nil {
				t.Fatal(err)
			}
			column, value := "idempotency_key", any(" \n")
			switch change {
			case "answer_blank":
				column = "answer"
			case "answer_utf8":
				column, value = "answer", string([]byte{0xff})
			case "identity_oversized":
				column, value = "id", strings.Repeat("界", 86)
			case "timestamp_unsafe":
				column, value = "created_at", int64(1<<53)
			}
			if _, err := w.db.ExecContext(ctx, `UPDATE team_assistance_answers SET `+column+`=? WHERE request_id=?`, value, c.RequestID); err != nil {
				t.Fatal(err)
			}
			before := assistanceContinueSnapshot(t, w, ctx)
			if out, err := teams.ContinueAssistance(ctx, c); !errors.Is(err, team.ErrInvalid) || out != (team.AssistanceContinuation{}) {
				t.Fatalf("malformed answer resumed execution: %+v %v", out, err)
			}
			if after := assistanceContinueSnapshot(t, w, ctx); after != before {
				t.Fatal("malformed input changed execution or retained history")
			}
		})
	}
}
