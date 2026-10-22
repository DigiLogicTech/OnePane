package agentworker

import (
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"context"
)

// ErrDispatchRequiresReconciliation signals that an inference/Agent-runtime
// invocation might have consumed paid quota, produced output or performed
// external effects. The Worker must park its original Attempt for operator
// reconciliation, never automatically route to a fallback model.
var ErrDispatchRequiresReconciliation = errors.New("agent worker dispatch requires operator reconciliation")

func dispatchReconciliationError(err error) error {
	return fmt.Errorf("%w: %w", ErrDispatchRequiresReconciliation, err)
}

// Only a durable known terminal failure/rate limit proves an inference
// request was not left executing with an unknown result. Empty identifiers,
// interrupted database reads and intermediate states fail closed.
func reconcileInferenceDispatchError(q inference.InferenceRequest, err error) error {
	if err == nil {
		return nil
	}
	switch q.Status {
	case inference.RequestFailed, inference.RequestRateLimited:
		return err
	default:
		return dispatchReconciliationError(fmt.Errorf("inference request %s status %s: %w", q.ID, q.Status, err))
	}
}

// External Agent runtimes may perform their own side effects. An UNKNOWN,
// in-flight, successful-but-unreadable or unavailable Invocation is never
// proof that repeating the request is safe.
func reconcileRuntimeDispatchError(r agentruntime.InvokeResult, err error) error {
	if err == nil {
		return nil
	}
	if r.Invocation.Status == agentruntime.InvocationFailed {
		return err
	}
	return dispatchReconciliationError(fmt.Errorf("Agent runtime invocation %s status %s: %w", r.Invocation.ID, r.Invocation.Status, err))
}

const uncertainDispatchRecoveryReason = "agent_worker_dispatch_outcome_requires_operator_reconciliation"

// An external request may have consumed quota or performed side effects.
// Task, Attempt, Worker, audit and outbox must transition in one transaction.
// If SQLite fails, halt in-process scheduling until startup recovery has
// safely interrupted the still-running Worker. Never guess external outcome.
func (s *Service) interruptDispatchForReconciliation(
	ctx context.Context, run Run, res TickResult,
	stepKind, candidateKind, candidateID, resultRef string, _ error,
) TickResult {
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var revision int64
		var runStatus, taskState string
		err := tx.QueryRowContext(ctx, `SELECT r.status,t.state,t.revision
			FROM agent_worker_runs r
			JOIN tasks t ON t.id=r.task_id
			JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
				AND a.status IN ('created','queued','running','waiting')
			WHERE r.id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=?
				AND NOT EXISTS (SELECT 1 FROM task_attempts other
					WHERE other.task_id=t.id AND other.id<>r.attempt_id
						AND other.status IN ('created','queued','running','waiting'))`,
			run.ID, run.TaskID, run.AttemptID, run.Revision,
		).Scan(&runStatus, &taskState, &revision)
		if err != nil { return err }
		if runStatus != string(RunRunning) || taskState != string(task.StateRunning) {
			return ErrInvalidWorkerState
		}
		actor := AuthorityPrincipal
		if err := s.tasks.InterruptForRecoveryInTransaction(ctx, tx, task.TransitionCommand{
			TaskID: run.TaskID, ExpectedRevision: revision,
			ActorPrincipalID: &actor, Reason: uncertainDispatchRecoveryReason,
		}); err != nil { return err }
		now := s.clock.UnixMilli()
		updated, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs
			SET status='interrupted',last_error=?,revision=revision+1,
				updated_at=?,completed_at=?
			WHERE id=? AND task_id=? AND attempt_id=? AND revision=?
				AND status='running'`,
			uncertainDispatchRecoveryReason, now, now,
			run.ID, run.TaskID, run.AttemptID, run.Revision)
		if err != nil { return err }
		n, err := updated.RowsAffected()
		if err != nil { return err }
		if n != 1 { return ErrInvalidWorkerState }
		return s.journalInTransaction(ctx, tx, run.ID, stepKind, "unknown",
			&candidateKind, &candidateID, nil, &resultRef,
			map[string]any{
				"reason": uncertainDispatchRecoveryReason,
				"outcome": "unknown_or_response_unreadable",
				"action": "operator_reconciliation_required",
			})
	})
	if err != nil {
		s.dispatchReconciliationFailed = true
		return failedResult(res, fmt.Errorf(
			"external dispatch recovery commit failed; Worker scheduling halted until reconciliation: %w", err))
	}
	res.Status = "blocked"
	res.Error = "external dispatch requires operator reconciliation; automatic fallback disabled"
	return res
}
