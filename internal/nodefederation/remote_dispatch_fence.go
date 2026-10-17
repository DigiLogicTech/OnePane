package nodefederation

import (
 "context"
 "database/sql"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/inference"
)

// ErrFederatedDispatchNotAdmitted means no remote inference payload was
// transmitted. Retry requires a fresh Node manifest and a new admission;
// another model must never be silently substituted.
var ErrFederatedDispatchNotAdmitted=errors.New("remote Node model is not admitted for dispatch")

// authorizeRemoteModelDispatch rechecks the resolved *local* deployment
// against current Node trust, persisted mTLS pairing and still-valid
// capability manifest immediately before opening the network transport.
// The immutable remote deployment ID must exist in the current manifest.
// Also recheck an explicitly disabled remote compute policy at the very last
// pre-network boundary. An omitted policy retains the existing legacy
// default; an explicitly revoked one cannot be bypassed by direct transport.
// No model data, raw prompts, credentials or endpoint details are returned.
// Legacy/local call sites without a Task are intentionally unable to use a
// selected-Projects compute policy: such authority cannot be inferred from
// deployment routing metadata.
func(s *Service) authorizeRemoteModelDispatch(
 ctx context.Context,peerID,localDeploymentID,remoteDeploymentID string,
)error{
 // Historical internal snapshot helper: no export, no network authority.
 return s.authorizeRemoteModelDispatchSnapshot(ctx,peerID,
  localDeploymentID,remoteDeploymentID,nil,"")
}

// requirePersistedInferenceDispatch ensures this exact inference request
// is still executing and belongs to its claimed Task and deployment.
func(s *Service) requirePersistedInferenceDispatch(ctx context.Context,
 req inference.DispatchRequest)error{
 if s==nil||s.db==nil||req.TaskID==nil||*req.TaskID==""||
  req.RequestID==""||req.WorkspaceID==""||req.Deployment.ID==""||
  len(req.RequestJSON)==0{return ErrFederatedDispatchNotAdmitted}
 var present int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM inference_requests i
  JOIN tasks t ON t.id=i.task_id AND t.workspace_id=i.workspace_id
  WHERE i.id=? AND i.task_id=? AND i.workspace_id=?
   AND i.deployment_id=? AND i.provider_connection_id IS NULL
   AND i.status='executing' AND i.request_json=?`,
  req.RequestID,*req.TaskID,req.WorkspaceID,req.Deployment.ID,
  string(req.RequestJSON)).Scan(&present)
 if err!=nil||present!=1{return ErrFederatedDispatchNotAdmitted}
 return nil
}

// Actual remote network dispatch MUST pass both persisted Project and
// canonical Workspace transfer grants; model/config metadata alone cannot
// grant off-host inference or select a different tenant Task.
func(s *Service) authorizeRemoteModelDispatchForTask(
 ctx context.Context,peerID,localDeploymentID,remoteDeploymentID string,
 taskID *string,workspaceID string,
)error{
 if s==nil||s.db==nil||taskID==nil||*taskID==""||workspaceID==""{
  return ErrFederatedDispatchNotAdmitted
 }
 allowed,err:=(featurepolicy.StoredScopes{DB:s.db}).AuthorizeNodeTransfer(
  ctx,workspaceID,*taskID,peerID)
 if err!=nil||!allowed{return ErrFederatedDispatchNotAdmitted}
 return s.authorizeRemoteModelDispatchSnapshot(ctx,peerID,
  localDeploymentID,remoteDeploymentID,taskID,workspaceID)
}

// Internal snapshot tests do not transmit prompts. Only the authorising
// adapter above can enter a remote transport with this result.
func(s *Service) authorizeRemoteModelDispatchSnapshot(
 ctx context.Context,peerID,localDeploymentID,remoteDeploymentID string,
 taskID *string,workspaceID string,
)error{
 if s==nil||s.db==nil||s.clock==nil||
  peerID==""||localDeploymentID==""||remoteDeploymentID==""{
  return ErrFederatedDispatchNotAdmitted
 }
 var status,trust,nodeTrust,pairStatus,manifestNode,manifestProtocol string
 var expiresAt int64
 var localConfirmed,peerConfirmed,remoteNodeLocal,computeAllowed,projectAllowed int
 var claimedRemoteID,claimedPeerID string
 var listed int
 err:=s.db.QueryRowContext(ctx,`SELECT
  d.status,m.trust_state,h.trust_state,p.status,
  cm.expires_at,p.local_confirmed,p.peer_confirmed,h.local,
  COALESCE(policy.enabled,1),
  CASE WHEN policy.node_id IS NULL OR policy.project_scope='all' THEN 1
   WHEN policy.project_scope='selected' AND
    json_type(policy.allowed_projects_json)='array' AND
    EXISTS (SELECT 1 FROM tasks t
     JOIN json_each(policy.allowed_projects_json) ap
      ON ap.value=t.project_id AND ap.type='text'
     JOIN projects project ON project.id=t.project_id
      AND project.status='active'
     WHERE t.id=? AND t.workspace_id=? AND t.project_id IS NOT NULL)
    THEN 1 ELSE 0 END,
  COALESCE(json_extract(d.runtime_config_json,'$.federation.remote_deployment_id'),''),
  COALESCE(json_extract(d.runtime_config_json,'$.federation.peer_node_id'),''),
  COALESCE(json_extract(cm.manifest_json,'$.node_id'),''),
  COALESCE(json_extract(cm.manifest_json,'$.protocol'),''),
  EXISTS(SELECT 1 FROM json_each(cm.manifest_json,'$.models') AS advertised
   WHERE json_extract(advertised.value,'$.deployment_id')=?)
  FROM model_deployments d
  JOIN models m ON m.id=d.model_id
  JOIN harness_nodes h ON h.id=d.node_id
  JOIN node_pairings p ON p.peer_node_id=h.id
  JOIN node_capability_manifests cm ON cm.peer_node_id=h.id
  LEFT JOIN remote_node_compute_policies policy ON policy.node_id=h.id
  WHERE d.id=? AND d.node_id=? AND d.provider_connection_id IS NULL`,
  taskID,workspaceID,remoteDeploymentID,localDeploymentID,peerID).
  Scan(&status,&trust,&nodeTrust,&pairStatus,&expiresAt,
   &localConfirmed,&peerConfirmed,&remoteNodeLocal,&computeAllowed,&projectAllowed,
   &claimedRemoteID,&claimedPeerID,&manifestNode,&manifestProtocol,&listed)
 if errors.Is(err,sql.ErrNoRows){return ErrFederatedDispatchNotAdmitted}
 if err!=nil{return err}
 if (status!="ready"&&status!="degraded")||
  (trust!="trusted"&&trust!="user_trusted")||
  nodeTrust!="paired"||pairStatus!="paired"||
  localConfirmed!=1||peerConfirmed!=1||remoteNodeLocal!=0||
  computeAllowed!=1||projectAllowed!=1||expiresAt<=s.clock.UnixMilli()||
  claimedRemoteID!=remoteDeploymentID||
  claimedPeerID!=peerID||
  manifestNode!=peerID||manifestProtocol!=ProtocolVersion||listed!=1{
  return ErrFederatedDispatchNotAdmitted
 }
 return nil
}
