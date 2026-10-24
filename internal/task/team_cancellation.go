package task

import (
	"context"
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// PristineCancelledInTransaction is an observation for a trusted graph owner.
// It preserves the ordinary settlement evidence contract, including Team
// exclusion, and never changes the Task or its historical records.
func (s *Service) PristineCancelledInTransaction(ctx context.Context, tx storage.Tx, id string) (bool, error) {
	t, err := s.repo.GetForUpdate(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if t.State != StateCancelled || len(t.Result) != 0 || t.CancelRequestedAt == nil {
		return false, nil
	}
	clean, err := cancellationPristine(ctx, tx, id)
	if err != nil || !clean {
		return false, err
	}
	intent, err := unstartedCancellationIntent(ctx, tx, id, t.Revision-1)
	if err != nil || !intent {
		return false, err
	}
	var proven bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events e WHERE e.aggregate_type='task' AND e.aggregate_id=? AND e.event_type='task.cancelled' AND json_extract(e.payload_json,'$.revision')=? AND json_extract(e.payload_json,'$.reason')='never_started_no_execution_evidence')`, id, t.Revision).Scan(&proven)
	return proven, err
}

// CompleteUnstartedTeamCancellationInTransaction is reserved for the trusted
// Team owner after it reconciles ALL reasoning/resource/descendant evidence in
// this same transaction. It cannot terminalize an Attempt or Worker and never
// bypasses the generic pristine maintenance guard.
func (s *Service) CompleteUnstartedTeamCancellationInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand, sessionID, planID string) error {
	if tx == nil || sessionID == "" || planID == "" {
		return ErrInvalidCommand
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	if t.State != StateCancelRequested || t.CancelRequestedAt == nil || len(t.Result) != 0 {
		return ErrInvalidTransition
	}
	intent, err := unstartedCancellationIntent(ctx, tx, t.ID, t.Revision)
	if err != nil {
		return err
	}
	if !intent {
		return ErrInvalidTransition
	}
	var eligible bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_sessions ss JOIN team_assignment_roots a ON a.session_id=ss.id AND a.task_id=ss.task_id AND a.plan_id=ss.accepted_plan_id JOIN task_execution_profiles ep ON ep.task_id=ss.task_id AND ep.team_session_id=ss.id AND ep.execution_mode='team' AND ep.workspace_id=ss.workspace_id WHERE ss.id=? AND ss.task_id=? AND ss.accepted_plan_id=? AND ss.workspace_id=? AND NOT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=ss.task_id) AND NOT EXISTS(SELECT 1 FROM agent_worker_runs WHERE task_id=ss.task_id))`, sessionID, t.ID, planID, t.WorkspaceID).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrInvalidTransition
	}
	now := s.clock.UnixMilli()
	if err = s.repo.Transition(ctx, tx, transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision, From: StateCancelRequested, To: StateCancelled, UpdatedAt: now}); err != nil {
		return err
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"task_id": t.ID, "from": StateCancelRequested, "to": StateCancelled, "revision": t.Revision + 1, "reason": "team_reasoning_settled_no_execution", "session_id": sessionID, "plan_id": planID})
	return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &t.WorkspaceID, Type: "task.cancelled", AggregateType: "task", AggregateID: t.ID, Payload: payload, OccurredAt: now})
}
