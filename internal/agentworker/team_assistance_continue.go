package agentworker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/featurepolicy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

// ContinueAssistanceInTransaction is a trusted Team collaborator, not an API or
// model proposal. Team validates the exact request/answer and human credential
// in this same transaction and retains the immutable continuation receipt after
// this method returns. No call may commit this transition without that receipt.
// It reuses the original Attempt and ordinary admission; it never releases a
// hold, creates an Attempt or refunds accounting.
func (s *Service) ContinueAssistanceInTransaction(ctx context.Context, tx storage.Tx, c team.AssistanceContinuationCommand) (string, error) {
	if s == nil || s.tasks == nil || s.clock == nil || tx == nil {
		return "", ErrInvalidWorkerState
	}
	r, a := c.Request, c.Answer
	if r.RequestID == "" || r.TaskID == "" || r.AttemptID == "" || r.RunID == "" || r.WaitStepID == "" ||
		r.TaskState != string(task.StateWaitingApproval) || r.TaskRevision < 1 || r.WorkerRevision < 1 ||
		a.AnswerID == "" || a.RequestID != r.RequestID || c.ActorPrincipalID == "" {
		return "", ErrInvalidWorkerState
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_worker_runs r
 JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id AND t.state='waiting_approval' AND t.archived_at IS NULL AND t.revision=?
 JOIN workspaces tenant ON tenant.id=r.workspace_id AND tenant.status='active'
 JOIN principals worker ON worker.id=r.worker_principal_id AND worker.id='system:agent-worker' AND worker.principal_type='system' AND worker.status='active'
 JOIN workspace_memberships wm ON wm.workspace_id=r.workspace_id AND wm.principal_id=worker.id AND wm.status='active'
 JOIN task_attempts attempt ON attempt.id=r.attempt_id AND attempt.task_id=r.task_id AND attempt.worker_principal_id=worker.id AND attempt.status='waiting' AND attempt.ended_at IS NULL
 AND attempt.id=(SELECT id FROM task_attempts WHERE task_id=t.id ORDER BY attempt_number DESC LIMIT 1)
 AND (SELECT COUNT(*) FROM task_attempts active WHERE active.task_id=t.id AND active.status IN ('created','queued','running','waiting'))=1
 AND json_extract(CASE WHEN length(CAST(attempt.metadata_json AS BLOB))<=16384 AND json_valid(attempt.metadata_json) THEN attempt.metadata_json ELSE '{}' END,'$.agent_worker')='v1'
 JOIN agent_worker_steps step ON step.id=? AND step.run_id=r.id AND step.step_kind='human' AND step.proposal_type='human' AND step.status='waiting' AND step.completed_at IS NOT NULL
 AND step.step_number=(SELECT MAX(step_number) FROM agent_worker_steps WHERE run_id=r.id)
 JOIN team_assistance_requests h ON h.id=? AND h.wait_step_id=step.id AND h.run_id=r.id AND h.task_id=t.id AND h.attempt_id=attempt.id AND h.workspace_id=r.workspace_id
 JOIN team_assistance_answers answer ON answer.request_id=h.id AND answer.id=?
 JOIN principals human ON human.id=? AND human.principal_type='human' AND human.status='active'
 JOIN workspace_memberships hm ON hm.workspace_id=r.workspace_id AND hm.principal_id=human.id AND hm.status='active'
 WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='waiting')`,
		r.TaskRevision, r.WaitStepID, r.RequestID, a.AnswerID, c.ActorPrincipalID,
		r.RunID, r.WorkspaceID, r.TaskID, r.AttemptID, r.WorkerRevision).Scan(&valid)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", ErrInvalidWorkerState
	}
	allowed, err := (featurepolicy.StoredScopes{DB: s.db}).AuthorizeTaskTx(ctx, tx, r.WorkspaceID, r.TaskID, "tasks.autonomous")
	if err != nil {
		return "", fmt.Errorf("assistance continuation autonomy unavailable: %w", err)
	}
	if !allowed {
		return "", fmt.Errorf("assistance continuation autonomy revoked")
	}
	// This transaction independently validates the exact approval transition;
	// ordinary running-step admission rejects its still-waiting Task.
	if _, err := teamgate.RequireAdmission(ctx, tx, r.WorkspaceID, r.TaskID); err != nil {
		return "", err
	}
	actor := c.ActorPrincipalID
	if err := s.tasks.ResumeApprovalAttemptInTransaction(ctx, tx, task.TransitionCommand{
		TaskID: r.TaskID, ExpectedRevision: r.TaskRevision, ActorPrincipalID: &actor,
		Reason: "authenticated Team assistance continued by human",
	}, r.AttemptID); err != nil {
		return "", err
	}
	changed, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs AS r SET status='running',continuation_json='{}',last_error=NULL,revision=revision+1,updated_at=?
 WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='waiting' AND `+activeWorkerExecutionBindingPredicate,
		s.clock.UnixMilli(), r.RunID, r.WorkspaceID, r.TaskID, r.AttemptID, r.WorkerRevision)
	if err != nil {
		return "", err
	}
	n, err := changed.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrInvalidWorkerState
	}
	proposal := "human"
	stepID, err := s.journalStepInTransaction(ctx, tx, r.RunID, "human", "succeeded", nil, nil, &proposal, nil, map[string]any{
		"action": "assistance_continued", "request_id": r.RequestID, "answer_id": a.AnswerID,
		"wait_step_id": r.WaitStepID, "actor_principal_id": actor,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidWorkerState
	}
	return stepID, err
}

var _ team.AssistanceContinuationWorker = (*Service)(nil)
