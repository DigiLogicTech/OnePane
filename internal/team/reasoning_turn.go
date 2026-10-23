package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// Deliberation may continue while execution is paused, but never after its
// Task has requested cancellation or become terminal/recovery-owned.
func requireReasoningTask(ctx context.Context, tx storage.Tx, taskID, workspace string) error {
	var eligible bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE id=? AND workspace_id=? AND archived_at IS NULL AND state IN ('created','ready','running','waiting_dependency','waiting_approval','paused','completion_requested','verifying'))`, taskID, workspace).Scan(&eligible); err != nil {
		return err
	}
	if !eligible {
		return ErrSessionState
	}
	return nil
}

// CompleteReasoningTurn publishes a trusted worker's response and retires its
// exact running turn atomically. A stale owner cannot leave a transcript entry.
func (s *Service) CompleteReasoningTurn(ctx context.Context, sessionID, turnID, memberID, worker string, round int64, content json.RawMessage) (Message, error) {
	var out Message
	raw, err := canonical(content)
	if err != nil {
		return out, err
	}
	if worker != "system:team-worker" || round < 1 {
		return out, ErrInvalid
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var ss Session
		if err := tx.QueryRowContext(ctx, `SELECT s.id,s.task_id,s.workspace_id,s.team_id FROM team_sessions s JOIN team_turn_requests tr ON tr.session_id=s.id AND tr.workspace_id=s.workspace_id JOIN team_members m ON m.id=tr.member_id AND m.team_id=s.team_id AND m.workspace_id=s.workspace_id JOIN principals p ON p.id=? AND p.principal_type='system' AND p.status='active' JOIN workspace_memberships wm ON wm.principal_id=p.id AND wm.workspace_id=s.workspace_id AND wm.status='active' WHERE s.id=? AND s.status='deliberating' AND s.round_number=? AND tr.id=? AND tr.member_id=? AND tr.round_number=? AND tr.status='running' AND tr.response_message_id IS NULL AND m.member_kind!='human' AND m.status='active' AND EXISTS(SELECT 1 FROM workspaces w WHERE w.id=s.workspace_id AND w.status='active')`, worker, sessionID, round, turnID, memberID, round).Scan(&ss.ID, &ss.TaskID, &ss.WorkspaceID, &ss.TeamID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSessionState
			}
			return err
		}
		if err := requireReasoningTask(ctx, tx, ss.TaskID, ss.WorkspaceID); err != nil {
			return err
		}
		mid, err := s.ids.New("tmsg")
		if err != nil {
			return err
		}
		now := s.clock.UnixMilli()
		if _, err = tx.ExecContext(ctx, `INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) VALUES(?,?,?,?,?,'agent',?,?,?)`, mid, ss.WorkspaceID, sessionID, memberID, worker, string(raw), round, now); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `UPDATE team_turn_requests SET status='succeeded',response_message_id=?,updated_at=?,completed_at=? WHERE id=? AND status='running' AND response_message_id IS NULL`, mid, now, now, turnID)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrSessionState
		}
		if err = s.emit(ctx, tx, ss.WorkspaceID, "team.message_posted", "team_session", sessionID, &worker, map[string]any{"message_id": mid, "kind": "agent", "round": round}); err != nil {
			return err
		}
		out = Message{ID: mid, WorkspaceID: ss.WorkspaceID, SessionID: sessionID, AuthorMemberID: &memberID, AuthorPrincipalID: &worker, Kind: "agent", Content: raw, RoundNumber: round, CreatedAt: now}
		return nil
	})
	return out, err
}
