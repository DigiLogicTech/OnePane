package team

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/featurepolicy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func (s *Service) PauseExecution(ctx context.Context, c ExecutionControlCommand) (Session, error) {
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		scope, err := executionAuthority(ctx, tx, c)
		if err != nil {
			return err
		}
		var active string
		err = tx.QueryRowContext(ctx, `SELECT id FROM team_execution_holds WHERE session_id=? AND released_at IS NULL`, c.SessionID).Scan(&active)
		if err == nil {
			if scope.state == "paused" {
				return nil
			}
			return ErrSessionState
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if scope.state != "ready" && scope.state != "running" {
			return ErrSessionState
		}
		owned, err := executionDescendants(ctx, tx, scope)
		if err != nil {
			return err
		}
		holdID, err := s.ids.New("teamhold")
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO team_execution_holds(id,session_id,plan_id,root_task_id,created_by,created_at) VALUES(?,?,?,?,?,?)`, holdID, c.SessionID, c.PlanID, scope.root, c.ActorPrincipalID, s.clock.UnixMilli()); err != nil {
			return err
		}
		for _, item := range owned {
			if item.state != "ready" && item.state != "running" {
				continue
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO team_execution_held_tasks(hold_id,task_id,previous_state) VALUES(?,?,?)`, holdID, item.id, item.state); err != nil {
				return err
			}
			if err = s.tasks.PauseInTransaction(ctx, tx, task.TransitionCommand{TaskID: item.id, ExpectedRevision: item.revision, ActorPrincipalID: &c.ActorPrincipalID, Reason: c.Reason}); err != nil {
				return err
			}
		}
		return s.emit(ctx, tx, scope.workspace, "team.execution_paused", "team_session", c.SessionID, &c.ActorPrincipalID, map[string]any{"hold_id": holdID, "task_id": scope.root, "plan_id": c.PlanID})
	})
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, c.SessionID)
}

func (s *Service) ResumeExecution(ctx context.Context, c ExecutionControlCommand) (Session, error) {
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		scope, err := executionAuthority(ctx, tx, c)
		if err != nil {
			return err
		}
		if scope.state != "paused" {
			return ErrSessionState
		}
		var holdID string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM team_execution_holds WHERE session_id=? AND plan_id=? AND root_task_id=? AND released_at IS NULL`, c.SessionID, c.PlanID, scope.root).Scan(&holdID); err != nil {
			return ErrSessionState
		}
		// A running Worker may still own a model response. It must finish or
		// durably park before any original Attempt can be resumed.
		var draining int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_runs r JOIN team_execution_held_tasks h ON h.task_id=r.task_id WHERE h.hold_id=? AND r.status='running'`, holdID).Scan(&draining); err != nil {
			return err
		}
		if draining != 0 {
			return ErrSessionState
		}
		owned, err := executionDescendants(ctx, tx, scope)
		if err != nil {
			return err
		}
		byID := map[string]executionTask{}
		for _, item := range owned {
			byID[item.id] = item
		}
		rows, err := tx.QueryContext(ctx, `SELECT task_id FROM team_execution_held_tasks WHERE hold_id=? ORDER BY CASE WHEN task_id=? THEN 0 ELSE 1 END,task_id`, holdID, scope.root)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
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
		if len(ids) == 0 || len(ids) > 256 {
			return ErrSessionState
		}
		// Releasing the hold and resuming every recorded Task is atomic. Any
		// revoked profile, dependency or canonical scope rolls this all back.
		if _, err = tx.ExecContext(ctx, `UPDATE team_execution_holds SET released_by=?,released_at=? WHERE id=? AND released_at IS NULL`, c.ActorPrincipalID, s.clock.UnixMilli(), holdID); err != nil {
			return err
		}
		for _, id := range ids {
			item, ok := byID[id]
			if !ok || item.state != "paused" {
				return ErrSessionState
			}
			allowed, err := (featurepolicy.StoredScopes{DB: s.db}).AuthorizeTaskTx(ctx, tx, scope.workspace, id, "tasks.autonomous")
			if err != nil {
				return err
			}
			if !allowed {
				return ErrSessionState
			}
			if err = s.tasks.ResumeInTransaction(ctx, tx, task.TransitionCommand{TaskID: id, ExpectedRevision: item.revision, ActorPrincipalID: &c.ActorPrincipalID, Reason: c.Reason}); err != nil {
				return err
			}
		}
		return s.emit(ctx, tx, scope.workspace, "team.execution_resumed", "team_session", c.SessionID, &c.ActorPrincipalID, map[string]any{"hold_id": holdID, "task_id": scope.root, "plan_id": c.PlanID})
	})
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, c.SessionID)
}
