package chatcommands

import (
	"context"
	"database/sql"
	"time"
)

// SQLStore implements durable chat controls/queue storage using database/sql.
// It intentionally imports no SQLite driver; the OnePane process supplies its
// already-open authoritative database handle.
type SQLStore struct{ DB *sql.DB }

func (s SQLStore) Controls(ctx context.Context, id string) (SessionControls, error) {
	var c SessionControls
	var paused int
	var updated string
	err := s.DB.QueryRowContext(ctx, `SELECT session_id,busy_mode,approval_mode,approval_level,queue_paused,COALESCE(model_override,''),COALESCE(agent_override,''),COALESCE(reasoning_effort,''),updated_at FROM chat_session_controls WHERE session_id=?`, id).Scan(&c.SessionID, &c.BusyMode, &c.ApprovalMode, &c.ApprovalLevel, &paused, &c.ModelOverride, &c.AgentOverride, &c.ReasoningEffort, &updated)
	if err == sql.ErrNoRows {
		return SessionControls{SessionID: id, BusyMode: BusyQueue, ApprovalMode: ApprovalManual, ApprovalLevel: ApprovalMedium, UpdatedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return SessionControls{}, err
	}
	c.QueuePaused = paused != 0
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, nil
}
func (s SQLStore) SaveControls(ctx context.Context, c SessionControls) error {
	now := time.Now().UTC()
	c.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `INSERT INTO chat_session_controls(session_id,busy_mode,approval_mode,approval_level,queue_paused,model_override,agent_override,reasoning_effort,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET busy_mode=excluded.busy_mode,approval_mode=excluded.approval_mode,approval_level=excluded.approval_level,queue_paused=excluded.queue_paused,model_override=excluded.model_override,agent_override=excluded.agent_override,reasoning_effort=excluded.reasoning_effort,updated_at=excluded.updated_at`, c.SessionID, c.BusyMode, c.ApprovalMode, c.ApprovalLevel, boolInt(c.QueuePaused), nullEmpty(c.ModelOverride), nullEmpty(c.AgentOverride), nullEmpty(c.ReasoningEffort), now.Format(time.RFC3339Nano))
	return err
}
func (s SQLStore) Enqueue(ctx context.Context, q QueueItem) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM chat_prompt_queue WHERE session_id=? AND status IN ('pending','delivering')`, q.SessionID).Scan(&max); err != nil {
		return err
	}
	q.Position = 1
	if max.Valid {
		q.Position = max.Int64 + 1
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	q.UpdatedAt = q.CreatedAt
	if q.Status == "" {
		q.Status = QueuePending
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO chat_prompt_queue(id,session_id,position,prompt,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, q.ID, q.SessionID, q.Position, q.Prompt, q.Status, q.CreatedAt.Format(time.RFC3339Nano), q.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s SQLStore) Queue(ctx context.Context, id string) ([]QueueItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,session_id,position,prompt,status,created_at,updated_at FROM chat_prompt_queue WHERE session_id=? AND status IN ('pending','delivering') ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueItem
	for rows.Next() {
		var q QueueItem
		var c, u string
		if err := rows.Scan(&q.ID, &q.SessionID, &q.Position, &q.Prompt, &q.Status, &c, &u); err != nil {
			return nil, err
		}
		q.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		q.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
		out = append(out, q)
	}
	return out, rows.Err()
}
func (s SQLStore) ReplaceQueue(ctx context.Context, id string, items []QueueItem) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE chat_prompt_queue SET status='cancelled',updated_at=? WHERE session_id=? AND status='pending'`, now, id); err != nil {
		return err
	}
	for i, q := range items {
		q.Position = int64(i + 1)
		if _, err = tx.ExecContext(ctx, `UPDATE chat_prompt_queue SET position=?,status='pending',updated_at=? WHERE id=? AND session_id=?`, q.Position, now, q.ID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func nullEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
