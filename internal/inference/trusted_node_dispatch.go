package inference

import (
    "context"
    "fmt"
)

// authorizeTrustedNodeTaskDispatch is the last local-side off-host data
// transfer boundary. Neither a Task's routing JSON nor a stale scheduler
// decision is authority to export the inference request to a peer.
//
// The grant is resolved from the canonical Project AND Workspace on each
// invocation; the Node must still be paired with a fresh manifest and an
// enabled remote compute policy. Model/provider labels are not trust proof.
func (s *Service) authorizeTrustedNodeTaskDispatch(ctx context.Context, cmd ExecuteCommand, d ModelDeployment) error {
    if d.NodeID == nil || *d.NodeID == s.localNodeID { return nil }
    if cmd.TaskID == nil || d.ProviderConnectionID != nil {
        return fmt.Errorf("%w: paired Node transfer requires scoped Task and local runtime", ErrInformationFlow)
    }
    ok, err := s.scopes.AuthorizeNodeTransfer(ctx,cmd.WorkspaceID,*cmd.TaskID,*d.NodeID)
    if err != nil || !ok {
        return fmt.Errorf("%w: trusted Node transfer grant is missing or revoked", ErrInformationFlow)
    }
    if s.scopes.DB == nil { return ErrRemoteNodeUnsupported }
    var count int
    err=s.scopes.DB.QueryRowContext(ctx,`SELECT COUNT(*)
      FROM harness_nodes n
      JOIN node_pairings p ON p.peer_node_id=n.id AND p.status='paired'
      JOIN node_capability_manifests m ON m.peer_node_id=n.id AND m.expires_at>?
      LEFT JOIN remote_node_compute_policies policy ON policy.node_id=n.id
      WHERE n.id=? AND n.local=0 AND n.trust_state='paired'
      AND (policy.node_id IS NULL OR policy.enabled=1)`,s.clock.UnixMilli(),*d.NodeID).Scan(&count)
    if err != nil || count == 0 {
        return fmt.Errorf("%w: paired Node trust or capability manifest unavailable", ErrRemoteNodeUnsupported)
    }
    return nil
}
