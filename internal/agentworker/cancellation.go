package agentworker

import (
	"context"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// A completed reasoning call cannot apply its proposal after cancellation.
// Keep the original Attempt and unknown effects for the cancellation owner.
func (s *Service) parkIfCancellationRequested(ctx context.Context, run Run, res TickResult) (TickResult, bool) {
	parked := false
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT t.state FROM tasks t JOIN agent_worker_runs r ON r.task_id=t.id AND r.workspace_id=t.workspace_id WHERE r.id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='running'`, run.ID, run.TaskID, run.AttemptID, run.Revision).Scan(&state); err != nil {
			return err
		}
		if state != "cancel_requested" && state != "cancelling" && state != "compensating" {
			return nil
		}
		if err := s.tasks.ParkCancellingWorkerAttemptInTransaction(ctx, tx, run.TaskID, run.AttemptID, WorkerPrincipal); err != nil {
			return err
		}
		changed, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs SET status='waiting',continuation_json='{"cancellation_requested":true}',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='running'`, s.clock.UnixMilli(), run.ID, run.Revision)
		if err != nil {
			return err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrInvalidWorkerState
		}
		parked = true
		return s.journalInTransaction(ctx, tx, run.ID, "model", "waiting", nil, nil, nil, nil, map[string]any{"state": "cancellation_requested", "proposal_discarded": true})
	})
	if err != nil {
		s.dispatchReconciliationFailed = true
		return failedResult(res, fmt.Errorf("cancellation observation/park failed; reconciliation required: %w", err)), true
	}
	if parked {
		res.Status = "cancellation_requested"
		res.Error = ""
	}
	return res, parked
}
