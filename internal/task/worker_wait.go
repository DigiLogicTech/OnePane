package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// WorkerWaitInTransaction validates the original running Agent Attempt and
// transitions it, the Task and its audit record in the caller's transaction.
// The matching Worker run is transitioned separately in the SAME transaction.
// This control-plane method must not be exposed as a model-authored tool.
func (s *Service) WorkerWaitInTransaction(
	ctx context.Context, tx storage.Tx, cmd TransitionCommand,
	attemptID, workerPrincipalID string, next State,
) error {
	if s == nil || tx == nil || cmd.ExpectedRevision < 1 ||
		strings.TrimSpace(cmd.TaskID) == "" || strings.TrimSpace(attemptID) == "" ||
		strings.TrimSpace(workerPrincipalID) == "" {
		return fmt.Errorf("%w: exact Worker wait identity required", ErrInvalidCommand)
	}
	eventType := ""
	switch next {
	case StatePaused: eventType = "task.paused"
	case StateWaitingApproval: eventType = "task.waiting_approval"
	default: return fmt.Errorf("%w: unsupported Worker wait state %s", ErrInvalidCommand, next)
	}
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil { return err }
	if t.Revision != cmd.ExpectedRevision { return ErrRevisionConflict }
	if t.State != StateRunning || !CanTransition(t.State, next) {
		return fmt.Errorf("%w: Worker wait requires RUNNING Task, found %s", ErrInvalidTransition, t.State)
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil { return err }
	if active == nil || active.ID != attemptID || active.State != AttemptRunning ||
		active.WorkerPrincipalID == nil || *active.WorkerPrincipalID != workerPrincipalID {
		return ErrInvalidAttempt
	}
	var count int
	if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
		WHERE task_id=? AND status IN ('created','queued','running','waiting')`,t.ID).Scan(&count);err!=nil{return err}
	if count!=1{return ErrInvalidAttempt}
	now:=s.clock.UnixMilli()
	id,err:=s.ids.New("evt")
	if err!=nil{return err}
	if err:=s.repo.TransitionAttempt(ctx,tx,active.ID,AttemptRunning,AttemptWaiting,now);err!=nil{return err}
	if err:=s.repo.Transition(ctx,tx,transitionRecord{
		TaskID:t.ID,ExpectedRevision:t.Revision,From:StateRunning,To:next,UpdatedAt:now,
	});err!=nil{return err}
	payload,_:=json.Marshal(map[string]any{
		"task_id":t.ID,"from":t.State,"to":next,"reason":cmd.Reason,
		"revision":t.Revision+1,
	})
	return s.events.Append(ctx,tx,event.Event{
		ID:id,WorkspaceID:&t.WorkspaceID,Type:eventType,
		AggregateType:"task",AggregateID:t.ID,ActorPrincipalID:cmd.ActorPrincipalID,
		RequestID:cmd.RequestID,TraceID:cmd.TraceID,Payload:payload,OccurredAt:now,
	})
}
