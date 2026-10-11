package nodefederation

import (
 "context"
 "database/sql"
 "errors"
)

// ErrFederatedDispatchNotAdmitted means no remote inference payload was
// transmitted. Retry requires a fresh Node manifest and a new admission;
// another model must never be silently substituted.
var ErrFederatedDispatchNotAdmitted=errors.New("remote Node model is not admitted for dispatch")

// authorizeRemoteModelDispatch rechecks the resolved *local* deployment
// against current Node trust, persisted mTLS pairing and still-valid
// capability manifest immediately before opening the network transport.
// The immutable remote deployment ID must exist in the current manifest.
// No model data, raw prompts, credentials or endpoint details are returned.
func(s *Service) authorizeRemoteModelDispatch(
 ctx context.Context,peerID,localDeploymentID,remoteDeploymentID string,
)error{
 if s==nil||s.db==nil||s.clock==nil||
  peerID==""||localDeploymentID==""||remoteDeploymentID==""{
  return ErrFederatedDispatchNotAdmitted
 }
 var status,trust,nodeTrust,pairStatus,manifestNode,manifestProtocol string
 var expiresAt int64
 var claimedRemoteID,claimedPeerID string
 var listed int
 err:=s.db.QueryRowContext(ctx,`SELECT
  d.status,m.trust_state,h.trust_state,p.status,
  cm.expires_at,
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
  WHERE d.id=? AND d.node_id=? AND d.provider_connection_id IS NULL`,
  remoteDeploymentID,localDeploymentID,peerID).
  Scan(&status,&trust,&nodeTrust,&pairStatus,&expiresAt,
   &claimedRemoteID,&claimedPeerID,&manifestNode,&manifestProtocol,&listed)
 if errors.Is(err,sql.ErrNoRows){return ErrFederatedDispatchNotAdmitted}
 if err!=nil{return err}
 if (status!="ready"&&status!="degraded")||
  (trust!="trusted"&&trust!="user_trusted")||
  nodeTrust!="paired"||pairStatus!="paired"||
  expiresAt<=s.clock.UnixMilli()||
  claimedRemoteID!=remoteDeploymentID||
  claimedPeerID!=peerID||
  manifestNode!=peerID||manifestProtocol!=ProtocolVersion||listed!=1{
  return ErrFederatedDispatchNotAdmitted
 }
 return nil
}
