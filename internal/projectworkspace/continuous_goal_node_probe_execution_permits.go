package projectworkspace

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// A short-lived operator-granted exact challenge and exact paired-Node
// authority. The JSON is NOT server-signed: transfer must happen over an
// independently authenticated human channel. Not physical attestation.
type GoalNodeProbeExecutionPermit struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 SourceID string `json:"source_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 PeerNodeID string `json:"peer_node_id"`
 PeerTLSFingerprint string `json:"peer_tls_fingerprint"`
 OriginalChallengeSHA256 string `json:"original_challenge_sha256"`
 InstrumentPublicKeySHA256 string `json:"instrument_public_key_sha256"`
 PlanSHA256 string `json:"plan_sha256"`
 PlanGeneration int64 `json:"plan_generation"`
 Status string `json:"status"`
 EffectiveExecutionState string `json:"effective_execution_state,omitempty"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}
type ApproveGoalNodeProbeExecutionPermitCommand struct {
 ProjectID,GoalID,InstrumentKeyID,ChallengeID,PeerNodeID,ActorPrincipalID string
 ExpectedGoalRevision int64
}
type RevokeGoalNodeProbeExecutionPermitCommand struct {
 ProjectID,GoalID,PermitID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedPermitRevision int64
}
const nodeProbePermitColumns="id,project_id,goal_id,project_workspace_id,instrument_key_id,challenge_id,source_id,declared_target_id,tls_pin_id,peer_node_id,peer_tls_fingerprint,original_challenge_sha256,instrument_public_key_sha256,plan_sha256,plan_generation,status,revision,approved_by,approved_at,expires_at,revoked_at"
func scanNodeProbePermit(row scanner)(GoalNodeProbeExecutionPermit,error){
 var p GoalNodeProbeExecutionPermit
 err:=row.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,
  &p.InstrumentKeyID,&p.ChallengeID,&p.SourceID,&p.DeclaredTargetID,
  &p.TLSPinID,&p.PeerNodeID,&p.PeerTLSFingerprint,
  &p.OriginalChallengeSHA256,&p.InstrumentPublicKeySHA256,
  &p.PlanSHA256,&p.PlanGeneration,&p.Status,&p.Revision,&p.ApprovedBy,&p.ApprovedAt,&p.ExpiresAt,&p.RevokedAt)
 return p,err
}
func nodeProbeHash(raw []byte)string{
 h:=sha256.Sum256(raw)
 return "sha256:"+hex.EncodeToString(h[:])
}

// ApproveGoalNodeProbeExecutionPermit is transactional: a human must own the
// active L5 Project/Goal, the issued challenge must still be unused/live,
// and both directions of the peer pairing must currently be confirmed.
func(s *Service)ApproveGoalNodeProbeExecutionPermit(ctx context.Context,
 c ApproveGoalNodeProbeExecutionPermitCommand)(GoalNodeProbeExecutionPermit,error){
 if c.ProjectID==""||c.GoalID==""||c.InstrumentKeyID==""||
  c.ChallengeID==""||c.PeerNodeID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1{return GoalNodeProbeExecutionPermit{},ErrInvalidCommand}
 id,err:=s.ids.New("gnexec");if err!=nil{return GoalNodeProbeExecutionPermit{},err}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalNodeProbeExecutionPermit{},err}
 var out GoalNodeProbeExecutionPermit
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var revision,planGeneration int64
  var status,wsID,planRaw string
  err=tx.QueryRowContext(ctx,
   "SELECT revision,status,project_workspace_id,monitoring_plan_json,plan_generation FROM project_continuous_goals WHERE id=? AND project_id=?",
   c.GoalID,c.ProjectID).Scan(&revision,&status,&wsID,&planRaw,&planGeneration)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"||planGeneration<1{return ErrInvalidTransition}
  monitored,planHash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  key,pin,err:=s.activeInstrumentKeyTx(ctx,tx,c.ProjectID,c.GoalID,c.InstrumentKeyID,now)
  if err!=nil{return err}
  if key.ProjectWorkspaceID!=wsID{return ErrInvalidTransition}
  scope,err:=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,key.DeclaredTargetID,now)
  if err!=nil{return err}
  monitoredSource:=false
  for _,source:=range monitored.SourceIDs{
   if source==scope.Target.SourceID{monitoredSource=true;break}
  }
  if !monitoredSource||scope.Target.SourceID!=scope.Permit.SourceID||
   scope.Target.ProjectWorkspaceID!=wsID||
   scope.Target.PermitID!=pin.PermitID||
   scope.Permit.PlanHash!=key.PlanHash{return ErrInvalidTransition}
  ch:=GoalInstrumentChallenge{ID:c.ChallengeID,KeyID:c.InstrumentKeyID,
   ProjectID:c.ProjectID,GoalID:c.GoalID}
  err=tx.QueryRowContext(ctx,
   "SELECT nonce,signing_payload,issued_at,expires_at,status FROM project_goal_instrument_challenges WHERE id=? AND instrument_key_id=? AND project_id=? AND goal_id=?",
   c.ChallengeID,c.InstrumentKeyID,c.ProjectID,c.GoalID).
   Scan(&ch.Nonce,&ch.SigningPayload,&ch.IssuedAt,&ch.ExpiresAt,&ch.Status)
  if err!=nil{return ErrInvalidTransition}
  if ch.Status!="issued"||ch.IssuedAt>now||ch.ExpiresAt<=now||
   ch.IssuedAt<key.ApprovedAt||ch.ExpiresAt>key.ExpiresAt||
   ch.ExpiresAt>pin.ExpiresAt{return ErrInvalidTransition}
  original,err:=instrumentSigningBytes(key,pin,ch)
  if err!=nil{return err}
  if !bytes.Equal(original,ch.SigningPayload){return ErrInvalidTransition}
  var trust,pair,fp string
  var locally,remotely int
  err=tx.QueryRowContext(ctx,
   "SELECT n.trust_state,p.status,p.peer_tls_fingerprint,p.local_confirmed,p.peer_confirmed FROM harness_nodes n JOIN node_pairings p ON p.peer_node_id=n.id WHERE n.id=? AND n.local=0",
   c.PeerNodeID).Scan(&trust,&pair,&fp,&locally,&remotely)
  if err!=nil||trust!="paired"||pair!="paired"||locally!=1||remotely!=1||
   !tlsCertificateFingerprint.MatchString(fp){return ErrInvalidTransition}
  out=GoalNodeProbeExecutionPermit{
   ID:id,ProjectID:c.ProjectID,GoalID:c.GoalID,ProjectWorkspaceID:wsID,
   InstrumentKeyID:key.ID,ChallengeID:ch.ID,
   SourceID:scope.Target.SourceID,DeclaredTargetID:key.DeclaredTargetID,
   TLSPinID:pin.ID,PeerNodeID:c.PeerNodeID,
   PeerTLSFingerprint:fp,
   OriginalChallengeSHA256:nodeProbeHash(ch.SigningPayload),
   InstrumentPublicKeySHA256:nodeProbeHash(key.PublicKey),
   PlanSHA256:planHash,PlanGeneration:planGeneration,
   Status:"approved",Revision:1,ApprovedBy:c.ActorPrincipalID,
   ApprovedAt:now,ExpiresAt:ch.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_node_probe_execution_permits(
   id,project_id,goal_id,project_workspace_id,instrument_key_id,
   challenge_id,source_id,declared_target_id,tls_pin_id,peer_node_id,
   peer_tls_fingerprint,original_challenge_sha256,instrument_public_key_sha256,
   plan_sha256,plan_generation,status,revision,approved_by,approved_at,expires_at,revoked_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'approved',1,?,?,?,0)`,
   out.ID,out.ProjectID,out.GoalID,out.ProjectWorkspaceID,out.InstrumentKeyID,
   out.ChallengeID,out.SourceID,out.DeclaredTargetID,out.TLSPinID,
   out.PeerNodeID,out.PeerTLSFingerprint,out.OriginalChallengeSHA256,
   out.InstrumentPublicKeySHA256,out.PlanSHA256,out.PlanGeneration,
   out.ApprovedBy,out.ApprovedAt,out.ExpiresAt)
  if err!=nil{return err}
  updated,err:=tx.ExecContext(ctx,
   "UPDATE project_continuous_goals SET revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND revision=?",
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err:=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":out.ID,
   "challenge_id":ch.ID,"peer_node_id":out.PeerNodeID,
   "source_id":out.SourceID,"one_shot":true,
   "device_identity_verified":false,"monitoring_active":false,
   "remediation_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_node_probe_execution_approved",
   AggregateType:"project_goal_node_probe_execution_permit",
   AggregateID:out.ID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalNodeProbeExecutionPermit{},err}
 return out,nil
}

func(s *Service)RevokeGoalNodeProbeExecutionPermit(ctx context.Context,
 c RevokeGoalNodeProbeExecutionPermitCommand)(GoalNodeProbeExecutionPermit,error){
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPermitRevision<1{
  return GoalNodeProbeExecutionPermit{},ErrInvalidCommand
 }
 evt,err:=s.ids.New("evt");if err!=nil{return GoalNodeProbeExecutionPermit{},err}
 var out GoalNodeProbeExecutionPermit
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  out,err=scanNodeProbePermit(tx.QueryRowContext(ctx,
   "SELECT "+nodeProbePermitColumns+
   " FROM project_goal_node_probe_execution_permits WHERE id=? AND project_id=? AND goal_id=?",
   c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if out.Status!="approved"||out.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  var revision int64
  err=tx.QueryRowContext(ctx,
   "SELECT revision FROM project_continuous_goals WHERE id=? AND project_id=?",
   c.GoalID,c.ProjectID).Scan(&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  updated,err:=tx.ExecContext(ctx,`UPDATE project_goal_node_probe_execution_permits
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND status='approved' AND revision=?`,
   c.ActorPrincipalID,now,c.PermitID,c.ExpectedPermitRevision)
  if err!=nil{return err}
  n,err:=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  updated,err=tx.ExecContext(ctx,
   "UPDATE project_continuous_goals SET revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND revision=?",
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  out.Status="revoked";out.Revision++;out.RevokedAt=now
  payload,_:=json.Marshal(map[string]any{
   "goal_id":out.GoalID,"permit_id":out.ID,
   "peer_node_id":out.PeerNodeID,"revoked":true,
   "monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_node_probe_execution_revoked",
   AggregateType:"project_goal_node_probe_execution_permit",
   AggregateID:out.ID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalNodeProbeExecutionPermit{},err}
 return out,nil
}
// Effective execution state is a bounded live inventory projection.
// Historical approval alone does NOT mean a Node may still act.
func(s *Service)ListGoalNodeProbeExecutionPermits(ctx context.Context,
 projectID,goalID string)([]GoalNodeProbeExecutionPermit,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,
  "SELECT 1 FROM project_continuous_goals g JOIN projects p ON p.id=g.project_id AND p.status='active' JOIN workspaces t ON t.id=p.workspace_id AND t.status='active' WHERE g.id=? AND g.project_id=?",
  goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 now:=s.clock.UnixMilli()
 rows,err:=s.db.QueryContext(ctx,"SELECT "+nodeProbePermitColumns+`,
 CASE
  WHEN permit.status='revoked' THEN 'revoked'
  WHEN permit.expires_at<=? THEN 'expired'
  WHEN NOT EXISTS(SELECT 1 FROM projects p
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_continuous_goals goal ON goal.id=permit.goal_id
    AND goal.project_id=p.id AND goal.status='configured'
   JOIN project_workspaces pw ON pw.id=permit.project_workspace_id
    AND pw.project_id=p.id AND pw.status='active'
   WHERE p.id=permit.project_id AND p.status='active'
    AND goal.project_workspace_id=permit.project_workspace_id
    AND goal.plan_generation=permit.plan_generation
    AND EXISTS(SELECT 1 FROM json_each(goal.monitoring_plan_json,'$.source_ids') src
     WHERE src.type='text' AND src.value=permit.source_id))
   THEN 'scope_inactive'
  WHEN NOT EXISTS(SELECT 1 FROM project_goal_instrument_challenges ch
   WHERE ch.id=permit.challenge_id AND ch.status='issued'
    AND ch.expires_at>?) THEN 'challenge_unavailable'
  WHEN NOT EXISTS(SELECT 1 FROM project_goal_instrument_keys k
   WHERE k.id=permit.instrument_key_id AND k.status='approved'
    AND k.expires_at>?) THEN 'instrument_unavailable'
  WHEN NOT EXISTS(SELECT 1 FROM project_goal_tls_endpoint_pins pin
   WHERE pin.id=permit.tls_pin_id AND pin.status='approved'
    AND pin.expires_at>?) THEN 'pin_unavailable'
  WHEN NOT EXISTS(SELECT 1 FROM principals h
   JOIN projects p ON p.id=permit.project_id
   JOIN workspace_memberships m ON m.workspace_id=p.workspace_id
    AND m.principal_id=h.id AND m.status='active'
   WHERE h.id=permit.approved_by AND h.status='active'
    AND h.principal_type='human') THEN 'approver_inactive'
  WHEN NOT EXISTS(SELECT 1 FROM harness_nodes n
   JOIN node_pairings pair ON pair.peer_node_id=n.id
   WHERE n.id=permit.peer_node_id AND n.local=0
    AND n.trust_state='paired' AND pair.status='paired'
    AND pair.local_confirmed=1 AND pair.peer_confirmed=1
    AND pair.peer_tls_fingerprint=permit.peer_tls_fingerprint)
   THEN 'peer_unavailable'
  ELSE 'active'
 END
 FROM project_goal_node_probe_execution_permits permit
 WHERE permit.project_id=? AND permit.goal_id=?
 ORDER BY permit.approved_at DESC LIMIT 100`,
 now,now,now,now,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalNodeProbeExecutionPermit{}
 for rows.Next(){
  var p GoalNodeProbeExecutionPermit
  err=rows.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,
   &p.InstrumentKeyID,&p.ChallengeID,&p.SourceID,&p.DeclaredTargetID,
   &p.TLSPinID,&p.PeerNodeID,&p.PeerTLSFingerprint,
   &p.OriginalChallengeSHA256,&p.InstrumentPublicKeySHA256,
   &p.PlanSHA256,&p.PlanGeneration,&p.Status,&p.Revision,&p.ApprovedBy,&p.ApprovedAt,&p.ExpiresAt,
   &p.RevokedAt,&p.EffectiveExecutionState)
  if err!=nil{return nil,err}
  out=append(out,p)
 }
 return out,rows.Err()
}
