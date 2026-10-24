package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// ContinueAfterVerificationWorkerInTransaction resumes only the exact original
// waiting Worker Attempt. The caller commits its Worker and retry journal in
// this same transaction. This is a trusted control-plane boundary, not recovery
// disposition or permission to create another Attempt.
func (s *Service) ContinueAfterVerificationWorkerInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand, attemptID, workerPrincipal string) error {
	if s == nil || tx == nil || strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(attemptID) == "" || strings.TrimSpace(workerPrincipal) == "" {
		return fmt.Errorf("%w: transaction, Task revision and original Worker Attempt required", ErrInvalidCommand)
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if t.State != StateVerifying || !CanTransition(t.State, StateRunning) {
		return ErrInvalidTransition
	}
	a, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	if a == nil || a.ID != attemptID || a.State != AttemptWaiting || a.WorkerPrincipalID == nil || *a.WorkerPrincipalID != workerPrincipal {
		return ErrInvalidAttempt
	}
	if err = s.requireRuntimeDependencyIntegrityTx(ctx, tx, t); err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	if err = s.repo.TransitionAttempt(ctx, tx, a.ID, AttemptWaiting, AttemptRunning, now); err != nil {
		return err
	}
	if err = s.repo.Transition(ctx, tx, transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision, From: StateVerifying, To: StateRunning, UpdatedAt: now}); err != nil {
		return err
	}
	id, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"task_id": t.ID, "attempt_id": a.ID, "from": StateVerifying, "to": StateRunning, "reason": cmd.Reason, "revision": t.Revision + 1})
	return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &t.WorkspaceID, Type: "task.verification_retry", AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: body, OccurredAt: now})
}
