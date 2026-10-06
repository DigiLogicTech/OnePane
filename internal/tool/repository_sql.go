package tool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface{ Scan(...any) error }

const invocationSelect = `
SELECT id,workspace_id,task_id,attempt_id,principal_id,operation_id,
       tool_id,tool_version,adapter_id,adapter_version,mode,resource_ref,input_hash,
       status,summary,result_json,error_code,started_at,ended_at,created_at,updated_at
FROM tool_invocations WHERE id = ?`

func scanInvocation(row rowScanner) (Invocation, error) {
	var inv Invocation
	var taskID, attemptID, operationID, resourceRef, summary, errorCode sql.NullString
	var result []byte
	var startedAt, endedAt sql.NullInt64
	if err := row.Scan(
		&inv.ID, &inv.WorkspaceID, &taskID, &attemptID, &inv.PrincipalID, &operationID,
		&inv.ToolID, &inv.ToolVersion, &inv.AdapterID, &inv.AdapterVersion, &inv.Mode, &resourceRef,
		&inv.InputHash, &inv.Status, &summary, &result, &errorCode, &startedAt, &endedAt,
		&inv.CreatedAt, &inv.UpdatedAt,
	); err != nil {
		return Invocation{}, err
	}
	if taskID.Valid {
		v := taskID.String
		inv.TaskID = &v
	}
	if attemptID.Valid {
		v := attemptID.String
		inv.AttemptID = &v
	}
	if operationID.Valid {
		v := operationID.String
		inv.OperationID = &v
	}
	if resourceRef.Valid {
		v := resourceRef.String
		inv.ResourceRef = &v
	}
	if summary.Valid {
		v := summary.String
		inv.Summary = &v
	}
	if len(result) > 0 {
		inv.Result = append([]byte(nil), result...)
	}
	if errorCode.Valid {
		v := errorCode.String
		inv.ErrorCode = &v
	}
	if startedAt.Valid {
		v := startedAt.Int64
		inv.StartedAt = &v
	}
	if endedAt.Valid {
		v := endedAt.Int64
		inv.EndedAt = &v
	}
	if !ValidStatus(inv.Status) {
		return Invocation{}, fmt.Errorf("stored tool invocation %s has invalid status %q", inv.ID, inv.Status)
	}
	return inv, nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (Invocation, error) {
	return scanInvocation(r.db.QueryRowContext(ctx, invocationSelect, id))
}

func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, inv Invocation) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO tool_invocations(
    id,workspace_id,task_id,attempt_id,principal_id,operation_id,
    tool_id,tool_version,adapter_id,adapter_version,mode,resource_ref,input_hash,
    status,summary,result_json,error_code,started_at,ended_at,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inv.ID, inv.WorkspaceID, inv.TaskID, inv.AttemptID, inv.PrincipalID, inv.OperationID,
		inv.ToolID, inv.ToolVersion, inv.AdapterID, inv.AdapterVersion, inv.Mode, inv.ResourceRef,
		inv.InputHash, inv.Status, inv.Summary, nil, inv.ErrorCode, inv.StartedAt, inv.EndedAt,
		inv.CreatedAt, inv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tool invocation: %w", err)
	}
	return nil
}

func (r *sqlRepository) Transition(ctx context.Context, tx storage.Tx, tr Transition) error {
	if !CanTransition(tr.From, tr.To) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, tr.From, tr.To)
	}
	var result any
	if len(tr.Result) > 0 {
		result = string(tr.Result)
	}
	terminal := tr.To == StatusSucceeded || tr.To == StatusFailed || tr.To == StatusTimedOut || tr.To == StatusCancelled || tr.To == StatusInterrupted
	res, err := tx.ExecContext(ctx, `
UPDATE tool_invocations
SET status = ?,
    summary = CASE WHEN ? IS NULL THEN summary ELSE ? END,
    result_json = CASE WHEN ? IS NULL THEN result_json ELSE ? END,
    error_code = CASE WHEN ? IS NULL THEN error_code ELSE ? END,
    started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN ? ELSE started_at END,
    ended_at = CASE WHEN ? THEN ? ELSE ended_at END,
    updated_at = ?
WHERE id = ? AND status = ?`,
		tr.To,
		tr.Summary, tr.Summary,
		result, result,
		tr.ErrorCode, tr.ErrorCode,
		tr.To, tr.At,
		terminal, tr.At,
		tr.At, tr.InvocationID, tr.From,
	)
	if err != nil {
		return fmt.Errorf("transition tool invocation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidTransition
	}
	return nil
}

func (r *sqlRepository) ValidateExecutionContext(ctx context.Context, workspaceID, principalID string, taskID, attemptID *string) error {
	if attemptID != nil && taskID == nil {
		return ErrAttemptRequiresTask
	}
	if taskID != nil {
		var taskWorkspace string
		if err := r.db.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id = ?`, *taskID).Scan(&taskWorkspace); err != nil {
			return fmt.Errorf("%w: resolve task: %v", ErrExecutionContext, err)
		}
		if taskWorkspace != workspaceID {
			return ErrTaskWorkspaceMismatch
		}
	}
	if attemptID == nil {
		return nil
	}
	var actualTask, state string
	var worker sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT task_id,status,worker_principal_id FROM task_attempts WHERE id = ?`, *attemptID).Scan(&actualTask, &state, &worker)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: attempt not found", ErrExecutionContext)
	}
	if err != nil {
		return fmt.Errorf("%w: resolve attempt: %v", ErrExecutionContext, err)
	}
	if actualTask != *taskID {
		return ErrAttemptTaskMismatch
	}
	if state != "running" {
		return ErrAttemptNotRunning
	}
	if worker.Valid && worker.String != principalID {
		return ErrAttemptPrincipalMismatch
	}
	return nil
}
