//go:build integration

package agentworker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

func assistanceAnswerReadSnapshot(t *testing.T, w *Service, ctx context.Context) string {
	t.Helper()
	var events int
	var answers string
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRowContext(ctx, `SELECT COALESCE(group_concat(quote(request_id)||quote(id)||quote(author_principal_id)||quote(idempotency_key)||quote(answer)||quote(task_revision)||quote(worker_revision)||quote(created_at)),'') FROM team_assistance_answers`).Scan(&answers); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(events, "/", answers, "/", assistanceAnswerExecutionSnapshot(t, w, ctx))
}

func TestTeamAssistanceAnswerReadActualWaitAndHeldOwner(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("held_%v", held), func(t *testing.T) {
			w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
			answer, err := teams.AnswerAssistance(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			if held {
				request, err := teams.AssistanceRequest(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID)
				if err != nil {
					t.Fatal(err)
				}
				root, err := w.tasks.Get(ctx, request.ParentTaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: command.SessionID, PlanID: request.PlanID, ActorPrincipalID: command.ActorPrincipalID, ExpectedTaskRevision: root.Revision, Reason: "observe retained reply while held"}); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			restarted := team.NewService(w.db, w.tx, w.clock, w.tasks)
			observed, err := restarted.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID)
			if err != nil || observed.Answer != answer || observed.Request.RequestID != command.RequestID || observed.Request.TaskID != command.TaskID || observed.Request.AttemptID != command.AttemptID || observed.Request.RunID != command.RunID || observed.Request.WaitStepID != command.WaitStepID || observed.Request.Question != "Review the recorded contribution?" {
				t.Fatalf("wrong retained observation: %+v %v", observed, err)
			}
			if observed.Answer.AuthorPrincipalID != command.ActorPrincipalID || observed.Answer.Text != command.Text || observed.Request.AuthorPrincipalID != WorkerPrincipal {
				t.Fatal("answer read combined model/question and authenticated human provenance")
			}
			if second, err := restarted.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID); err != nil || second != observed {
				t.Fatalf("exact answer observation changed across reads %+v %v", second, err)
			}
			// Reading authored provenance requires current membership, not a new
			// human-only role. The HTTP boundary separately checks read capability.
			if memberRead, err := restarted.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, WorkerPrincipal); err != nil || memberRead != observed {
				t.Fatalf("active nonhuman reader acquired an unrelated role requirement: %+v %v", memberRead, err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("answer observation changed durable execution/history/holds/accounting")
			}
		})
	}
}

func TestTeamAssistanceAnswerReadMissingAndUnavailable(t *testing.T) {
	w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
	before := assistanceAnswerReadSnapshot(t, w, ctx)
	if out, err := teams.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID); !errors.Is(err, sql.ErrNoRows) || out != (team.AssistanceAnswerObservation{}) {
		t.Fatalf("missing answer fabricated: %+v %v", out, err)
	}
	if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
		t.Fatal("absent answer read wrote durable state")
	}
	if _, err := teams.AnswerAssistance(ctx, command); err != nil {
		t.Fatal(err)
	}
	before = assistanceAnswerReadSnapshot(t, w, ctx)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if out, err := teams.AssistanceAnswer(canceled, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID); !errors.Is(err, context.Canceled) || out != (team.AssistanceAnswerObservation{}) {
		t.Fatalf("cancellation concealed or bytes returned: %+v %v", out, err)
	}
	if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
		t.Fatal("cancelled answer read wrote durable state")
	}
	if err := w.db.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := teams.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID); err == nil || out != (team.AssistanceAnswerObservation{}) {
		t.Fatalf("unavailable database returned authored bytes: %+v %v", out, err)
	}
}

func TestTeamAssistanceAnswerReadDeniesChangedOwnerAndReader(t *testing.T) {
	for _, change := range []string{"foreign_tenant", "foreign_session", "foreign_request", "foreign_reader", "reader_revoked", "reader_membership", "reader_id_oversized", "reader_id_utf8", "reader_id_empty", "task_terminal", "worker_terminal", "replaced_attempt", "root_seal", "child_seal", "accepted_plan", "worker_revoked", "step_newer"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, command := prepareTeamAssistanceAnswer(t)
			if _, err := teams.AnswerAssistance(ctx, command); err != nil {
				t.Fatal(err)
			}
			reader := command.ActorPrincipalID
			query := ""
			switch change {
			case "foreign_tenant":
				command.WorkspaceID = "foreign"
			case "foreign_session":
				command.SessionID = "foreign"
			case "foreign_request":
				command.RequestID = "foreign"
			case "foreign_reader":
				reader = "foreign"
			case "reader_revoked":
				query = `UPDATE principals SET status='disabled' WHERE id='proposal-human'`
			case "reader_membership":
				query = `DELETE FROM workspace_memberships WHERE principal_id='proposal-human'`
			case "reader_id_oversized":
				reader = strings.Repeat("界", 86)
			case "reader_id_utf8":
				reader = string([]byte{0xff})
			case "reader_id_empty":
				reader = ""
			case "task_terminal":
				query = `UPDATE tasks SET state='failed' WHERE id=?`
			case "worker_terminal":
				query = `UPDATE agent_worker_runs SET status='failed' WHERE task_id=?`
			case "replaced_attempt":
				if _, err := w.db.ExecContext(ctx, `UPDATE task_attempts SET status='failed',ended_at=3 WHERE id=?`, command.AttemptID); err != nil {
					t.Fatal(err)
				}
				query = `INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,metadata_json) VALUES('replacement',?,2,'waiting','system:agent-worker','{"agent_worker":"v1"}')`
			case "root_seal":
				query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=(SELECT parent_task_id FROM tasks WHERE id=?)`
			case "child_seal":
				query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=?`
			case "accepted_plan":
				query = `UPDATE team_sessions SET accepted_plan_id=NULL`
			case "worker_revoked":
				query = `UPDATE principals SET status='disabled' WHERE id='system:agent-worker'`
			case "step_newer":
				query = `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT 'newer-answer-step',id,99,'wait','waiting','wait','{}',1,2 FROM agent_worker_runs WHERE task_id=?`
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
			if out, err := teams.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, reader); err == nil || out != (team.AssistanceAnswerObservation{}) {
				t.Fatalf("changed owner/reader exposed answer %+v %v", out, err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("denied answer read changed durable state")
			}
		})
	}
}

func TestTeamAssistanceAnswerReadDeniesMalformedStoredAnswer(t *testing.T) {
	for _, change := range []string{"identity_oversized", "identity_utf8", "key_blank", "answer_blank", "answer_utf8", "timestamp_unsafe"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, _, teams, command := prepareTeamAssistanceAnswer(t)
			if _, err := teams.AnswerAssistance(ctx, command); err != nil {
				t.Fatal(err)
			}
			if _, err := w.db.ExecContext(ctx, `DROP TRIGGER team_assistance_answer_immutable`); err != nil {
				t.Fatal(err)
			}
			column := "id"
			var value any = strings.Repeat("界", 86)
			switch change {
			case "identity_utf8":
				value = string([]byte{0xff})
			case "key_blank":
				column, value = "idempotency_key", " \n"
			case "answer_blank":
				column, value = "answer", " \n"
			case "answer_utf8":
				column, value = "answer", string([]byte{0xff})
			case "timestamp_unsafe":
				column, value = "created_at", int64(1<<53)
			}
			if _, err := w.db.ExecContext(ctx, `UPDATE team_assistance_answers SET `+column+`=? WHERE request_id=?`, value, command.RequestID); err != nil {
				t.Fatal(err)
			}
			before := assistanceAnswerReadSnapshot(t, w, ctx)
			if out, err := teams.AssistanceAnswer(ctx, command.WorkspaceID, command.SessionID, command.RequestID, command.ActorPrincipalID); !errors.Is(err, team.ErrInvalid) || out != (team.AssistanceAnswerObservation{}) {
				t.Fatalf("malformed retained answer exposed %+v %v", out, err)
			}
			if after := assistanceAnswerReadSnapshot(t, w, ctx); after != before {
				t.Fatal("invalid answer read rewrote retained history")
			}
		})
	}
}
