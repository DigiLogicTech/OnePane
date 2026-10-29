package agentworker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

const uncommittedWriteRecoveryReason = "agent_worker_uncommitted_write_requires_reconciliation"
const uncommittedWriteLastError = "local write failed after an uncertain or uncommitted worker step"

func (s *Service) noteSchedulingHalt(run Run) {
	if s == nil {
		return
	}
	s.haltMu.Lock()
	s.dispatchReconciliationFailed = true
	if run.ID != "" {
		if s.haltedRuns == nil {
			s.haltedRuns = map[string]Run{}
		}
		s.haltedRuns[run.ID] = run
	}
	s.haltMu.Unlock()
	s.publishWorkerHealth()
}

func (s *Service) clearSchedulingHalt() {
	if s == nil {
		return
	}
	s.haltMu.Lock()
	s.dispatchReconciliationFailed = false
	s.haltedRuns = nil
	s.quarantine = nil
	s.haltMu.Unlock()
	s.workerHalt.Store(false)
	s.workerHaltReason.Store("")
}

func (s *Service) publishWorkerHealth() {
	s.haltMu.Lock()
	halted := s.dispatchReconciliationFailed
	isolated := len(s.quarantine) > 0
	s.haltMu.Unlock()
	switch {
	case halted:
		s.workerHalt.Store(true)
		s.workerHaltReason.Store("autonomous worker is parking an uncommitted write; unknown side effects are not replayed")
	case isolated:
		s.workerHalt.Store(true)
		s.workerHaltReason.Store("a worker with an uncertain outcome is isolated; other autonomous work continues")
	default:
		s.workerHalt.Store(false)
		s.workerHaltReason.Store("")
	}
}

func (s *Service) runQuarantined(id string) bool {
	s.haltMu.Lock()
	defer s.haltMu.Unlock()
	_, ok := s.quarantine[id]
	return ok
}

// ObservedHealth is the lock-free worker view for /v1/health. It does not
// report healthy merely because this process can answer the request.
func (s *Service) ObservedHealth() (string, string) {
	if s == nil {
		return "unobserved", "agent worker is not running"
	}
	if !s.workerHalt.Load() {
		return "ok", ""
	}
	reason, _ := s.workerHaltReason.Load().(string)
	if reason == "" {
		reason = "autonomous worker is halted"
	}
	return "halted", reason
}

// reconcileSchedulingHalt retries only the local park of workers whose
// write failed. It does not call a model, tool, or external runtime.
// A transient store error keeps scheduling stopped. A worker that cannot
// be parked without guessing is isolated so other work can continue.
func (s *Service) reconcileSchedulingHalt(ctx context.Context) error {
	s.haltMu.Lock()
	if !s.dispatchReconciliationFailed && len(s.haltedRuns) == 0 {
		s.haltMu.Unlock()
		return nil
	}
	pending := make([]Run, 0, len(s.haltedRuns))
	for _, run := range s.haltedRuns {
		pending = append(pending, run)
	}
	unidentified := s.dispatchReconciliationFailed && len(pending) == 0
	s.haltMu.Unlock()
	if unidentified {
		return ErrDispatchRequiresReconciliation
	}
	var transient error
	for _, run := range pending {
		err := s.parkRecordedRun(ctx, run)
		s.haltMu.Lock()
		if err == nil {
			delete(s.haltedRuns, run.ID)
			delete(s.quarantine, run.ID)
		} else if errors.Is(err, ErrInvalidWorkerState) {
			delete(s.haltedRuns, run.ID)
			if s.quarantine == nil {
				s.quarantine = map[string]string{}
			}
			s.quarantine[run.ID] = err.Error()
		} else {
			transient = errors.Join(transient, err)
		}
		s.haltMu.Unlock()
	}
	s.haltMu.Lock()
	s.dispatchReconciliationFailed = len(s.haltedRuns) > 0 || transient != nil
	s.haltMu.Unlock()
	s.publishWorkerHealth()
	if transient != nil {
		return errors.Join(ErrDispatchRequiresReconciliation, transient)
	}
	return nil
}

func (s *Service) parkRecordedRun(ctx context.Context, run Run) error {
	var status, taskID string
	var taskRevision int64
	err := s.db.QueryRowContext(ctx, `SELECT r.status,t.id,t.revision
		FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id
		WHERE r.id=? AND r.task_id=?`, run.ID, run.TaskID).Scan(&status, &taskID, &taskRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidWorkerState
	}
	if err != nil {
		return err
	}
	if status != string(RunRunning) {
		return nil
	}
	return s.parkRunningWorker(ctx, run.ID, taskID, taskRevision, uncommittedWriteRecoveryReason, uncommittedWriteLastError)
}

func (s *Service) parkRunningWorker(ctx context.Context, runID, taskID string, taskRevision int64, reason, lastError string) error {
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var status, attemptStatus string
		var attemptPrincipal sql.NullString
		var activeCount int
		err := tx.QueryRowContext(ctx, `SELECT r.status,a.status,a.worker_principal_id,
			(SELECT COUNT(*) FROM task_attempts other
				WHERE other.task_id=r.task_id
					AND other.status IN ('created','queued','running','waiting'))
			FROM agent_worker_runs r
			JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
			JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
			WHERE r.id=? AND r.task_id=? AND t.revision=?`,
			runID, taskID, taskRevision).Scan(&status, &attemptStatus, &attemptPrincipal, &activeCount)
		if err != nil {
			return fmt.Errorf("Worker recovery identity unavailable: %w", err)
		}
		if status != string(RunRunning) || activeCount != 1 ||
			(attemptStatus != "created" && attemptStatus != "queued" &&
				attemptStatus != "running" && attemptStatus != "waiting") ||
			!attemptPrincipal.Valid || attemptPrincipal.String != WorkerPrincipal {
			return ErrInvalidWorkerState
		}
		actor := AuthorityPrincipal
		if err := s.tasks.InterruptForRecoveryInTransaction(ctx, tx, task.TransitionCommand{
			TaskID: taskID, ExpectedRevision: taskRevision, ActorPrincipalID: &actor,
			Reason: reason,
		}); err != nil {
			return fmt.Errorf("interrupt Task %s for worker recovery: %w", taskID, err)
		}
		now := s.clock.UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs SET status='interrupted',
			last_error=?,revision=revision+1,
			updated_at=?,completed_at=? WHERE id=? AND task_id=? AND status='running'`,
			lastError, now, now, runID, taskID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("worker recovery run %s lost ownership", runID)
		}
		return nil
	})
}
