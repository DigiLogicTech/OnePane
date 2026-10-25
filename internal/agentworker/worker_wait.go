package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

// Wait/approval must never persist a WAITING Worker before the Task and
// original Attempt have actually entered their matching waiting states.
// Failed commits keep the running Worker for explicit crash-style recovery.
func (s *Service) waitRunAtomically(
	ctx context.Context, run Run, res TickResult, next task.State,
	reason string, continuation json.RawMessage, kind, resultStatus string,
) TickResult {
	if s == nil || s.tx == nil || s.tasks == nil {
		return failedResult(res, ErrInvalidWorkerState)
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var state string
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT r.status,t.revision FROM agent_worker_runs r
			JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
			WHERE r.id=? AND r.workspace_id=? AND r.task_id=?
				AND r.attempt_id=? AND r.revision=? AND `+activeWorkerExecutionBindingPredicate,
			run.ID, run.WorkspaceID, run.TaskID, run.AttemptID, run.Revision,
		).Scan(&state, &revision)
		if err != nil {
			return err
		}
		if state != string(RunRunning) {
			return ErrInvalidWorkerState
		}
		// A recorded model proposal cannot park a now-ineligible contribution.
		// Recheck sealed accepted-Team scope, dependencies and ancestor holds in
		// the same transaction before changing Task, Attempt or Worker state.
		if _, err := teamgate.Require(ctx, tx, run.WorkspaceID, run.TaskID); err != nil {
			return err
		}
		actor := WorkerPrincipal
		if err := s.tasks.WorkerWaitInTransaction(ctx, tx, task.TransitionCommand{
			TaskID: run.TaskID, ExpectedRevision: revision, ActorPrincipalID: &actor,
			Reason: boundedString(reason, 512),
		}, run.AttemptID, WorkerPrincipal, next); err != nil {
			return err
		}
		now := s.clock.UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs
			SET status='waiting',continuation_json=?,revision=revision+1,
				updated_at=? WHERE id=? AND workspace_id=? AND task_id=?
				AND attempt_id=? AND revision=? AND status='running'`,
			string(continuation), now, run.ID, run.WorkspaceID, run.TaskID, run.AttemptID, run.Revision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrInvalidWorkerState
		}
		var proposalType string
		switch kind {
		case "human":
			proposalType = "human"
		case "wait":
			proposalType = "wait"
		default:
			return fmt.Errorf("unknown Worker wait reason kind %q", kind)
		}
		return s.journalInTransaction(ctx, tx, run.ID, kind, "waiting",
			nil, nil, &proposalType, nil, map[string]any{"reason": "worker_wait_safely_committed"})
	})
	if err != nil {
		s.dispatchReconciliationFailed = true
		if err == sql.ErrNoRows {
			err = ErrInvalidWorkerState
		}
		return failedResult(res, fmt.Errorf("Worker wait commit failed; scheduling halted until recovery: %w", err))
	}
	res.Status = resultStatus
	return res
}
