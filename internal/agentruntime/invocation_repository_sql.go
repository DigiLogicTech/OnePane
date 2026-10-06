package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlInvocationRepository struct{ db *sql.DB }

func newSQLInvocationRepository(db *sql.DB) invocationRepository {
	return &sqlInvocationRepository{db: db}
}

type invScanner interface{ Scan(...any) error }

const invocationSelect = `SELECT id,workspace_id,task_id,attempt_id,principal_id,connection_id,status,request_json,response_artifact_id,error_code,revision,created_at,updated_at,completed_at FROM agent_runtime_invocations WHERE id=?`

func scanInvocation(row invScanner) (Invocation, error) {
	var v Invocation
	var task, attempt, response, errorCode sql.NullString
	var completed sql.NullInt64
	var req string
	if err := row.Scan(&v.ID, &v.WorkspaceID, &task, &attempt, &v.PrincipalID, &v.ConnectionID, &v.Status, &req, &response, &errorCode, &v.Revision, &v.CreatedAt, &v.UpdatedAt, &completed); err != nil {
		return Invocation{}, err
	}
	if task.Valid {
		x := task.String
		v.TaskID = &x
	}
	if attempt.Valid {
		x := attempt.String
		v.AttemptID = &x
	}
	if response.Valid {
		x := response.String
		v.ResponseArtifactID = &x
	}
	if errorCode.Valid {
		x := errorCode.String
		v.ErrorCode = &x
	}
	if completed.Valid {
		x := completed.Int64
		v.CompletedAt = &x
	}
	v.RequestJSON = json.RawMessage(req)
	if !ValidInvocationStatus(v.Status) {
		return Invocation{}, fmt.Errorf("stored agent runtime invocation %s has invalid status %q", v.ID, v.Status)
	}
	return v, nil
}
func (r *sqlInvocationRepository) Get(ctx context.Context, id string) (Invocation, error) {
	return scanInvocation(r.db.QueryRowContext(ctx, invocationSelect, id))
}
func (r *sqlInvocationRepository) GetTx(ctx context.Context, tx storage.Tx, id string) (Invocation, error) {
	return scanInvocation(tx.QueryRowContext(ctx, invocationSelect, id))
}
func (r *sqlInvocationRepository) Insert(ctx context.Context, tx storage.Tx, v Invocation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_runtime_invocations(id,workspace_id,task_id,attempt_id,principal_id,connection_id,status,request_json,response_artifact_id,error_code,revision,created_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.TaskID, v.AttemptID, v.PrincipalID, v.ConnectionID, v.Status, string(v.RequestJSON), v.ResponseArtifactID, v.ErrorCode, v.Revision, v.CreatedAt, v.UpdatedAt, v.CompletedAt)
	if err != nil {
		return fmt.Errorf("insert agent runtime invocation: %w", err)
	}
	return nil
}
func (r *sqlInvocationRepository) Transition(ctx context.Context, tx storage.Tx, v Invocation, to InvocationStatus, responseArtifact, errorCode *string, completedAt *int64, now int64) error {
	if !CanInvocationTransition(v.Status, to) {
		return ErrInvalidTransition
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_runtime_invocations SET status=?,response_artifact_id=COALESCE(?,response_artifact_id),error_code=?,revision=revision+1,updated_at=?,completed_at=? WHERE id=? AND revision=? AND status=?`, to, responseArtifact, errorCode, now, completedAt, v.ID, v.Revision, v.Status)
	if err != nil {
		return fmt.Errorf("transition agent runtime invocation: %w", err)
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
func (r *sqlInvocationRepository) PrincipalEligibleTx(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (bool, error) {
	var typ, status string
	var membership sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT p.principal_type,p.status,wm.status FROM principals p LEFT JOIN workspace_memberships wm ON wm.workspace_id=? AND wm.principal_id=p.id WHERE p.id=?`, workspaceID, principalID).Scan(&typ, &status, &membership)
	if err != nil {
		return false, err
	}
	if status != "active" {
		return false, nil
	}
	if typ == "system" || typ == "recovery" || typ == "watchdog" {
		return true, nil
	}
	return membership.Valid && membership.String == "active", nil
}
func (r *sqlInvocationRepository) TaskWorkspaceTx(ctx context.Context, tx storage.Tx, taskID string) (string, error) {
	var ws string
	err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id=?`, taskID).Scan(&ws)
	return ws, err
}
func (r *sqlInvocationRepository) AttemptTaskTx(ctx context.Context, tx storage.Tx, attemptID string) (string, error) {
	var task string
	err := tx.QueryRowContext(ctx, `SELECT task_id FROM task_attempts WHERE id=?`, attemptID).Scan(&task)
	return task, err
}
