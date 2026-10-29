package task

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// RequestCancelInTransaction lets a trusted owner request cancellation of a
// bounded Task graph atomically. It records intent, not completed cancellation
// or proof that already-dispatched external work stopped.
func (s *Service) RequestCancelInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand) error {
	if tx == nil || strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return ErrInvalidCommand
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if !CanTransition(t.State, StateCancelRequested) {
		return ErrInvalidTransition
	}
	now := s.clock.UnixMilli()
	if err = s.repo.Transition(ctx, tx, transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: StateCancelRequested, UpdatedAt: now, CancelRequestedAt: &now}); err != nil {
		return err
	}
	id, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"task_id": t.ID, "from": t.State, "to": StateCancelRequested, "reason": cmd.Reason, "revision": t.Revision + 1})
	return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &t.WorkspaceID, Type: "task.cancel_requested", AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
}

// ParkCancellingWorkerAttemptInTransaction preserves the original Attempt while
// the cancellation owner reconciles it. It never declares cancellation complete.
func (s *Service) ParkCancellingWorkerAttemptInTransaction(ctx context.Context, tx storage.Tx, taskID, attemptID, principal string) error {
	if tx == nil || taskID == "" || attemptID == "" || principal == "" {
		return ErrInvalidCommand
	}
	t, err := s.repo.GetForUpdate(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if t.State != StateCancelRequested && t.State != StateCancelling && t.State != StateCompensating {
		return ErrInvalidTransition
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if active == nil || active.ID != attemptID || active.WorkerPrincipalID == nil || *active.WorkerPrincipalID != principal {
		return ErrInvalidAttempt
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=? AND status IN ('created','queued','running','waiting')`, taskID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidAttempt
	}
	if active.State == AttemptWaiting {
		return nil
	}
	if active.State != AttemptRunning {
		return ErrInvalidAttempt
	}
	return s.repo.TransitionAttempt(ctx, tx, attemptID, AttemptRunning, AttemptWaiting, s.clock.UnixMilli())
}
