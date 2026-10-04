package inference

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type rowScanner interface{ Scan(...any) error }

func scanProvider(row rowScanner) (ProviderConnection, error) {
	var p ProviderConnection
	var workspace, secret sql.NullString
	var retry sql.NullInt64
	var raw string
	if err := row.Scan(&p.ID, &workspace, &p.Provider, &p.DisplayName, &p.AuthType, &secret,
		&p.Status, &raw, &retry, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return ProviderConnection{}, err
	}
	if workspace.Valid {
		v := workspace.String
		p.WorkspaceID = &v
	}
	if secret.Valid {
		v := secret.String
		p.SecretRef = &v
	}
	if retry.Valid {
		v := retry.Int64
		p.RetryAfter = &v
	}
	p.ConnectionJSON = json.RawMessage(raw)
	if !ValidProviderStatus(p.Status) {
		return ProviderConnection{}, fmt.Errorf("stored provider %s has invalid status %q", p.ID, p.Status)
	}
	return p, nil
}

const providerSelect = `
SELECT id,workspace_id,provider,display_name,auth_type,secret_ref,status,connection_json,
       retry_after,revision,created_at,updated_at
FROM provider_connections WHERE id=?`

func (r *sqlRepository) Provider(ctx context.Context, id string) (ProviderConnection, error) {
	return scanProvider(r.db.QueryRowContext(ctx, providerSelect, id))
}

func (r *sqlRepository) Providers(ctx context.Context, workspaceID *string) ([]ProviderConnection, error) {
	query := `SELECT id,workspace_id,provider,display_name,auth_type,secret_ref,status,connection_json,
       retry_after,revision,created_at,updated_at
FROM provider_connections`
	args := []any{}
	if workspaceID != nil && strings.TrimSpace(*workspaceID) != "" {
		query += ` WHERE workspace_id=?`
		args = append(args, strings.TrimSpace(*workspaceID))
	}
	query += ` ORDER BY display_name COLLATE NOCASE, created_at`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list provider connections: %w", err)
	}
	defer rows.Close()
	out := make([]ProviderConnection, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (r *sqlRepository) ProviderTx(ctx context.Context, tx storage.Tx, id string) (ProviderConnection, error) {
	return scanProvider(tx.QueryRowContext(ctx, providerSelect, id))
}
func (r *sqlRepository) InsertProvider(ctx context.Context, tx storage.Tx, p ProviderConnection) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO provider_connections(
        id,workspace_id,provider,display_name,auth_type,secret_ref,status,connection_json,retry_after,
        revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.WorkspaceID, p.Provider, p.DisplayName, p.AuthType, p.SecretRef, p.Status, string(p.ConnectionJSON), p.RetryAfter,
		p.Revision, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert provider connection: %w", err)
	}
	return nil
}
func (r *sqlRepository) UpdateProviderStatus(ctx context.Context, tx storage.Tx, p ProviderConnection, status ProviderStatus, retryAfter *int64, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE provider_connections
        SET status=?,retry_after=?,revision=revision+1,updated_at=?
        WHERE id=? AND revision=?`, status, retryAfter, now, p.ID, p.Revision)
	if err != nil {
		return fmt.Errorf("update provider connection: %w", err)
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

func scanModel(row rowScanner) (Model, error) {
	var m Model
	var provider, arch, rev, hash, quant sql.NullString
	var modalities, metadata string
	if err := row.Scan(&m.ID, &provider, &m.ModelRef, &arch, &rev, &hash, &quant, &modalities, &metadata, &m.TrustState, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return Model{}, err
	}
	if provider.Valid {
		v := provider.String
		m.ProviderName = &v
	}
	if arch.Valid {
		v := arch.String
		m.Architecture = &v
	}
	if rev.Valid {
		v := rev.String
		m.RevisionRef = &v
	}
	if hash.Valid {
		v := hash.String
		m.WeightsHash = &v
	}
	if quant.Valid {
		v := quant.String
		m.Quantization = &v
	}
	m.ModalitiesJSON = json.RawMessage(modalities)
	m.StaticMetadataJSON = json.RawMessage(metadata)
	if !ValidModelTrustState(m.TrustState) {
		return Model{}, fmt.Errorf("stored model %s has invalid trust state %q", m.ID, m.TrustState)
	}
	return m, nil
}

const modelSelect = `SELECT id,provider_name,model_ref,architecture,revision_ref,weights_hash,quantization,
modalities_json,static_metadata_json,trust_state,created_at,updated_at FROM models WHERE id=?`

func (r *sqlRepository) Model(ctx context.Context, id string) (Model, error) {
	return scanModel(r.db.QueryRowContext(ctx, modelSelect, id))
}
func (r *sqlRepository) ModelTx(ctx context.Context, tx storage.Tx, id string) (Model, error) {
	return scanModel(tx.QueryRowContext(ctx, modelSelect, id))
}
func (r *sqlRepository) InsertModel(ctx context.Context, tx storage.Tx, m Model) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO models(id,provider_name,model_ref,architecture,revision_ref,weights_hash,quantization,
        modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ProviderName, m.ModelRef, m.Architecture, m.RevisionRef, m.WeightsHash, m.Quantization, string(m.ModalitiesJSON), string(m.StaticMetadataJSON), m.TrustState, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert model: %w", err)
	}
	return nil
}

func (r *sqlRepository) UpdateModelTrust(ctx context.Context, tx storage.Tx, m Model, trust ModelTrustState, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE models SET trust_state=?,updated_at=? WHERE id=? AND trust_state=?`, trust, now, m.ID, m.TrustState)
	if err != nil {
		return fmt.Errorf("update model trust: %w", err)
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

func scanDeployment(row rowScanner) (ModelDeployment, error) {
	var d ModelDeployment
	var node, provider, rn, rv, residency sql.NullString
	var config string
	var reported, verified sql.NullInt64
	if err := row.Scan(&d.ID, &d.ModelID, &node, &provider, &rn, &rv, &config, &d.Status, &residency, &reported, &verified, &d.DeploymentFingerprint, &d.Revision, &d.DiscoveredAt, &d.UpdatedAt); err != nil {
		return ModelDeployment{}, err
	}
	if node.Valid {
		v := node.String
		d.NodeID = &v
	}
	if provider.Valid {
		v := provider.String
		d.ProviderConnectionID = &v
	}
	if rn.Valid {
		v := rn.String
		d.RuntimeName = &v
	}
	if rv.Valid {
		v := rv.String
		d.RuntimeVersion = &v
	}
	if residency.Valid {
		v := ResidencyState(residency.String)
		if !ValidResidencyState(v) {
			return ModelDeployment{}, fmt.Errorf("stored deployment %s has invalid residency %q", d.ID, v)
		}
		d.ResidencyState = &v
	}
	if reported.Valid {
		v := reported.Int64
		d.ContextMaxReported = &v
	}
	if verified.Valid {
		v := verified.Int64
		d.ContextMaxVerified = &v
	}
	d.RuntimeConfigJSON = json.RawMessage(config)
	if !ValidDeploymentStatus(d.Status) {
		return ModelDeployment{}, fmt.Errorf("stored deployment %s has invalid status %q", d.ID, d.Status)
	}
	return d, nil
}

const deploymentSelect = `SELECT id,model_id,node_id,provider_connection_id,runtime_name,runtime_version,runtime_config_json,status,
residency_state,context_max_reported,context_max_verified,deployment_fingerprint,revision,discovered_at,updated_at
FROM model_deployments WHERE id=?`

func (r *sqlRepository) Deployment(ctx context.Context, id string) (ModelDeployment, error) {
	return scanDeployment(r.db.QueryRowContext(ctx, deploymentSelect, id))
}
func (r *sqlRepository) DeploymentTx(ctx context.Context, tx storage.Tx, id string) (ModelDeployment, error) {
	return scanDeployment(tx.QueryRowContext(ctx, deploymentSelect, id))
}
func (r *sqlRepository) InsertDeployment(ctx context.Context, tx storage.Tx, d ModelDeployment) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO model_deployments(id,model_id,node_id,provider_connection_id,runtime_name,runtime_version,
        runtime_config_json,status,residency_state,context_max_reported,context_max_verified,deployment_fingerprint,revision,discovered_at,updated_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.ModelID, d.NodeID, d.ProviderConnectionID, d.RuntimeName, d.RuntimeVersion, string(d.RuntimeConfigJSON), d.Status, d.ResidencyState, d.ContextMaxReported, d.ContextMaxVerified, d.DeploymentFingerprint, d.Revision, d.DiscoveredAt, d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert model deployment: %w", err)
	}
	return nil
}
func (r *sqlRepository) UpdateDeploymentStatus(ctx context.Context, tx storage.Tx, d ModelDeployment, status DeploymentStatus, residency *ResidencyState, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE model_deployments SET status=?,residency_state=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, status, residency, now, d.ID, d.Revision)
	if err != nil {
		return fmt.Errorf("update model deployment: %w", err)
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
func (r *sqlRepository) WorkspaceStatusTx(ctx context.Context, tx storage.Tx, id string) (string, error) {
	var s string
	err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, id).Scan(&s)
	return s, err
}
func (r *sqlRepository) NodeExistsTx(ctx context.Context, tx storage.Tx, id string) (bool, error) {
	var x int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM harness_nodes WHERE id=?`, id).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (r *sqlRepository) NodeTrust(ctx context.Context, id string) (string, error) {
	var state string
	err := r.db.QueryRowContext(ctx, `SELECT trust_state FROM harness_nodes WHERE id=?`, id).Scan(&state)
	return state, err
}
