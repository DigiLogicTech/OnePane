package observation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface{ Scan(...any) error }

const observationFields = `id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,
source_principal_id,adapter_id,adapter_version,value_json,confidentiality,residency,trust,origin_node_id,
integrity_hash,observed_at,created_at`

func scanObservation(row rowScanner) (Observation, error) {
	var o Observation
	var source, adapterID, adapterVersion, origin sql.NullString
	var value, confidentiality, residency, trust string
	if err := row.Scan(
		&o.ID, &o.WorkspaceID, &o.SubjectRef, &o.ObservationType, &o.ProbeToolID, &o.ProbeToolVersion,
		&source, &adapterID, &adapterVersion, &value, &confidentiality, &residency, &trust, &origin,
		&o.IntegrityHash, &o.ObservedAt, &o.CreatedAt,
	); err != nil {
		return Observation{}, err
	}
	if source.Valid {
		o.SourcePrincipalID = &source.String
	}
	if adapterID.Valid {
		o.AdapterID = &adapterID.String
	}
	if adapterVersion.Valid {
		o.AdapterVersion = &adapterVersion.String
	}
	var originPtr *string
	if origin.Valid {
		originPtr = &origin.String
	}
	label, err := policy.DataLabelFromStorage(o.WorkspaceID, confidentiality, residency, trust, originPtr)
	if err != nil {
		return Observation{}, fmt.Errorf("decode observation label: %w", err)
	}
	o.Label = label
	o.Value = json.RawMessage(value)
	return o, nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (Observation, error) {
	return scanObservation(r.db.QueryRowContext(ctx, `SELECT `+observationFields+` FROM observations WHERE id = ?`, id))
}

func (r *sqlRepository) Latest(ctx context.Context, workspaceID, subjectRef, observationType string) (Observation, error) {
	query := `SELECT ` + observationFields + ` FROM observations WHERE workspace_id = ? AND subject_ref = ?`
	args := []any{workspaceID, subjectRef}
	if observationType != "" {
		query += ` AND observation_type = ?`
		args = append(args, observationType)
	}
	query += ` ORDER BY observed_at DESC, created_at DESC, id DESC LIMIT 1`
	return scanObservation(r.db.QueryRowContext(ctx, query, args...))
}

func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, o Observation) error {
	conf, residency, trust, err := policy.StorageLabel(o.Label)
	if err != nil {
		return err
	}
	var origin any
	if o.Label.OriginNodeID != "" {
		origin = o.Label.OriginNodeID
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO observations(
    id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,
    source_principal_id,adapter_id,adapter_version,value_json,confidentiality,residency,trust,
    origin_node_id,integrity_hash,observed_at,created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.WorkspaceID, o.SubjectRef, o.ObservationType, o.ProbeToolID, o.ProbeToolVersion,
		o.SourcePrincipalID, o.AdapterID, o.AdapterVersion, string(o.Value), conf, residency, trust,
		origin, o.IntegrityHash, o.ObservedAt, o.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

func (r *sqlRepository) WorkspaceStatus(ctx context.Context, tx storage.Tx, workspaceID string) (string, error) {
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id = ?`, workspaceID).Scan(&status); err != nil {
		return "", err
	}
	return status, nil
}

func (r *sqlRepository) SourceEligible(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (bool, error) {
	var principalType, principalStatus string
	var membership sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT p.principal_type,p.status,wm.status
FROM principals p
LEFT JOIN workspace_memberships wm ON wm.workspace_id = ? AND wm.principal_id = p.id
WHERE p.id = ?`, workspaceID, principalID).Scan(&principalType, &principalStatus, &membership)
	if err != nil {
		return false, err
	}
	if principalStatus != "active" {
		return false, nil
	}
	switch principalType {
	case "system", "recovery", "watchdog":
		return true, nil
	case "human", "agent", "service":
		return membership.Valid && membership.String == "active", nil
	default:
		return false, nil
	}
}
