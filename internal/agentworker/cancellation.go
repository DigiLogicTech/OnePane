package agentworker

import (
	"context"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// A completed reasoning call cannot apply its proposal after a Team hold or
// cancellation. Retain the original Attempt; unknown effects need reconciliation.
func (s *Service) parkIfExecutionControlRequested(ctx context.Context, run Run, res TickResult) (TickResult, bool) {
	parked := false
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT t.state FROM tasks t
		 JOIN agent_worker_runs r ON r.task_id=t.id AND r.workspace_id=t.workspace_id
		 JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id AND a.worker_principal_id=r.worker_principal_id
		 WHERE r.id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='running'
		 AND r.worker_principal_id=? AND a.status IN ('running','waiting')
		 AND NOT EXISTS(SELECT 1 FROM task_attempts other WHERE other.task_id=t.id AND other.id<>a.id AND other.status IN ('created','queued','running','waiting'))`, run.ID, run.TaskID, run.AttemptID, run.Revision, WorkerPrincipal).Scan(&state); err != nil {
			return err
		}
		pause := false
		if state == "paused" {
			var held int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_execution_held_tasks ht JOIN team_execution_holds h ON h.id=ht.hold_id AND h.released_at IS NULL WHERE ht.task_id=?`, run.TaskID).Scan(&held); err != nil {
				return err
			}
			if held != 1 {
				return nil
			}
			var valid int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=? AND id=? AND worker_principal_id=? AND status='waiting'`, run.TaskID, run.AttemptID, WorkerPrincipal).Scan(&valid); err != nil {
				return err
			}
			if valid != 1 {
				return ErrInvalidWorkerState
			}
			pause = true
		}
		if !pause && state != "cancel_requested" && state != "cancelling" && state != "compensating" {
			return nil
		}
		if !pause {
			if err := s.tasks.ParkCancellingWorkerAttemptInTransaction(ctx, tx, run.TaskID, run.AttemptID, WorkerPrincipal); err != nil {
				return err
			}
		}
		continuation := `{"cancellation_requested":true}`
		status := "cancellation_requested"
		if pause {
			continuation = `{"team_execution_paused":true}`
			status = "team_execution_paused"
		}
		changed, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs SET status='waiting',continuation_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='running'`, continuation, s.clock.UnixMilli(), run.ID, run.Revision)
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
		res.Status = status
		return s.journalInTransaction(ctx, tx, run.ID, "model", "waiting", nil, nil, nil, nil, map[string]any{"state": status, "proposal_discarded": true})
	})
	if err != nil {
		s.noteSchedulingHalt(run)
		return failedResult(res, fmt.Errorf("execution control observation/park failed; reconciliation required: %w", err)), true
	}
	if parked {
		res.Error = ""
	}
	return res, parked
}
