package team

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type ExecutionControlCommand = CancelExecutionCommand
type executionScope struct{ workspace, root, state string }
type executionTask struct {
	id, state string
	revision  int64
}

func executionAuthority(ctx context.Context, tx storage.Tx, c ExecutionControlCommand) (executionScope, error) {
	var out executionScope
	var revision int64
	var status string
	if c.SessionID == "" || c.PlanID == "" || c.ExpectedTaskRevision < 1 || len(c.Reason) > 2048 {
		return out, ErrInvalid
	}
	if err := tx.QueryRowContext(ctx, `SELECT s.workspace_id,s.task_id,s.status,t.state,t.revision FROM team_sessions s
	 JOIN team_assignment_roots a ON a.session_id=s.id AND a.plan_id=s.accepted_plan_id AND a.task_id=s.task_id
	 JOIN team_session_manifests m ON m.session_id=s.id AND m.workspace_id=s.workspace_id AND m.execution_mode='team'
	 JOIN tasks t ON t.id=s.task_id AND t.workspace_id=s.workspace_id AND t.archived_at IS NULL
	 WHERE s.id=? AND s.accepted_plan_id=?`, c.SessionID, c.PlanID).Scan(&out.workspace, &out.root, &status, &out.state, &revision); err != nil {
		return out, ErrSessionState
	}
	var human int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p
	 JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active'
	 JOIN workspaces w ON w.id=m.workspace_id AND w.status='active'
	 WHERE p.id=? AND p.principal_type='human' AND p.status='active')`, out.workspace, c.ActorPrincipalID).Scan(&human); err != nil {
		return out, err
	}
	if human != 1 {
		return out, ErrHumanRequired
	}
	if revision != c.ExpectedTaskRevision {
		return out, task.ErrRevisionConflict
	}
	if status != "plan_accepted" && status != "executing" && status != "team_review" {
		return out, ErrSessionState
	}
	return out, nil
}

func executionDescendants(ctx context.Context, tx storage.Tx, scope executionScope) ([]executionTask, error) {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE owned(id) AS (
	 SELECT ? UNION SELECT t.id FROM tasks t JOIN owned o ON t.parent_task_id=o.id LIMIT 257
	) SELECT t.id,t.workspace_id,t.state,t.revision FROM tasks t JOIN owned o ON o.id=t.id LIMIT 257`, scope.root)
	if err != nil {
		return nil, err
	}
	out := []executionTask{}
	for rows.Next() {
		var item executionTask
		var ws string
		if err = rows.Scan(&item.id, &ws, &item.state, &item.revision); err != nil {
			break
		}
		if ws != scope.workspace {
			err = ErrInvalid
			break
		}
		out = append(out, item)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(out) > 256 {
		return nil, ErrInvalid
	}
	return out, nil
}
