package task

import (
	"context"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// currentTeamAssistanceWait is a narrow generic-resume fence, not Continue
// authority. Only the current waiting Attempt/Worker's latest committed human
// step and its immutable request match. Older requests must not lock out a
// later ordinary wait or a replacement Attempt. An answer alone cannot lift it.
func currentTeamAssistanceWait(ctx context.Context, tx storage.Tx, taskID, attemptID string) (bool, error) {
	var installed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM main.sqlite_schema WHERE type='table' AND name='team_assistance_requests')`).Scan(&installed); err != nil {
		return false, err
	}
	// Older Task-only schemas have no assistance history and retain their
	// original lifecycle behavior; do not prepare SQL against missing tables.
	if !installed {
		return false, nil
	}
	var waiting bool
	err := tx.QueryRowContext(ctx, currentTeamAssistanceWaitSQL, taskID, attemptID, attemptID).Scan(&waiting)
	return waiting, err
}

const currentTeamAssistanceWaitSQL = `SELECT EXISTS(
 SELECT 1 FROM main.task_attempts a
 JOIN main.agent_worker_runs r ON r.attempt_id=a.id AND r.task_id=a.task_id AND r.status='waiting'
 JOIN main.agent_worker_steps step ON step.run_id=r.id
 AND step.step_number=(SELECT MAX(latest.step_number) FROM main.agent_worker_steps latest WHERE latest.run_id=r.id)
 AND step.step_kind='human' AND step.proposal_type='human' AND step.status='waiting' AND step.completed_at IS NOT NULL
 JOIN main.team_assistance_requests h ON h.wait_step_id=step.id AND h.run_id=r.id AND h.task_id=a.task_id AND h.attempt_id=a.id
 WHERE a.task_id=? AND (?='' OR a.id=?) AND a.status IN ('waiting','running') AND a.ended_at IS NULL
 AND a.id=(SELECT id FROM main.task_attempts WHERE task_id=a.task_id ORDER BY attempt_number DESC LIMIT 1)
 )`

// RequireOrdinaryResumeInTransaction rejects the current durable human wait,
// including legacy Task/Attempt projections already marked running. Callers
// must retain their own exact original-owner CAS and ordinary admission. Empty
// attemptID checks the Task's current active owner; Worker callers pin its ID.
// This is never permission to Continue or an applied-answer receipt.
func RequireOrdinaryResumeInTransaction(ctx context.Context, tx storage.Tx, taskID, attemptID string) error {
	if tx == nil || taskID == "" {
		return ErrInvalidCommand
	}
	assisted, err := currentTeamAssistanceWait(ctx, tx, taskID, attemptID)
	if err != nil {
		return err
	}
	if assisted {
		return fmt.Errorf("%w: Team assistance requires explicit Continue", ErrInvalidTransition)
	}
	return nil
}

type ordinaryResumeRepository interface {
	RequireOrdinaryResume(context.Context, storage.Tx, string) error
}

// Production Task repositories persist assistance in SQLite. In-memory
// repositories have no durable assistance tables and retain their own lifecycle.
func (r *sqlRepository) RequireOrdinaryResume(ctx context.Context, tx storage.Tx, taskID string) error {
	return RequireOrdinaryResumeInTransaction(ctx, tx, taskID, "")
}
