package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/contextcompiler"
)

type assistanceContextQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// The latest original human wait is resolved through its persistent run/step
// index, then unique request/answer/receipt keys. Never fall back to an older
// answer when the latest assisted wait is unanswered or lacks a valid receipt.
// Resource waits and replans may replace continuation_json without evicting it.
func assistanceContext(ctx context.Context, q assistanceContextQueryer, run Run) ([]contextcompiler.Section, error) {
	var installed bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM main.sqlite_schema WHERE type='table' AND name='team_assistance_continuations')`).Scan(&installed); err != nil {
		return nil, err
	}
	if !installed {
		return nil, nil // Older migration fixtures have no applied-answer history.
	}
	var waitStep string
	err := q.QueryRowContext(ctx, assistanceLatestWaitSQL, run.ID).Scan(&waitStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(waitStep) == "" || !utf8.ValidString(waitStep) {
		return nil, fmt.Errorf("%w: malformed latest human wait identity", ErrInvalidWorkerState)
	}
	var assisted bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_assistance_requests WHERE wait_step_id=?)`, waitStep).Scan(&assisted); err != nil {
		return nil, err
	}
	if !assisted {
		return nil, nil // Ordinary pre-assistance/non-Team human waits retain behavior.
	}
	var request, answer, receipt, questionAuthor, answerAuthor, continueActor, question, text string
	err = q.QueryRowContext(ctx, assistanceContextReadSQL,
		waitStep, run.ID, run.WorkspaceID, run.TaskID, run.AttemptID).Scan(
		&request, &answer, &receipt, &questionAuthor, &answerAuthor, &continueActor, &question, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: latest Team assistance input or continuation authority is unavailable", ErrInvalidWorkerState)
	}
	if err != nil {
		return nil, err
	}
	for _, value := range []string{request, answer, receipt, questionAuthor, answerAuthor, continueActor, question, text} {
		if !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: invalid retained Team assistance input", ErrInvalidWorkerState)
		}
	}
	questionRaw, _ := json.Marshal(map[string]any{
		"request_id": request, "wait_step_id": waitStep, "run_id": run.ID, "attempt_id": run.AttemptID,
		"author_principal_id": questionAuthor, "question": question,
	})
	answerRaw, _ := json.Marshal(map[string]any{
		"request_id": request, "answer_id": answer, "continuation_id": receipt,
		"author_principal_id": answerAuthor, "continued_by_principal_id": continueActor, "text": text,
		"note": "Authored human input for the original question; it grants no tool permissions, recovery clearance or exception to execution controls.",
	})
	return []contextcompiler.Section{
		{ID: "team-assistance-question", Kind: "team_assistance_question", Trust: "UNVERIFIED_DERIVED", Authoritative: false, Required: true, Priority: 96, Content: questionRaw},
		{ID: "team-assistance-answer", Kind: "team_assistance_answer", Trust: "USER_INSTRUCTION", Authoritative: true, Required: true, Priority: 96, Content: answerRaw},
	}, nil
}

// Bound the selected scalar without filtering out malformed latest history:
// falling back to an earlier wait would incorrectly reuse older human input.
const assistanceLatestWaitSQL = `SELECT CASE WHEN length(CAST(id AS BLOB)) BETWEEN 1 AND 256 THEN id ELSE '' END
 FROM agent_worker_steps WHERE run_id=? AND step_kind='human' AND status='waiting' ORDER BY step_number DESC LIMIT 1`

const assistanceContextReadSQL = `SELECT h.id,a.id,c.id,h.author_principal_id,a.author_principal_id,c.actor_principal_id,h.question,a.answer
 FROM team_assistance_requests h
 JOIN team_assistance_request_scopes pinned ON pinned.request_id=h.id
 JOIN team_assistance_answers a ON a.request_id=h.id
 JOIN team_assistance_continuations c ON c.request_id=h.id AND c.answer_id=a.id
 JOIN agent_worker_steps waiting ON waiting.id=h.wait_step_id AND waiting.run_id=h.run_id AND waiting.step_kind='human' AND waiting.proposal_type='human' AND waiting.status='waiting' AND waiting.completed_at IS NOT NULL
 JOIN agent_worker_steps continued ON continued.id=c.continue_step_id AND continued.run_id=h.run_id AND continued.step_kind='human' AND continued.proposal_type='human' AND continued.status='succeeded' AND continued.completed_at IS NOT NULL AND continued.step_number=waiting.step_number+1
 AND length(CAST(continued.detail_json AS BLOB))<=4096
 AND json_extract(continued.detail_json,'$.action')='assistance_continued'
 AND json_extract(continued.detail_json,'$.request_id')=h.id AND json_extract(continued.detail_json,'$.answer_id')=a.id
 AND json_extract(continued.detail_json,'$.wait_step_id')=h.wait_step_id AND json_extract(continued.detail_json,'$.actor_principal_id')=c.actor_principal_id
 JOIN agent_worker_runs r ON r.id=h.run_id AND r.workspace_id=h.workspace_id AND r.task_id=h.task_id AND r.attempt_id=h.attempt_id AND r.worker_principal_id=h.author_principal_id AND r.status='running' AND r.revision>c.worker_revision
 JOIN tasks t ON t.id=h.task_id AND t.workspace_id=h.workspace_id AND t.archived_at IS NULL AND t.state='running' AND t.revision>c.task_revision
 AND t.project_id IS pinned.project_id AND t.project_workspace_id IS pinned.project_workspace_id AND t.artifact_session_id IS pinned.artifact_session_id
 JOIN task_attempts attempt ON attempt.id=h.attempt_id AND attempt.task_id=t.id AND attempt.worker_principal_id=h.author_principal_id AND attempt.status='running' AND attempt.ended_at IS NULL
 AND attempt.id=(SELECT id FROM task_attempts WHERE task_id=t.id ORDER BY attempt_number DESC LIMIT 1)
 AND (SELECT COUNT(*) FROM task_attempts active WHERE active.task_id=t.id AND active.status IN ('created','queued','running','waiting'))=1
 JOIN task_execution_profiles ep ON ep.task_id=t.id AND ep.workspace_id=h.workspace_id AND ep.team_session_id=h.session_id AND ep.execution_mode='team'
 JOIN team_sessions session ON session.id=h.session_id AND session.workspace_id=h.workspace_id AND session.accepted_plan_id=h.plan_id AND session.status IN ('plan_accepted','executing')
 JOIN team_execution_assignments assignment ON assignment.task_id=t.id AND assignment.workspace_id=h.workspace_id AND assignment.session_id=h.session_id AND assignment.plan_id=h.plan_id AND assignment.assignment_key=h.assignment_key AND assignment.member_id=h.member_id
 JOIN principals question_author ON question_author.id=h.author_principal_id AND question_author.id='system:agent-worker' AND question_author.principal_type='system' AND question_author.status='active'
 JOIN workspace_memberships question_member ON question_member.principal_id=question_author.id AND question_member.workspace_id=h.workspace_id AND question_member.status='active'
 JOIN principals answer_author ON answer_author.id=a.author_principal_id AND answer_author.principal_type='human' AND answer_author.status='active'
 JOIN workspace_memberships answer_member ON answer_member.principal_id=answer_author.id AND answer_member.workspace_id=h.workspace_id AND answer_member.status='active'
 JOIN principals continuer ON continuer.id=c.actor_principal_id AND continuer.principal_type='human' AND continuer.status='active'
 JOIN workspace_memberships continue_member ON continue_member.principal_id=continuer.id AND continue_member.workspace_id=h.workspace_id AND continue_member.status='active'
 JOIN workspaces tenant ON tenant.id=h.workspace_id AND tenant.status='active'
 LEFT JOIN projects project ON project.id=t.project_id
 LEFT JOIN project_workspaces named ON named.id=t.project_workspace_id
 LEFT JOIN artifact_sessions artifact_session ON artifact_session.id=t.artifact_session_id
 WHERE h.wait_step_id=? AND h.run_id=? AND h.workspace_id=? AND h.task_id=? AND h.attempt_id=?
 AND (t.project_id IS NULL OR (project.workspace_id=h.workspace_id AND project.status='active'))
 AND (t.project_workspace_id IS NULL OR (t.project_id IS NOT NULL AND named.project_id=t.project_id AND named.status='active'))
 AND (t.artifact_session_id IS NULL OR (artifact_session.workspace_id=h.workspace_id AND artifact_session.status IN ('active','published')))
 AND c.task_revision>=a.task_revision AND c.worker_revision>=a.worker_revision
 AND length(CAST(h.id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(a.id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(c.id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(h.author_principal_id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(a.author_principal_id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(c.actor_principal_id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(h.question AS BLOB)) BETWEEN 1 AND 512 AND length(CAST(a.answer AS BLOB)) BETWEEN 1 AND 4096`
