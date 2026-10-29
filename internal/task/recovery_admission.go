package task

import (
	"context"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// ErrRecoveryRequired remains an invalid ordinary lifecycle transition. Only a
// future trusted reconciliation owner may disposition an interrupted execution;
// changing a Task's state or consuming its outbox job cannot clear this fence.
var ErrRecoveryRequired = fmt.Errorf("%w: interrupted execution requires human recovery", ErrInvalidTransition)

func (s *Service) requireExecutionAdmissionTx(ctx context.Context, tx storage.Tx, t Task) error {
	if err := s.repo.RequireNoExecutionHold(ctx, tx, t); err != nil {
		return err
	}
	return s.repo.RequireRecoveryClear(ctx, tx, t)
}

func (r *sqlRepository) RequireRecoveryClear(ctx context.Context, tx storage.Tx, t Task) error {
	var interrupted, foreign, rooted bool
	var count int
	// Admission for delegated children must respect their original parent's
	// recovery ownership too. UNION terminates corrupt cycles; a truncated,
	// foreign or rootless ancestry cannot establish safe admission.
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestry(id,parent,workspace) AS (
 SELECT id,parent_task_id,workspace_id FROM tasks WHERE id=? AND workspace_id=?
 UNION SELECT p.id,p.parent_task_id,p.workspace_id FROM tasks p JOIN ancestry a ON p.id=a.parent LIMIT 257
 ) SELECT
 EXISTS(SELECT 1 FROM ancestry a WHERE
 EXISTS(SELECT 1 FROM task_attempts attempt WHERE attempt.task_id=a.id AND attempt.status='interrupted') OR
 EXISTS(SELECT 1 FROM events e WHERE e.aggregate_type='task' AND e.aggregate_id=a.id AND e.event_type='task.attempt_interrupted')),
 COUNT(*),EXISTS(SELECT 1 FROM ancestry WHERE workspace<>?),
 EXISTS(SELECT 1 FROM ancestry WHERE parent IS NULL) FROM ancestry`, t.ID, t.WorkspaceID, t.WorkspaceID).Scan(&interrupted, &count, &foreign, &rooted)
	if err != nil {
		return err
	}
	if interrupted || count == 0 || count > 256 || foreign || !rooted {
		return ErrRecoveryRequired
	}
	return nil
}
