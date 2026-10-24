package teamworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func (s *Service) bindReasoningCandidate(ctx context.Context, r Result, ws string, round int64, kind, candidateID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE team_turn_requests SET selected_candidate_kind=?,selected_candidate_id=?,updated_at=?
		WHERE id=? AND workspace_id=? AND session_id=? AND member_id=? AND round_number=? AND status='running' AND response_message_id IS NULL AND completed_at IS NULL`,
		kind, candidateID, s.clock.UnixMilli(), r.TurnID, ws, r.SessionID, r.MemberID, round)
	if err != nil {
		return fmt.Errorf("persist Team candidate binding: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Team candidate binding lost turn ownership")
	}
	return nil
}

func (s *Service) bindReasoningReservation(ctx context.Context, r Result, ws string, round int64, reservationID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE team_turn_requests SET budget_reservation_id=?,updated_at=?
		WHERE id=? AND workspace_id=? AND session_id=? AND member_id=? AND round_number=? AND status='running' AND response_message_id IS NULL AND completed_at IS NULL AND budget_reservation_id IS NULL`,
		reservationID, s.clock.UnixMilli(), r.TurnID, ws, r.SessionID, r.MemberID, round)
	if err != nil {
		return fmt.Errorf("persist Team reservation binding: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Team reservation binding lost turn ownership")
	}
	return nil
}

// An uncertain deliberation has no Task Attempt to interrupt. Retain the exact
// claimed turn as running, which is never selected by Tick or Research retry,
// and append its reconciliation record atomically. A restart is not permission
// to refund potentially spent quota or repeat the seat's external invocation.
// No Task state, Attempt, response message or successful contribution is created.
func (s *Service) reconcileReasoningTurn(ctx context.Context, r Result, ws string, round int64, cause error) Result {
	r.Status = "unknown"
	r.Error = cause.Error()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		changed, err := tx.ExecContext(ctx, `UPDATE team_turn_requests SET error_text=?,updated_at=?
			WHERE id=? AND workspace_id=? AND session_id=? AND member_id=? AND round_number=?
			AND status='running' AND response_message_id IS NULL AND completed_at IS NULL`,
			bound("operator reconciliation required: "+cause.Error(), 2048), s.clock.UnixMilli(), r.TurnID, ws, r.SessionID, r.MemberID, round)
		if err != nil {
			return err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("Team reconciliation lost original turn ownership")
		}
		id, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"turn_id": r.TurnID, "session_id": r.SessionID, "member_id": r.MemberID, "round": round, "action": "operator_reconciliation_required", "automatic_retry": false})
		actor := WorkerPrincipal
		return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &ws, Type: "team.reasoning_reconciliation_required", AggregateType: "team_turn_request", AggregateID: r.TurnID, ActorPrincipalID: &actor, Payload: payload, OccurredAt: s.clock.UnixMilli()})
	})
	if err != nil {
		r.Error = errors.Join(cause, fmt.Errorf("persist Team reconciliation: %w", err)).Error()
	}
	return r
}
