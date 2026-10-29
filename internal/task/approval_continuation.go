package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// ResumeApprovalAttemptInTransaction is a trusted control-plane primitive for
// explicit human approval. It resumes only the exact original waiting Attempt
// of an approval-waiting Task, preserving ordinary execution admission. It
// cannot resume manual pauses or dependency waits or create a new Attempt.
//
// This helper does not itself authorize an approval. Its caller MUST validate
// the authenticated human, current permissions and feature policy, original
// assistance request and final answer, and exact Worker authority in this SAME
// transaction, and atomically commit the Worker journal and immutable applied
// answer receipt. Never expose it through a generic Task resume or model tool.
func (s *Service) ResumeApprovalAttemptInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand, attemptID string) error {
	if tx == nil || strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 ||
		strings.TrimSpace(attemptID) == "" || cmd.ActorPrincipalID == nil || strings.TrimSpace(*cmd.ActorPrincipalID) == "" {
		return fmt.Errorf("%w: transaction, Task revision, original Attempt and approval actor required", ErrInvalidCommand)
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if t.State != StateWaitingApproval || !CanTransition(t.State, StateRunning) {
		return fmt.Errorf("%w: explicit approval requires approval-waiting Task, found %s", ErrInvalidTransition, t.State)
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	if active == nil || active.ID != attemptID || active.State != AttemptWaiting || active.StartedAt == nil || active.EndedAt != nil {
		return fmt.Errorf("%w: explicit approval requires the original waiting Attempt", ErrInvalidAttempt)
	}
	if t.ArchivedAt != nil {
		return fmt.Errorf("%w: archived Task cannot continue", ErrInvalidTransition)
	}
	if err := s.requireRuntimeDependencyIntegrityTx(ctx, tx, t); err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	if err := s.repo.TransitionAttempt(ctx, tx, active.ID, AttemptWaiting, AttemptRunning, now); err != nil {
		return err
	}
	if err := s.repo.Transition(ctx, tx, transitionRecord{
		TaskID: t.ID, ExpectedRevision: t.Revision, From: StateWaitingApproval, To: StateRunning, UpdatedAt: now,
	}); err != nil {
		return err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id": t.ID, "attempt_id": attemptID, "from": StateWaitingApproval,
		"to": StateRunning, "reason": cmd.Reason, "revision": t.Revision + 1,
	})
	return s.events.Append(ctx, tx, event.Event{
		ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.resumed",
		AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
		RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
	})
}
