package projectworkspace

import (
 "crypto/ed25519"
 "encoding/json"
)

// NodeOperatorExecutionApprovalSigningBytes is the exact versioned message
// authenticated by the human operator's SEPARATE Ed25519 private key.
// The live authenticated HTTP principal is supplied by OnePane's session,
// never selected by the Node. Each server-issued challenge ID is random,
// and optimistic Goal revision prevents key proofs from being transferred
// to a different approval, Node, Workspace or epoch.
//
// This is proof of operator software key possession, NOT proof that the
// OnePane server signed its portable permit or that a physical host ran.
func NodeOperatorExecutionApprovalSigningBytes(
 c ApproveGoalNodeProbeExecutionPermitCommand)([]byte,error){
 if c.ProjectID==""||c.GoalID==""||c.InstrumentKeyID==""||
  c.ChallengeID==""||c.PeerNodeID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||len(c.OperatorPublicKey)!=ed25519.PublicKeySize{
  return nil,ErrInvalidCommand
 }
 return json.Marshal(struct{
  Domain string `json:"domain"`
  ProjectID string `json:"project_id"`
  GoalID string `json:"goal_id"`
  InstrumentKeyID string `json:"instrument_key_id"`
  ChallengeID string `json:"challenge_id"`
  PeerNodeID string `json:"peer_node_id"`
  ActorPrincipalID string `json:"actor_principal_id"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  OperatorPublicKey []byte `json:"operator_public_key"`
 }{
  Domain:"OnePane/human-node-one-shot-approval/ed25519/v1",
  ProjectID:c.ProjectID,GoalID:c.GoalID,InstrumentKeyID:c.InstrumentKeyID,
  ChallengeID:c.ChallengeID,PeerNodeID:c.PeerNodeID,
  ActorPrincipalID:c.ActorPrincipalID,
  ExpectedGoalRevision:c.ExpectedGoalRevision,
  OperatorPublicKey:c.OperatorPublicKey,
 })
}
