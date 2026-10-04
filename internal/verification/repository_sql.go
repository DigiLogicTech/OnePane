package verification

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface{ Scan(...any) error }

const verificationSelect = `SELECT id,workspace_id,task_id,operation_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision FROM verifications WHERE id=?`

func scanVerification(row rowScanner) (Verification, error) {
	var v Verification
	var taskID, operationID, achieved, result, verifiedBy sql.NullString
	var completed sql.NullInt64
	if err := row.Scan(&v.ID, &v.WorkspaceID, &taskID, &operationID, &v.SubjectRef, &v.RequiredLevel, &achieved, &v.Status, &v.Spec, &result, &verifiedBy, &v.StartedAt, &completed, &v.Revision); err != nil {
		return Verification{}, err
	}
	if taskID.Valid {
		x := taskID.String
		v.TaskID = &x
	}
	if operationID.Valid {
		x := operationID.String
		v.OperationID = &x
	}
	if achieved.Valid {
		x := policy.VerificationLevel(achieved.String)
		v.AchievedLevel = &x
	}
	if result.Valid {
		v.Result = []byte(result.String)
	}
	if verifiedBy.Valid {
		x := verifiedBy.String
		v.VerifiedBy = &x
	}
	if completed.Valid {
		x := completed.Int64
		v.CompletedAt = &x
	}
	if !ValidStatus(v.Status) || !ValidLevel(v.RequiredLevel) || (v.AchievedLevel != nil && !ValidLevel(*v.AchievedLevel)) {
		return Verification{}, fmt.Errorf("stored verification %s is invalid", v.ID)
	}
	return v, nil
}
func (r *sqlRepository) GetVerification(ctx context.Context, id string) (Verification, error) {
	return scanVerification(r.db.QueryRowContext(ctx, verificationSelect, id))
}
func (r *sqlRepository) GetVerificationTx(ctx context.Context, tx storage.Tx, id string) (Verification, error) {
	return scanVerification(tx.QueryRowContext(ctx, verificationSelect, id))
}
func (r *sqlRepository) InsertVerification(ctx context.Context, tx storage.Tx, v Verification) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO verifications(id,workspace_id,task_id,operation_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.TaskID, v.OperationID, v.SubjectRef, v.RequiredLevel, nil, v.Status, string(v.Spec), nil, nil, v.StartedAt, nil, v.Revision)
	if err != nil {
		return fmt.Errorf("insert verification: %w", err)
	}
	return nil
}
func (r *sqlRepository) ResolveVerification(ctx context.Context, tx storage.Tx, id string, expected int64, from, to Status, achieved *string, completed *int64, result any, verifiedBy any) error {
	res, err := tx.ExecContext(ctx, `UPDATE verifications SET status=?,achieved_level=?,result_json=?,verified_by=?,completed_at=?,revision=revision+1 WHERE id=? AND revision=? AND status=?`, to, achieved, result, verifiedBy, completed, id, expected, from)
	if err != nil {
		return fmt.Errorf("resolve verification: %w", err)
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
func (r *sqlRepository) TaskWorkspaceTx(ctx context.Context, tx storage.Tx, id string) (string, error) {
	var ws string
	err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id=?`, id).Scan(&ws)
	return ws, err
}
func (r *sqlRepository) OperationWorkspaceTx(ctx context.Context, tx storage.Tx, id string) (string, error) {
	var ws string
	err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM operations WHERE id=?`, id).Scan(&ws)
	return ws, err
}
func (r *sqlRepository) VerifierEligibleTx(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (bool, error) {
	var ptype, status string
	var membership sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT p.principal_type,p.status,(SELECT wm.status FROM workspace_memberships wm WHERE wm.workspace_id=? AND wm.principal_id=p.id) FROM principals p WHERE p.id=?`, workspaceID, principalID).Scan(&ptype, &status, &membership)
	if err != nil {
		return false, err
	}
	if status != "active" {
		return false, nil
	}
	switch ptype {
	case "system", "recovery", "watchdog":
		return true, nil
	case "human", "agent", "service":
		return membership.Valid && membership.String == "active", nil
	default:
		return false, nil
	}
}

const checkpointSelect = `SELECT id,workspace_id,task_id,verification_id,state_json,status,created_at,invalidated_at FROM checkpoints WHERE id=?`

func scanCheckpoint(row rowScanner) (Checkpoint, error) {
	var c Checkpoint
	var invalid sql.NullInt64
	if err := row.Scan(&c.ID, &c.WorkspaceID, &c.TaskID, &c.VerificationID, &c.State, &c.Status, &c.CreatedAt, &invalid); err != nil {
		return Checkpoint{}, err
	}
	if invalid.Valid {
		x := invalid.Int64
		c.InvalidatedAt = &x
	}
	return c, nil
}
func (r *sqlRepository) GetCheckpoint(ctx context.Context, id string) (Checkpoint, error) {
	return scanCheckpoint(r.db.QueryRowContext(ctx, checkpointSelect, id))
}
func (r *sqlRepository) LatestValidCheckpoint(ctx context.Context, taskID string) (Checkpoint, error) {
	return scanCheckpoint(r.db.QueryRowContext(ctx, `SELECT id,workspace_id,task_id,verification_id,state_json,status,created_at,invalidated_at FROM checkpoints WHERE task_id=? AND status='valid' ORDER BY created_at DESC LIMIT 1`, taskID))
}
func (r *sqlRepository) InsertCheckpoint(ctx context.Context, tx storage.Tx, c Checkpoint) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO checkpoints(id,workspace_id,task_id,verification_id,state_json,status,created_at,invalidated_at) VALUES(?,?,?,?,?,?,?,?)`, c.ID, c.WorkspaceID, c.TaskID, c.VerificationID, string(c.State), c.Status, c.CreatedAt, nil)
	if err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}
	return nil
}
func (r *sqlRepository) SupersedeValidCheckpoints(ctx context.Context, tx storage.Tx, taskID, exceptID string, at int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM checkpoints WHERE task_id=? AND id<>? AND status='valid' ORDER BY created_at`, taskID, exceptID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE checkpoints SET status='superseded',invalidated_at=? WHERE task_id=? AND id<>? AND status='valid'`, at, taskID, exceptID); err != nil {
		return nil, err
	}
	return ids, nil
}
func (r *sqlRepository) InvalidateCheckpoint(ctx context.Context, tx storage.Tx, id string, from, to CheckpointStatus, at int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE checkpoints SET status=?,invalidated_at=? WHERE id=? AND status=?`, to, at, id, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCheckpointInvalid
	}
	return nil
}
func (r *sqlRepository) TaskHasUnknownMutationTx(ctx context.Context, tx storage.Tx, taskID string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE task_id=? AND state IN ('unknown_outcome','blocked_unknown_outcome')`, taskID).Scan(&n)
	return n > 0, err
}
