package inference

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRequestRepository struct{ db *sql.DB }

func newSQLRequestRepository(db *sql.DB) requestRepository { return &sqlRequestRepository{db: db} }

type reqScanner interface{ Scan(...any) error }

const requestSelect = `SELECT id,workspace_id,task_id,session_id,principal_id,deployment_id,provider_connection_id,
origin_node_id,execution_node_id,status,capability_json,context_manifest_json,classification_json,request_json,
response_artifact_id,usage_json,error_code,created_at,updated_at,completed_at FROM inference_requests WHERE id=?`

func scanRequest(row reqScanner) (InferenceRequest, error) {
	var r InferenceRequest
	var task, session, deployment, provider, origin, execution, response, usage, errorCode sql.NullString
	var completed sql.NullInt64
	var capability, contextManifest, classification, request string
	if err := row.Scan(&r.ID, &r.WorkspaceID, &task, &session, &r.PrincipalID, &deployment, &provider, &origin, &execution, &r.Status, &capability, &contextManifest, &classification, &request, &response, &usage, &errorCode, &r.CreatedAt, &r.UpdatedAt, &completed); err != nil {
		return InferenceRequest{}, err
	}
	if task.Valid {
		v := task.String
		r.TaskID = &v
	}
	if session.Valid {
		v := session.String
		r.SessionID = &v
	}
	if deployment.Valid {
		v := deployment.String
		r.DeploymentID = &v
	}
	if provider.Valid {
		v := provider.String
		r.ProviderConnectionID = &v
	}
	if origin.Valid {
		v := origin.String
		r.OriginNodeID = &v
	}
	if execution.Valid {
		v := execution.String
		r.ExecutionNodeID = &v
	}
	if response.Valid {
		v := response.String
		r.ResponseArtifactID = &v
	}
	if usage.Valid {
		r.UsageJSON = json.RawMessage(usage.String)
	}
	if errorCode.Valid {
		v := errorCode.String
		r.ErrorCode = &v
	}
	if completed.Valid {
		v := completed.Int64
		r.CompletedAt = &v
	}
	r.CapabilityJSON = json.RawMessage(capability)
	r.ContextManifestJSON = json.RawMessage(contextManifest)
	r.ClassificationJSON = json.RawMessage(classification)
	r.RequestJSON = json.RawMessage(request)
	if !ValidRequestStatus(r.Status) {
		return InferenceRequest{}, fmt.Errorf("stored inference request %s has invalid status %q", r.ID, r.Status)
	}
	return r, nil
}
func (r *sqlRequestRepository) Get(ctx context.Context, id string) (InferenceRequest, error) {
	return scanRequest(r.db.QueryRowContext(ctx, requestSelect, id))
}
func (r *sqlRequestRepository) Insert(ctx context.Context, tx storage.Tx, q InferenceRequest) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO inference_requests(id,workspace_id,task_id,session_id,principal_id,deployment_id,provider_connection_id,origin_node_id,execution_node_id,status,capability_json,context_manifest_json,classification_json,request_json,response_artifact_id,usage_json,error_code,created_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, q.ID, q.WorkspaceID, q.TaskID, q.SessionID, q.PrincipalID, q.DeploymentID, q.ProviderConnectionID, q.OriginNodeID, q.ExecutionNodeID, q.Status, string(q.CapabilityJSON), string(q.ContextManifestJSON), string(q.ClassificationJSON), string(q.RequestJSON), q.ResponseArtifactID, nil, q.ErrorCode, q.CreatedAt, q.UpdatedAt, q.CompletedAt)
	if err != nil {
		return fmt.Errorf("insert inference request: %w", err)
	}
	return nil
}
func (r *sqlRequestRepository) Transition(ctx context.Context, tx storage.Tx, id string, from, to RequestStatus, now int64, origin, execution, provider *string, fields jsonFields) error {
	if !CanRequestTransition(from, to) {
		return ErrInvalidTransition
	}
	var usage any
	if len(fields.UsageJSON) > 0 {
		usage = string(fields.UsageJSON)
	}
	res, err := tx.ExecContext(ctx, `UPDATE inference_requests SET status=?,origin_node_id=COALESCE(?,origin_node_id),execution_node_id=COALESCE(?,execution_node_id),provider_connection_id=COALESCE(?,provider_connection_id),response_artifact_id=COALESCE(?,response_artifact_id),usage_json=COALESCE(?,usage_json),error_code=?,updated_at=?,completed_at=? WHERE id=? AND status=?`, to, origin, execution, provider, fields.ResponseArtifactID, usage, fields.ErrorCode, now, fields.CompletedAt, id, from)
	if err != nil {
		return fmt.Errorf("transition inference request: %w", err)
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
func (r *sqlRequestRepository) PrincipalEligibleTx(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (bool, error) {
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
func (r *sqlRequestRepository) TaskWorkspaceTx(ctx context.Context, tx storage.Tx, taskID string) (string, error) {
	var ws string
	err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id=?`, taskID).Scan(&ws)
	return ws, err
}
