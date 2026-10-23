package team

import (
	"context"
	"database/sql"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type CancelExecutionCommand struct {
	SessionID, PlanID, ActorPrincipalID, Reason string
	ExpectedTaskRevision                        int64
}

// Cancellation preserves the accepted graph and every Attempt/receipt. Only
// cancellation intent is recorded; external/unknown effects need reconciliation.
func (s *Service) RequestExecutionCancellation(ctx context.Context, cmd CancelExecutionCommand) (Session, error) {
	if cmd.SessionID == "" || cmd.PlanID == "" || cmd.ExpectedTaskRevision < 1 || len(cmd.Reason) > 2048 {
		return Session{}, ErrInvalid
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var ws, root, status, state string
		var revision int64
		var project, world, artifact sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT s.workspace_id,s.task_id,s.status,t.state,t.revision,t.project_id,t.project_workspace_id,t.artifact_session_id
		 FROM team_sessions s JOIN team_assignment_roots a ON a.session_id=s.id AND a.plan_id=s.accepted_plan_id AND a.task_id=s.task_id
		 JOIN team_session_manifests m ON m.session_id=s.id AND m.workspace_id=s.workspace_id AND m.execution_mode='team'
		 JOIN tasks t ON t.id=s.task_id AND t.workspace_id=s.workspace_id AND t.archived_at IS NULL
		 WHERE s.id=? AND s.accepted_plan_id=?`, cmd.SessionID, cmd.PlanID).Scan(&ws, &root, &status, &state, &revision, &project, &world, &artifact); err != nil {
			return ErrSessionState
		}
		var human int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p
		 JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active'
		 JOIN workspaces w ON w.id=m.workspace_id AND w.status='active'
		 WHERE p.id=? AND p.principal_type='human' AND p.status='active')`, ws, cmd.ActorPrincipalID).Scan(&human); err != nil {
			return err
		}
		if human != 1 {
			return ErrHumanRequired
		}
		if revision != cmd.ExpectedTaskRevision {
			return task.ErrRevisionConflict
		}
		if status != "plan_accepted" && status != "executing" && status != "team_review" {
			return ErrSessionState
		}
		// An earlier root-only request must still propagate to descendants.
		if state != "cancel_requested" && state != "cancelling" && !task.CanTransition(task.State(state), task.StateCancelRequested) {
			return ErrSessionState
		}
		rows, err := tx.QueryContext(ctx, `WITH RECURSIVE owned(id) AS (
		 SELECT ? UNION SELECT t.id FROM tasks t JOIN owned o ON t.parent_task_id=o.id LIMIT 257
		) SELECT t.id,t.workspace_id,t.state,t.revision,t.project_id,t.project_workspace_id,t.artifact_session_id FROM tasks t JOIN owned o ON o.id=t.id LIMIT 257`, root)
		if err != nil {
			return err
		}
		type ownedTask struct {
			id, state string
			revision  int64
		}
		tasks := []ownedTask{}
		for rows.Next() {
			var item ownedTask
			var taskWorkspace string
			var childProject, childWorld, childArtifact sql.NullString
			if err = rows.Scan(&item.id, &taskWorkspace, &item.state, &item.revision, &childProject, &childWorld, &childArtifact); err != nil {
				break
			}
			if taskWorkspace != ws || childProject != project || childWorld != world || childArtifact != artifact {
				err = ErrInvalid
				break
			}
			tasks = append(tasks, item)
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if len(tasks) > 256 {
			return ErrInvalid
		}
		changed := false
		cancellationRevision := revision
		for _, item := range tasks {
			switch task.State(item.state) {
			case task.StateComplete, task.StateFailed, task.StateCancelled, task.StateCancelRequested, task.StateCancelling, task.StateCompensating:
				continue
			}
			if err = s.tasks.RequestCancelInTransaction(ctx, tx, task.TransitionCommand{TaskID: item.id, ExpectedRevision: item.revision, ActorPrincipalID: &cmd.ActorPrincipalID, Reason: strings.TrimSpace(cmd.Reason)}); err != nil {
				return err
			}
			if item.id == root {
				cancellationRevision = item.revision + 1
			}
			changed = true
		}
		if !changed {
			return nil
		}
		return s.emit(ctx, tx, ws, "team.execution_cancellation_requested", "team_session", cmd.SessionID, &cmd.ActorPrincipalID, map[string]any{"task_id": root, "task_revision": cancellationRevision, "plan_id": cmd.PlanID, "scope": "task_and_descendants"})
	})
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, cmd.SessionID)
}
