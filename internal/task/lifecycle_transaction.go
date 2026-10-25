package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// PauseInTransaction parks only runnable Tasks. Existing approval, dependency,
// verification or recovery waits retain their own semantics under a Team hold.
func (s *Service) PauseInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand) error {
	if tx == nil || cmd.TaskID == "" || cmd.ExpectedRevision < 1 {
		return ErrInvalidCommand
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if t.State != StateReady && t.State != StateRunning {
		return ErrInvalidTransition
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	if t.State == StateRunning {
		if active == nil || active.State != AttemptRunning {
			return ErrInvalidAttempt
		}
		if err = s.repo.TransitionAttempt(ctx, tx, active.ID, AttemptRunning, AttemptWaiting, now); err != nil {
			return err
		}
	} else if active != nil {
		return ErrActiveAttempt
	}
	if err = s.repo.Transition(ctx, tx, transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: StatePaused, UpdatedAt: now}); err != nil {
		return err
	}
	id, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"task_id": t.ID, "from": t.State, "to": StatePaused, "reason": cmd.Reason, "revision": t.Revision + 1})
	return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &t.WorkspaceID, Type: "task.paused", AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
}

// ResumeInTransaction retains the normal dependency and original-Attempt checks
// inside the trusted owner's atomic lifecycle operation.
func (s *Service) ResumeInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand) error {
	if tx == nil || strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return ErrInvalidCommand
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	now := s.clock.UnixMilli()

	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if t.State != StatePaused && t.State != StateWaitingDependency && t.State != StateWaitingApproval {
		return fmt.Errorf("%w: cannot resume from %s", ErrInvalidTransition, t.State)
	}
	// A retained Team human request needs its own explicit, answer-bound
	// Continue transaction. Generic resume must not promote the original
	// Attempt and let Worker reconciliation silently bypass that protocol.
	if repository, ok := s.repo.(ordinaryResumeRepository); ok {
		if err := repository.RequireOrdinaryResume(ctx, tx, t.ID); err != nil {
			return err
		}
	}
	if err := s.requireExecutionAdmissionTx(ctx, tx, t); err != nil {
		return err
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	next := StateReady
	var readyAt *int64
	if active != nil {
		if active.State != AttemptWaiting {
			return ErrActiveAttempt
		}
		if err := s.requireRuntimeDependencyIntegrityTx(ctx, tx, t); err != nil {
			return err
		}
		if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, AttemptRunning, now); err != nil {
			return err
		}
		next = StateRunning
	} else {
		readyAt = &now
	}
	if !CanTransition(t.State, next) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, next)
	}
	if err := s.repo.Transition(ctx, tx, transitionRecord{
		TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: next,
		UpdatedAt: now, ReadyAt: readyAt,
	}); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id": t.ID, "from": t.State, "to": next, "revision": t.Revision + 1,
	})
	return s.events.Append(ctx, tx, event.Event{
		ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.resumed",
		AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
		RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
	})
}
