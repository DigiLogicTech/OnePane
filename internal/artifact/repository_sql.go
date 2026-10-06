package artifact

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

const artifactSelect = `
SELECT id,workspace_id,project_id,content_hash,media_type,size_bytes,storage_ref,
       confidentiality,residency,trust,origin_node_id,status,metadata_json,created_by,created_at
FROM artifacts WHERE id = ?`

func scanArtifact(row rowScanner) (Artifact, error) {
	var a Artifact
	var project, origin, createdBy sql.NullString
	var confidentiality, residency, trust, metadata string
	if err := row.Scan(
		&a.ID, &a.WorkspaceID, &project, &a.ContentHash, &a.MediaType, &a.SizeBytes, &a.StorageRef,
		&confidentiality, &residency, &trust, &origin, &a.Status, &metadata, &createdBy, &a.CreatedAt,
	); err != nil {
		return Artifact{}, err
	}
	if project.Valid {
		a.ProjectID = &project.String
	}
	if createdBy.Valid {
		a.CreatedBy = &createdBy.String
	}
	var originPtr *string
	if origin.Valid {
		originPtr = &origin.String
	}
	label, err := policy.DataLabelFromStorage(a.WorkspaceID, confidentiality, residency, trust, originPtr)
	if err != nil {
		return Artifact{}, fmt.Errorf("decode artifact label: %w", err)
	}
	a.Label = label
	a.Metadata = json.RawMessage(metadata)
	if !ValidStatus(a.Status) {
		return Artifact{}, fmt.Errorf("stored artifact %s has invalid status %q", a.ID, a.Status)
	}
	return a, nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (Artifact, error) {
	return scanArtifact(r.db.QueryRowContext(ctx, artifactSelect, id))
}

func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, a Artifact) error {
	conf, residency, trust, err := policy.StorageLabel(a.Label)
	if err != nil {
		return err
	}
	var origin any
	if a.Label.OriginNodeID != "" {
		origin = a.Label.OriginNodeID
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO artifacts(
    id,workspace_id,project_id,content_hash,media_type,size_bytes,storage_ref,
    confidentiality,residency,trust,origin_node_id,status,metadata_json,created_by,created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.WorkspaceID, a.ProjectID, a.ContentHash, a.MediaType, a.SizeBytes, a.StorageRef,
		conf, residency, trust, origin, a.Status, string(a.Metadata), a.CreatedBy, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert artifact: %w", err)
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

func (r *sqlRepository) ProjectWorkspace(ctx context.Context, tx storage.Tx, projectID string) (string, error) {
	var workspaceID string
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM projects WHERE id = ?`, projectID).Scan(&workspaceID); err != nil {
		return "", err
	}
	return workspaceID, nil
}

func (r *sqlRepository) PrincipalEligible(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (bool, error) {
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
