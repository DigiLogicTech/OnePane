package teamworker

import (
	"context"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

// A failed claim conveys no ownership. Observe only; never retire a turn
// which another worker may already be executing.
func (s *Service) observeReasoningTurn(ctx context.Context, r Result, cause error) Result {
	r.Error = cause.Error()
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM team_turn_requests WHERE id=? AND session_id=? AND member_id=?`, r.TurnID, r.SessionID, r.MemberID).Scan(&r.Status); err != nil {
		r.Status = "unknown"
		r.Error = fmt.Sprintf("%s; read current turn outcome: %v", cause, err)
	}
	return r
}

func (s *Service) finishReasoningError(ctx context.Context, r Result, status string, cause error) Result {
	now := s.clock.UnixMilli()
	r.Error = cause.Error()
	result, err := s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status=?,error_text=?,updated_at=?,completed_at=? WHERE id=? AND status='running' AND response_message_id IS NULL`, status, bound(cause.Error(), 2048), now, now, r.TurnID)
	if err == nil {
		var n int64
		n, err = result.RowsAffected()
		if err == nil && n == 1 {
			r.Status = status
			return r
		}
	}
	if err != nil {
		r.Status = "unknown"
		r.Error = fmt.Sprintf("%s; persist turn outcome: %v", cause, err)
		return r
	}
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM team_turn_requests WHERE id=?`, r.TurnID).Scan(&r.Status); err != nil {
		r.Status = "unknown"
		r.Error = fmt.Sprintf("%s; read current turn outcome: %v", cause, err)
	}
	return r
}

func (s *Service) claimReasoningTurn(ctx context.Context, ws, sessionID, turnID, memberID string, round int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status='running',updated_at=? WHERE id=? AND workspace_id=? AND session_id=? AND member_id=? AND round_number=? AND status='pending'
 AND EXISTS(SELECT 1 FROM team_sessions ss JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id WHERE ss.id=team_turn_requests.session_id AND ss.workspace_id=team_turn_requests.workspace_id AND ss.status='deliberating' AND ss.round_number=team_turn_requests.round_number AND t.archived_at IS NULL AND t.state IN ('created','ready','running','waiting_dependency','waiting_approval','paused','completion_requested','verifying'))`, s.clock.UnixMilli(), turnID, ws, sessionID, memberID, round)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return team.ErrSessionState
	}
	return nil
}

// This is the last worker-level fence before dispatch. Inference/runtime
// services retain their own immediate pre-transport authority checks.
func (s *Service) requireRunningReasoningTurn(ctx context.Context, ws, sessionID, turnID, memberID string, round int64) error {
	var eligible bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_turn_requests tr JOIN team_sessions ss ON ss.id=tr.session_id AND ss.workspace_id=tr.workspace_id JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id WHERE tr.id=? AND tr.workspace_id=? AND tr.session_id=? AND tr.member_id=? AND tr.round_number=? AND tr.status='running' AND tr.response_message_id IS NULL AND ss.status='deliberating' AND ss.round_number=tr.round_number AND t.archived_at IS NULL AND t.state IN ('created','ready','running','waiting_dependency','waiting_approval','paused','completion_requested','verifying'))`, turnID, ws, sessionID, memberID, round).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return team.ErrSessionState
	}
	return nil
}
