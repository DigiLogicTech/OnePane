package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct {
	db *sql.DB
}

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface {
	Scan(...any) error
}

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var project, projectWorkspace, artifactSession, plan, parent sql.NullString
	var completion, result sql.NullString
	var readyAt, cancelAt, archivedAt sql.NullInt64
	if err := row.Scan(
		&t.ID, &t.WorkspaceID, &project, &projectWorkspace, &artifactSession, &plan, &parent,
		&t.Objective, &t.State, &t.SchedulingClass, &t.Priority,
		&completion, &result, &t.Revision, &readyAt, &cancelAt, &archivedAt,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return Task{}, err
	}
	if completion.Valid && strings.TrimSpace(completion.String) != "" {
		raw := json.RawMessage(completion.String)
		if !json.Valid(raw) {
			return Task{}, fmt.Errorf("task %s completion_json contains invalid JSON", t.ID)
		}
		t.Completion = append(json.RawMessage(nil), raw...)
	}
	if result.Valid && strings.TrimSpace(result.String) != "" {
		raw := json.RawMessage(result.String)
		if !json.Valid(raw) {
			return Task{}, fmt.Errorf("task %s result_json contains invalid JSON", t.ID)
		}
		t.Result = append(json.RawMessage(nil), raw...)
	}
	if project.Valid {
		t.ProjectID = &project.String
	}
	if projectWorkspace.Valid {
		t.ProjectWorkspaceID = &projectWorkspace.String
	}
	if artifactSession.Valid {
		t.ArtifactSessionID = &artifactSession.String
	}
	if plan.Valid {
		t.PlanID = &plan.String
	}
	if parent.Valid {
		t.ParentTaskID = &parent.String
	}
	if readyAt.Valid {
		v := readyAt.Int64
		t.ReadyAt = &v
	}
	if cancelAt.Valid {
		v := cancelAt.Int64
		t.CancelRequestedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Int64
		t.ArchivedAt = &v
	}
	return t, nil
}

const taskSelect = `
SELECT id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
       objective,state,scheduling_class,priority,completion_json,result_json,
       revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
FROM tasks WHERE id = ?`

func (r *sqlRepository) Get(ctx context.Context, id string) (Task, error) {
	t, err := scanTask(r.db.QueryRowContext(ctx, taskSelect, id))
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

func (r *sqlRepository) List(ctx context.Context, workspaceID string, limit int) ([]Task, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
       objective,state,scheduling_class,priority,completion_json,result_json,
       revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
FROM tasks WHERE workspace_id=? AND archived_at IS NULL ORDER BY updated_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjectWorkspace searches *within* one canonical Workspace before the
// limit is applied. Scanning an arbitrary top-N tenancy Task list and filtering
// in JavaScript can silently hide real jobs in active multi-Project installs.
func (r *sqlRepository) ListProjectWorkspace(ctx context.Context, tenancyID, projectID, projectWorkspaceID string, limit int) ([]Task,error) {
 if limit<1||limit>500{limit=100}
 rows,err:=r.db.QueryContext(ctx,`SELECT
 id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
 objective,state,scheduling_class,priority,completion_json,result_json,
 revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
 FROM tasks
 WHERE workspace_id=? AND project_id=? AND project_workspace_id=? AND archived_at IS NULL
 ORDER BY updated_at DESC,id DESC LIMIT ?`,tenancyID,projectID,projectWorkspaceID,limit)
 if err!=nil{return nil,fmt.Errorf("list scoped Workspace tasks: %w",err)}
 defer rows.Close()
 out:=make([]Task,0)
 for rows.Next(){
  t,err:=scanTask(rows)
  if err!=nil{return nil,err}
  out=append(out,t)
 }
 return out,rows.Err()
}

func (r *sqlRepository) ListArchived(ctx context.Context, workspaceID string, limit int) ([]Task, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
       objective,state,scheduling_class,priority,completion_json,result_json,
       revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
FROM tasks WHERE workspace_id=? AND archived_at IS NOT NULL ORDER BY archived_at DESC,updated_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list archived tasks: %w", err)
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sqlRepository) GetForUpdate(ctx context.Context, tx storage.Tx, id string) (Task, error) {
	t, err := scanTask(tx.QueryRowContext(ctx, taskSelect, id))
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, t Task) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO tasks(
    id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
    objective,state,scheduling_class,priority,completion_json,result_json,
    revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.WorkspaceID, t.ProjectID, t.ProjectWorkspaceID, t.ArtifactSessionID, t.PlanID, t.ParentTaskID,
		t.Objective, t.State, t.SchedulingClass, t.Priority, string(t.Completion), nil,
		t.Revision, t.ReadyAt, t.CancelRequestedAt, t.ArchivedAt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (r *sqlRepository) SetArchived(ctx context.Context, tx storage.Tx, taskID string, expectedRevision int64, archivedAt *int64, updatedAt int64) error {
	res, err := tx.ExecContext(ctx, `
UPDATE tasks
SET archived_at = ?,
    revision = revision + 1,
    updated_at = ?
WHERE id = ? AND revision = ?`, archivedAt, updatedAt, taskID, expectedRevision)
	if err != nil {
		return fmt.Errorf("set task archive state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) Transition(ctx context.Context, tx storage.Tx, tr transitionRecord) error {
	res, err := tx.ExecContext(ctx, `
UPDATE tasks
SET state = ?,
    revision = revision + 1,
    ready_at = CASE WHEN ? IS NULL THEN ready_at ELSE ? END,
    cancel_requested_at = CASE WHEN ? IS NULL THEN cancel_requested_at ELSE ? END,
    result_json = CASE WHEN ? IS NULL THEN result_json ELSE ? END,
    updated_at = ?
WHERE id = ? AND revision = ? AND state = ?`,
		tr.To,
		tr.ReadyAt, tr.ReadyAt,
		tr.CancelRequestedAt, tr.CancelRequestedAt,
		tr.Result, tr.Result,
		tr.UpdatedAt,
		tr.TaskID, tr.ExpectedRevision, tr.From,
	)
	if err != nil {
		return fmt.Errorf("transition task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) NextAttemptNumber(ctx context.Context, tx storage.Tx, taskID string) (int64, error) {
	var next int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt_number),0)+1 FROM task_attempts WHERE task_id = ?`, taskID,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("next task attempt number: %w", err)
	}
	return next, nil
}

func (r *sqlRepository) InsertAttempt(ctx context.Context, tx storage.Tx, a Attempt) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO task_attempts(
    id,task_id,attempt_number,worker_principal_id,status,recovery_snapshot_id,
    started_at,ended_at,metadata_json
) VALUES(?,?,?,?,?,?,?,?,?)`,
		a.ID, a.TaskID, a.AttemptNumber, a.WorkerPrincipalID, a.State,
		a.RecoverySnapshotID, a.StartedAt, a.EndedAt, string(a.Metadata),
	)
	if err != nil {
		return fmt.Errorf("insert task attempt: %w", err)
	}
	return nil
}

func scanAttempt(row rowScanner) (Attempt, error) {
	var a Attempt
	var worker, recovery, metadata sql.NullString
	var started, ended sql.NullInt64
	if err := row.Scan(
		&a.ID, &a.TaskID, &a.AttemptNumber, &worker, &a.State,
		&recovery, &started, &ended, &metadata,
	); err != nil {
		return Attempt{}, err
	}
	if metadata.Valid && strings.TrimSpace(metadata.String) != "" {
		raw := json.RawMessage(metadata.String)
		if !json.Valid(raw) {
			return Attempt{}, fmt.Errorf("task attempt %s metadata_json contains invalid JSON", a.ID)
		}
		a.Metadata = append(json.RawMessage(nil), raw...)
	}
	if worker.Valid {
		a.WorkerPrincipalID = &worker.String
	}
	if recovery.Valid {
		a.RecoverySnapshotID = &recovery.String
	}
	if started.Valid {
		v := started.Int64
		a.StartedAt = &v
	}
	if ended.Valid {
		v := ended.Int64
		a.EndedAt = &v
	}
	return a, nil
}

// The dependency predicate must use the caller's write transaction, so
// predecessor completion and the new Attempt cannot race.
func (r *sqlRepository) HardDependenciesSatisfied(ctx context.Context,tx storage.Tx,taskID string)(bool,error){
 var blocked int
 err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_dependencies d
  JOIN tasks dep ON dep.id=d.depends_on_task_id
  WHERE d.task_id=? AND d.dependency_type='hard'
  AND (dep.state<>'complete' OR dep.archived_at IS NOT NULL)`,taskID).Scan(&blocked)
 return blocked==0,err
}
func (r *sqlRepository) ActiveAttempt(ctx context.Context, tx storage.Tx, taskID string) (*Attempt, error) {
	a, err := scanAttempt(tx.QueryRowContext(ctx, `
SELECT id,task_id,attempt_number,worker_principal_id,status,recovery_snapshot_id,
       started_at,ended_at,metadata_json
FROM task_attempts
WHERE task_id = ? AND status IN ('created','queued','running','waiting')
ORDER BY attempt_number DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *sqlRepository) TransitionAttempt(ctx context.Context, tx storage.Tx, id string, from, to AttemptState, at int64) error {
	if !CanTransitionAttempt(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidAttempt, from, to)
	}
	var ended any
	if to == AttemptSucceeded || to == AttemptFailed || to == AttemptCancelled || to == AttemptInterrupted {
		ended = at
	}
	res, err := tx.ExecContext(ctx, `
UPDATE task_attempts
SET status = ?,
    started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN ? ELSE started_at END,
    ended_at = CASE WHEN ? IS NULL THEN ended_at ELSE ? END
WHERE id = ? AND status = ?`, to, to, at, ended, ended, id, from)
	if err != nil {
		return fmt.Errorf("transition task attempt: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidAttempt
	}
	return nil
}

func (r *sqlRepository) ValidCompletionEvidence(ctx context.Context, tx storage.Tx, taskID, checkpointID string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM checkpoints c
JOIN verifications v ON v.id = c.verification_id
JOIN tasks t ON t.id = c.task_id
WHERE c.id = ? AND c.task_id = ? AND c.status = 'valid'
  AND c.workspace_id = t.workspace_id
  AND v.workspace_id = t.workspace_id
  AND v.task_id = t.id
  AND v.status = 'pass'
  AND v.achieved_level IS NOT NULL
  AND v.completed_at IS NOT NULL
  AND c.created_at >= v.completed_at
  AND NOT EXISTS (
      SELECT 1 FROM operations o
      WHERE o.task_id = t.id
        AND o.state IN ('unknown_outcome','blocked_unknown_outcome')
  )`, checkpointID, taskID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("validate completion evidence: %w", err)
	}
	return n == 1, nil
}
