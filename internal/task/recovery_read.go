package task

import (
	"context"
	"database/sql"
)

// RecoveryCaseMetadata is an intake observation, never a verification or an
// authorization to retry. Effect payloads and private interruption text are not
// exposed. Callers must independently authorize Task reads in this Workspace.
type RecoveryCaseMetadata struct {
	ID                    string `json:"id"`
	TaskID                string `json:"task_id"`
	AttemptID             string `json:"attempt_id"`
	Phase                 string `json:"phase"`
	TaskRevisionAtCapture int64  `json:"task_revision_at_capture"`
	CreatedAt             int64  `json:"created_at"`
}
type RecoveryCasePage struct {
	Cases     []RecoveryCaseMetadata `json:"cases"`
	NextAfter string                 `json:"next_after,omitempty"`
}

func (s *Service) ListRecoveryCases(ctx context.Context, workspace, taskID, after string) (RecoveryCasePage, error) {
	page := RecoveryCasePage{Cases: []RecoveryCaseMetadata{}}
	if workspace == "" || taskID == "" || len(after) > 160 {
		return page, ErrInvalidCommand
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.task_id,c.attempt_id,c.phase,c.task_revision_at_capture,c.created_at
 FROM task_recovery_cases c JOIN tasks t ON t.id=c.task_id AND t.workspace_id=c.workspace_id
 JOIN task_attempts a ON a.id=c.attempt_id AND a.task_id=c.task_id
 WHERE c.workspace_id=? AND c.task_id=? AND c.id>? ORDER BY c.id LIMIT 33`, workspace, taskID, after)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var c RecoveryCaseMetadata
		if err = rows.Scan(&c.ID, &c.TaskID, &c.AttemptID, &c.Phase, &c.TaskRevisionAtCapture, &c.CreatedAt); err != nil {
			return page, err
		}
		page.Cases = append(page.Cases, c)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Cases) > 32 {
		page.Cases = page.Cases[:32]
		page.NextAfter = page.Cases[31].ID
	}
	return page, nil
}

func (s *Service) GetRecoveryCase(ctx context.Context, workspace, taskID, caseID string) (RecoveryCaseMetadata, error) {
	var c RecoveryCaseMetadata
	if workspace == "" || taskID == "" || caseID == "" || len(caseID) > 160 {
		return c, sql.ErrNoRows
	}
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.task_id,c.attempt_id,c.phase,c.task_revision_at_capture,c.created_at FROM task_recovery_cases c JOIN tasks t ON t.id=c.task_id AND t.workspace_id=c.workspace_id JOIN task_attempts a ON a.id=c.attempt_id AND a.task_id=c.task_id WHERE c.id=? AND c.task_id=? AND c.workspace_id=?`, caseID, taskID, workspace).Scan(&c.ID, &c.TaskID, &c.AttemptID, &c.Phase, &c.TaskRevisionAtCapture, &c.CreatedAt)
	return c, err
}
