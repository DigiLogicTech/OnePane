package agentruntime

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

const selectConnection = `SELECT id,workspace_id,node_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,
auth_type,secret_ref,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,
revision,created_at,updated_at FROM agent_runtime_connections WHERE id=?`

func scanConnection(row rowScanner) (Connection, error) {
	var c Connection
	var workspace, node, secret sql.NullString
	var endpoint, protocol, caps, data string
	if err := row.Scan(&c.ID, &workspace, &node, &c.RuntimeKind, &c.DisplayName, &c.AdapterName, &c.AdapterVersion, &endpoint, &c.AuthType, &secret, &c.Status, &c.TrustState, &c.OperatingMode, &protocol, &caps, &data, &c.Revision, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Connection{}, err
	}
	if workspace.Valid {
		v := workspace.String
		c.WorkspaceID = &v
	}
	if node.Valid {
		v := node.String
		c.NodeID = &v
	}
	if secret.Valid {
		v := secret.String
		c.SecretRef = &v
	}
	c.EndpointJSON = json.RawMessage(endpoint)
	c.ProtocolJSON = json.RawMessage(protocol)
	c.CapabilitiesJSON = json.RawMessage(caps)
	c.DataPolicyJSON = json.RawMessage(data)
	if !ValidStatus(c.Status) || !ValidTrustState(c.TrustState) || !ValidOperatingMode(c.OperatingMode) {
		return Connection{}, fmt.Errorf("stored agent runtime %s has invalid enum state", c.ID)
	}
	return c, nil
}
func (r *sqlRepository) Get(ctx context.Context, id string) (Connection, error) {
	return scanConnection(r.db.QueryRowContext(ctx, selectConnection, id))
}
func (r *sqlRepository) GetTx(ctx context.Context, tx storage.Tx, id string) (Connection, error) {
	return scanConnection(tx.QueryRowContext(ctx, selectConnection, id))
}
func (r *sqlRepository) Insert(ctx context.Context, tx storage.Tx, c Connection) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_runtime_connections(id,workspace_id,node_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,secret_ref,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.WorkspaceID, c.NodeID, c.RuntimeKind, c.DisplayName, c.AdapterName, c.AdapterVersion, string(c.EndpointJSON), c.AuthType, c.SecretRef, c.Status, c.TrustState, c.OperatingMode, string(c.ProtocolJSON), string(c.CapabilitiesJSON), string(c.DataPolicyJSON), c.Revision, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert agent runtime connection: %w", err)
	}
	return nil
}
func (r *sqlRepository) UpdateStatus(ctx context.Context, tx storage.Tx, c Connection, status Status, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE agent_runtime_connections SET status=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, status, now, c.ID, c.Revision)
	if err != nil {
		return fmt.Errorf("update agent runtime status: %w", err)
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

func (r *sqlRepository) NodeTrustTx(ctx context.Context, tx storage.Tx, id string) (string, bool, error) {
	var trust string
	var local int
	err := tx.QueryRowContext(ctx, `SELECT trust_state,local FROM harness_nodes WHERE id=?`, id).Scan(&trust, &local)
	return trust, local == 1, err
}
