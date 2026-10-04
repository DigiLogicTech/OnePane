package operation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

const operationSelect = `
SELECT id, workspace_id, task_id, attempt_id, principal_id, compensates_operation_id,
       idempotency_key, state, capability_id, resource_ref, tool_id, tool_version,
       adapter_id, adapter_version, desired_state_json, precondition_json,
       reconciliation_json, compensation_json, policy_revision, input_hash,
       capability_lease_id, capability_lease_revision, resource_lease_id, global_resource_lease_id,
       required_verification, required_approval, revision, created_at, updated_at
FROM operations`

func scanOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var o Operation
	var taskID, attemptID, compensates, leaseID, resourceLeaseID, globalResourceLeaseID sql.NullString
	var compensation sql.NullString
	var leaseRevision sql.NullInt64
	var requiredVerification, requiredApproval sql.NullString
	var state string
	err := row.Scan(&o.ID, &o.WorkspaceID, &taskID, &attemptID, &o.PrincipalID, &compensates,
		&o.IdempotencyKey, &state, &o.CapabilityID, &o.ResourceRef, &o.ToolID, &o.ToolVersion,
		&o.AdapterID, &o.AdapterVersion, &o.DesiredState, &o.Precondition,
		&o.Reconciliation, &compensation, &o.PolicyRevision, &o.InputHash,
		&leaseID, &leaseRevision, &resourceLeaseID, &globalResourceLeaseID, &requiredVerification, &requiredApproval,
		&o.Revision, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return Operation{}, err
	}
	o.State = State(state)
	if taskID.Valid {
		x := taskID.String
		o.TaskID = &x
	}
	if attemptID.Valid {
		x := attemptID.String
		o.AttemptID = &x
	}
	if compensates.Valid {
		x := compensates.String
		o.CompensatesOperationID = &x
	}
	if compensation.Valid {
		o.Compensation = json.RawMessage(compensation.String)
	}
	if leaseID.Valid {
		x := leaseID.String
		o.CapabilityLeaseID = &x
	}
	if leaseRevision.Valid {
		x := leaseRevision.Int64
		o.CapabilityLeaseRevision = &x
	}
	if resourceLeaseID.Valid {
		x := resourceLeaseID.String
		o.ResourceLeaseID = &x
	}
	if globalResourceLeaseID.Valid {
		x := globalResourceLeaseID.String
		o.GlobalResourceLeaseID = &x
	}
	if requiredVerification.Valid {
		x := policy.VerificationLevel(requiredVerification.String)
		o.RequiredVerification = &x
	}
	if requiredApproval.Valid {
		x := policy.ApprovalLevel(requiredApproval.String)
		o.RequiredApproval = &x
	}
	return o, nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (Operation, error) {
	o, err := scanOperation(r.db.QueryRowContext(ctx, operationSelect+` WHERE id = ?`, id))
	if err != nil {
		return Operation{}, fmt.Errorf("get operation: %w", err)
	}
	return o, nil
}
func (r *sqlRepository) GetTx(ctx context.Context, tx storage.Tx, id string) (Operation, error) {
	o, err := scanOperation(tx.QueryRowContext(ctx, operationSelect+` WHERE id = ?`, id))
	if err != nil {
		return Operation{}, fmt.Errorf("get operation tx: %w", err)
	}
	return o, nil
}
func (r *sqlRepository) ByIdempotency(ctx context.Context, workspaceID, key string) (Operation, error) {
	o, err := scanOperation(r.db.QueryRowContext(ctx, operationSelect+` WHERE workspace_id = ? AND idempotency_key = ?`, workspaceID, key))
	if err != nil {
		return Operation{}, err
	}
	return o, nil
}

func (r *sqlRepository) ListByStates(ctx context.Context, states ...State) ([]Operation, error) {
	if len(states) == 0 {
		return []Operation{}, nil
	}
	q := operationSelect + ` WHERE state IN (`
	args := make([]any, 0, len(states))
	for i, st := range states {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, string(st))
	}
	q += `) ORDER BY updated_at,id`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, o Operation) error {
	var compensation any
	if len(o.Compensation) > 0 {
		compensation = string(o.Compensation)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO operations(
 id,workspace_id,task_id,attempt_id,principal_id,compensates_operation_id,idempotency_key,state,
 capability_id,resource_ref,tool_id,tool_version,adapter_id,adapter_version,
 desired_state_json,precondition_json,reconciliation_json,compensation_json,policy_revision,input_hash,
 revision,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.WorkspaceID, o.TaskID, o.AttemptID, o.PrincipalID, o.CompensatesOperationID, o.IdempotencyKey, string(o.State),
		o.CapabilityID, o.ResourceRef, o.ToolID, o.ToolVersion, o.AdapterID, o.AdapterVersion,
		string(o.DesiredState), string(o.Precondition), string(o.Reconciliation), compensation, o.PolicyRevision, o.InputHash,
		o.Revision, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert operation: %w", err)
	}
	return nil
}
func (r *sqlRepository) Transition(ctx context.Context, tx storage.Tx, id string, expectedRevision int64, from, to State, at int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE operations SET state=?, revision=revision+1, updated_at=? WHERE id=? AND revision=? AND state=?`, string(to), at, id, expectedRevision, string(from))
	if err != nil {
		return fmt.Errorf("transition operation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
func (r *sqlRepository) SetAuthorization(ctx context.Context, tx storage.Tx, id string, expectedRevision, policyRevision int64, leaseID string, leaseRevision int64, requiredVerification, requiredApproval string, at int64) error {
	res, err := tx.ExecContext(ctx, `
UPDATE operations SET capability_lease_id=?, capability_lease_revision=?, policy_revision=?, required_verification=?, required_approval=?, state='authorized', revision=revision+1, updated_at=?
WHERE id=? AND revision=? AND state='proposed'`, leaseID, leaseRevision, policyRevision, requiredVerification, requiredApproval, at, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("authorize operation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
func (r *sqlRepository) SetResourceLease(ctx context.Context, tx storage.Tx, id string, expectedRevision int64, resourceLeaseID string, globalResourceLeaseID *string, at int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE operations SET resource_lease_id=?,global_resource_lease_id=?, state='prepared', revision=revision+1, updated_at=? WHERE id=? AND revision=? AND state='authorized'`, resourceLeaseID, globalResourceLeaseID, at, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("prepare operation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) AcquireResourceLease(ctx context.Context, tx storage.Tx, l ResourceLease) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO resource_leases(id,workspace_id,task_id,resource_ref,lease_mode,status,acquired_at,expires_at,revision) VALUES(?,?,?,?,?,'active',?,?,1)`,
		l.ID, l.WorkspaceID, l.TaskID, l.ResourceRef, l.LeaseMode, l.AcquiredAt, l.ExpiresAt)
	if err != nil {
		// SQLite's partial unique index is the durable writer-exclusion backstop.
		return fmt.Errorf("%w: %v", ErrResourceBusy, err)
	}
	return nil
}
func scanResourceLease(row interface{ Scan(...any) error }) (ResourceLease, error) {
	var l ResourceLease
	var taskID sql.NullString
	var released sql.NullInt64
	if err := row.Scan(&l.ID, &l.WorkspaceID, &taskID, &l.ResourceRef, &l.LeaseMode, &l.Status, &l.AcquiredAt, &l.ExpiresAt, &released, &l.Revision); err != nil {
		return ResourceLease{}, err
	}
	if taskID.Valid {
		x := taskID.String
		l.TaskID = &x
	}
	if released.Valid {
		x := released.Int64
		l.ReleasedAt = &x
	}
	return l, nil
}
func (r *sqlRepository) ResourceLeaseTx(ctx context.Context, tx storage.Tx, id string) (ResourceLease, error) {
	l, err := scanResourceLease(tx.QueryRowContext(ctx, `SELECT id,workspace_id,task_id,resource_ref,lease_mode,status,acquired_at,expires_at,released_at,revision FROM resource_leases WHERE id=?`, id))
	if err != nil {
		return ResourceLease{}, fmt.Errorf("get resource lease: %w", err)
	}
	return l, nil
}
func (r *sqlRepository) ReleaseResourceLease(ctx context.Context, tx storage.Tx, id string, expectedRevision, at int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE resource_leases SET status='released',released_at=?,revision=revision+1 WHERE id=? AND revision=? AND status='active'`, at, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("release resource lease: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrResourceLeaseInvalid
	}
	return nil
}
func (r *sqlRepository) RenewResourceLease(ctx context.Context, tx storage.Tx, id string, expectedRevision, expiresAt int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE resource_leases SET expires_at=?,revision=revision+1 WHERE id=? AND revision=? AND status='active'`, expiresAt, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("renew resource lease: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrResourceLeaseInvalid
	}
	return nil
}

func (r *sqlRepository) ActiveResourceLeaseForResource(ctx context.Context, tx storage.Tx, workspaceID, resourceRef string) (*ResourceLease, error) {
	l, err := scanResourceLease(tx.QueryRowContext(ctx, `SELECT id,workspace_id,task_id,resource_ref,lease_mode,status,acquired_at,expires_at,released_at,revision FROM resource_leases WHERE workspace_id=? AND resource_ref=? AND status='active' AND lease_mode='exclusive_mutation'`, workspaceID, resourceRef))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}
func (r *sqlRepository) HasUnknownOutcomeForResource(ctx context.Context, workspaceID, resourceRef, excludeOperationID string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE workspace_id=? AND resource_ref=? AND id<>? AND state IN ('unknown_outcome','blocked_unknown_outcome')`, workspaceID, resourceRef, excludeOperationID).Scan(&n)
	return n > 0, err
}

// StaleValidCheckpointsForTask conservatively invalidates previously verified
// Task state before an external mutation is dispatched. If dispatch later fails
// before side effects occur, the Task must be verified again rather than
// silently reusing evidence that predates the attempted mutation boundary.
func (r *sqlRepository) StaleValidCheckpointsForTask(ctx context.Context, tx storage.Tx, taskID string, at int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM checkpoints WHERE task_id=? AND status='valid' ORDER BY created_at,id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list valid checkpoints: %w", err)
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return ids, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE checkpoints SET status='stale',invalidated_at=? WHERE task_id=? AND status='valid'`, at, taskID)
	if err != nil {
		return nil, fmt.Errorf("stale task checkpoints: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != int64(len(ids)) {
		return nil, fmt.Errorf("stale task checkpoints: expected %d rows, changed %d", len(ids), n)
	}
	return ids, nil
}
func (r *sqlRepository) InsertReceipt(ctx context.Context, tx storage.Tx, rc Receipt) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO operation_receipts(id,operation_id,receipt_type,external_request_id,receipt_json,integrity_hash,created_at) VALUES(?,?,?,?,?,?,?)`, rc.ID, rc.OperationID, rc.ReceiptType, rc.ExternalRequestID, string(rc.Receipt), rc.IntegrityHash, rc.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert operation receipt: %w", err)
	}
	return nil
}
func (r *sqlRepository) VerificationForOperation(ctx context.Context, tx storage.Tx, operationID, verificationID string) (verificationRecord, error) {
	var v verificationRecord
	var opID, achieved sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,operation_id,required_level,achieved_level,status FROM verifications WHERE id=?`, verificationID).Scan(&v.ID, &opID, &v.RequiredLevel, &achieved, &v.Status)
	if err != nil {
		return verificationRecord{}, fmt.Errorf("get operation verification: %w", err)
	}
	if opID.Valid {
		x := opID.String
		v.OperationID = &x
	}
	if achieved.Valid {
		x := achieved.String
		v.AchievedLevel = &x
	}
	if v.OperationID == nil || *v.OperationID != operationID {
		return verificationRecord{}, ErrVerificationRequired
	}
	return v, nil
}
