package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface{ Scan(...any) error }

const leaseSelect = `
SELECT id,workspace_id,principal_id,task_id,capability_id,scope_json,status,
       issued_by,issued_at,expires_at,usage_limit,usage_count,revision
FROM capability_leases WHERE id = ?`

func scanLease(row rowScanner) (Lease, error) {
	var l Lease
	var taskID sql.NullString
	var usageLimit sql.NullInt64
	var scopeRaw string
	if err := row.Scan(
		&l.ID, &l.WorkspaceID, &l.PrincipalID, &taskID, &l.CapabilityID, &scopeRaw, &l.Status,
		&l.IssuedBy, &l.IssuedAt, &l.ExpiresAt, &usageLimit, &l.UsageCount, &l.Revision,
	); err != nil {
		return Lease{}, err
	}
	if taskID.Valid {
		l.TaskID = &taskID.String
	}
	if usageLimit.Valid {
		v := usageLimit.Int64
		l.UsageLimit = &v
	}
	if err := json.Unmarshal([]byte(scopeRaw), &l.Scope); err != nil {
		return Lease{}, fmt.Errorf("decode capability lease scope: %w", err)
	}
	if err := l.Scope.Validate(); err != nil {
		return Lease{}, fmt.Errorf("stored capability lease %s: %w", l.ID, err)
	}
	if !ValidStatus(l.Status) {
		return Lease{}, fmt.Errorf("stored capability lease %s has invalid status %q", l.ID, l.Status)
	}
	return l, nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (Lease, error) {
	return scanLease(r.db.QueryRowContext(ctx, leaseSelect, id))
}

func (r *sqlRepository) GetForUpdate(ctx context.Context, tx storage.Tx, id string) (Lease, error) {
	return scanLease(tx.QueryRowContext(ctx, leaseSelect, id))
}

func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, l Lease) error {
	scope, err := l.Scope.JSON()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO capability_leases(
    id,workspace_id,principal_id,task_id,capability_id,scope_json,status,
    issued_by,issued_at,expires_at,usage_limit,usage_count,revision
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.WorkspaceID, l.PrincipalID, l.TaskID, l.CapabilityID, string(scope), l.Status,
		l.IssuedBy, l.IssuedAt, l.ExpiresAt, l.UsageLimit, l.UsageCount, l.Revision,
	)
	if err != nil {
		return fmt.Errorf("insert capability lease: %w", err)
	}
	return nil
}

func (r *sqlRepository) Transition(ctx context.Context, tx storage.Tx, id string, expectedRevision int64, from, to Status) error {
	res, err := tx.ExecContext(ctx, `
UPDATE capability_leases
SET status = ?, revision = revision + 1
WHERE id = ? AND revision = ? AND status = ?`, to, id, expectedRevision, from)
	if err != nil {
		return fmt.Errorf("transition capability lease: %w", err)
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

func (r *sqlRepository) ConsumeAt(ctx context.Context, tx storage.Tx, l Lease, uses int64, next Status, now int64) error {
	res, err := tx.ExecContext(ctx, `
UPDATE capability_leases
SET usage_count = usage_count + ?, status = ?, revision = revision + 1
WHERE id = ? AND revision = ? AND status = 'active'
  AND expires_at > ?
  AND (usage_limit IS NULL OR usage_count + ? <= usage_limit)`,
		uses, next, l.ID, l.Revision, now, uses)
	if err != nil {
		return fmt.Errorf("consume capability lease: %w", err)
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

func scanSubject(row rowScanner) (Subject, error) {
	var s Subject
	var membership sql.NullString
	if err := row.Scan(&s.PrincipalType, &s.PrincipalStatus, &s.WorkspaceStatus, &membership); err != nil {
		return Subject{}, err
	}
	if membership.Valid {
		s.MembershipStatus = &membership.String
	}
	return s, nil
}

const subjectSelect = `
SELECT p.principal_type,p.status,w.status,wm.status
FROM principals p
JOIN workspaces w ON w.id = ?
LEFT JOIN workspace_memberships wm ON wm.workspace_id = w.id AND wm.principal_id = p.id
WHERE p.id = ?`

func (r *sqlRepository) Subject(ctx context.Context, workspaceID, principalID string) (Subject, error) {
	return scanSubject(r.db.QueryRowContext(ctx, subjectSelect, workspaceID, principalID))
}

func (r *sqlRepository) SubjectTx(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (Subject, error) {
	return scanSubject(tx.QueryRowContext(ctx, subjectSelect, workspaceID, principalID))
}

func (r *sqlRepository) TaskWorkspace(ctx context.Context, taskID string) (string, error) {
	var workspaceID string
	if err := r.db.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id = ?`, taskID).Scan(&workspaceID); err != nil {
		return "", err
	}
	return workspaceID, nil
}

func (r *sqlRepository) TaskWorkspaceTx(ctx context.Context, tx storage.Tx, taskID string) (string, error) {
	var workspaceID string
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id = ?`, taskID).Scan(&workspaceID); err != nil {
		return "", err
	}
	return workspaceID, nil
}
