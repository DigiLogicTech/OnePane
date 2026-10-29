package teamgate

import (
	"context"
	"errors"
)

var ErrPaused = errors.New("Team execution is paused")

func Paused(ctx context.Context, q Queryer, sessionID string) (bool, error) {
	var installed int
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='team_execution_holds')`).Scan(&installed); err != nil {
		return false, err
	}
	if installed == 0 {
		return false, nil
	}
	var active int
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_execution_holds WHERE session_id=? AND released_at IS NULL)`, sessionID).Scan(&active)
	return active == 1, err
}

// Include delegated descendants even when they use an ordinary Task profile.
func TaskPaused(ctx context.Context, q Queryer, workspaceID, taskID string) (bool, error) {
	var installed int
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='team_execution_holds')`).Scan(&installed); err != nil {
		return false, err
	}
	if installed == 0 {
		return false, nil
	}
	var active, count, foreign int
	// Match the execution-control scope bound. UNION terminates corrupt cycles;
	// a truncated or cross-Workspace ancestry must never authorize dispatch.
	err := q.QueryRowContext(ctx, `WITH RECURSIVE ancestry(id,parent,workspace) AS (
	 SELECT id,parent_task_id,workspace_id FROM tasks WHERE id=? AND workspace_id=?
	 UNION SELECT t.id,t.parent_task_id,t.workspace_id FROM tasks t JOIN ancestry a ON t.id=a.parent LIMIT 257
	) SELECT EXISTS(SELECT 1 FROM ancestry a JOIN team_execution_holds h ON h.root_task_id=a.id AND h.released_at IS NULL),
	 COUNT(*),COALESCE(SUM(CASE WHEN workspace<>? THEN 1 ELSE 0 END),0) FROM ancestry`, taskID, workspaceID, workspaceID).Scan(&active, &count, &foreign)
	if err != nil {
		return false, err
	}
	if count > 256 || foreign != 0 {
		return false, ErrExecution
	}
	return active == 1, nil
}
