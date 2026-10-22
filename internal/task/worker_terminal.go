package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// WorkerTerminalInTransaction commits the ordinary Agent Worker terminal Task
// and exact Attempt transition inside its caller's Worker transaction. This
// trusted control-plane primitive must not be exposed to model-defined tools.
// Unlike a generic optional-Attempt Task transition, it refuses to select a
// different live Attempt or silently succeed without one.
func (s *Service) WorkerTerminalInTransaction(
	ctx context.Context, tx storage.Tx, cmd TransitionCommand,
	attemptID, workerPrincipalID string, next State,
) error {
	if s == nil || tx == nil || strings.TrimSpace(cmd.TaskID) == "" ||
		cmd.ExpectedRevision < 1 || strings.TrimSpace(attemptID) == "" ||
		strings.TrimSpace(workerPrincipalID) == "" {
		return fmt.Errorf("%w: exact Worker terminal transaction required", ErrInvalidCommand)
	}
	var attemptNext AttemptState
	var eventType string
	switch next {
	case StateBlocked:
		attemptNext, eventType = AttemptWaiting, "task.blocked"
	case StateFailed:
		attemptNext, eventType = AttemptFailed, "task.failed"
	default:
		return fmt.Errorf("%w: unsupported Worker terminal state %s", ErrInvalidCommand, next)
	}

	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil { return err }
	if t.Revision != cmd.ExpectedRevision { return ErrRevisionConflict }
	if !CanTransition(t.State, next) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, next)
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil { return err }
	if active == nil || active.ID != attemptID ||
		active.WorkerPrincipalID == nil || *active.WorkerPrincipalID != workerPrincipalID {
		return ErrInvalidAttempt
	}
	var activeCount int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts
		WHERE task_id=? AND status IN ('created','queued','running','waiting')`, t.ID).Scan(&activeCount)
	if err != nil { return err }
	if activeCount != 1 { return ErrInvalidAttempt }
	if !CanTransitionAttempt(active.State, attemptNext) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidAttempt, active.State, attemptNext)
	}
	now := s.clock.UnixMilli()
	eventID, err := s.ids.New("evt")
	if err != nil { return err }
	if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, attemptNext, now); err != nil {
		return err
	}
	if err := s.repo.Transition(ctx, tx, transitionRecord{
		TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State,
		To: next, UpdatedAt: now,
	}); err != nil { return err }
	payload, err := json.Marshal(map[string]any{
		"task_id": t.ID, "from": t.State, "to": next,
		"reason": cmd.Reason, "revision": t.Revision + 1,
	})
	if err != nil { return err }
	return s.events.Append(ctx, tx, event.Event{
		ID: eventID, WorkspaceID: &t.WorkspaceID, Type: eventType,
		AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
		RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
	})
}
