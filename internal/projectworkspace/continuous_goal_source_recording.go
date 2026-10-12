package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

var ErrSourceReadingRateLimited=errors.New("source reading admission rate limit reached")

// SourceReadingReceipt is only the metadata required for a source to sign
// its separately recorded Observation ID and hash. This receipt is explicitly
// NOT a trusted signature, Node provenance, or device-health assessment.
type SourceReadingReceipt struct{
 ObservationID string `json:"observation_id"`
 IntegrityHash string `json:"integrity_hash"`
 SourceID string `json:"source_id"`
 ObservedAt int64 `json:"observed_at"`
 Status string `json:"status"`
}

type RecordGoalSourceReadingCommand struct{
 ProjectID,GoalID,SignerID,ObservationType,ActorPrincipalID string
 CredentialID string
 Value json.RawMessage
}

// RecordGoalSourceReading admits one bounded UNVERIFIED local Observation
// from an enrolled signer identity. It reserves rate budget in a serialized
// SQLite transaction before recording; failure to record conservatively
// consumes the slot until its minute window expires. The separate Ed25519
// acceptance path MUST recheck all current grants before proof admission.
//
// The source cannot nominate Workspace, actor, provenance, trust label,
// subject URI, timestamp, adapter, Node or runtime side effects.
func(s *Service)RecordGoalSourceReading(ctx context.Context,
 c RecordGoalSourceReadingCommand)(SourceReadingReceipt,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.SignerID=strings.TrimSpace(c.SignerID)
 c.ObservationType=strings.TrimSpace(c.ObservationType)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.SignerID==""||
  c.ActorPrincipalID==""||!goalSourceID.MatchString(c.ObservationType)||
  len(c.Value)<1||len(c.Value)>4096{
  return SourceReadingReceipt{},ErrInvalidCommand
 }
 if err:=observation.ValidateUnambiguousJSON(c.Value);err!=nil{
  return SourceReadingReceipt{},fmt.Errorf("%w: invalid reading JSON",ErrInvalidCommand)
 }
 var sourceID,tenantID string
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"||p.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  var tenantStatus string
  err=tx.QueryRowContext(ctx,"SELECT status FROM workspaces WHERE id=?",p.WorkspaceID).Scan(&tenantStatus)
  if err!=nil{return err}
  if tenantStatus!="active"{return ErrWorkspaceInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var sourceKind string
  err=tx.QueryRowContext(ctx,"SELECT principal_type FROM principals WHERE id=? AND status='active'",
   c.ActorPrincipalID).Scan(&sourceKind)
  if err!=nil{return ErrPrincipalIneligible}
  if sourceKind!="agent"&&sourceKind!="service"{return ErrPrincipalIneligible}
  var goalWS,goalStatus,rawPlan string
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).
   Scan(&goalWS,&goalStatus,&rawPlan)
  if err!=nil{return err}
  if goalStatus!="configured"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(rawPlan)
  if err!=nil{return err}
  signer,err:=scanGoalObservationSigner(tx.QueryRowContext(ctx,
   `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
    WHERE id=? AND project_id=? AND goal_id=?`,c.SignerID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if err=checkGoalSourceCredentialTx(ctx,tx,c.CredentialID,c.ProjectID,
   c.GoalID,c.SignerID,p.WorkspaceID,c.ActorPrincipalID,now);err!=nil{return err}
  if signer.Status!="approved"||signer.ExpiresAt<=now||
   signer.SourcePrincipalID!=c.ActorPrincipalID||
   signer.ProjectWorkspaceID!=goalWS||signer.PlanHash!=hash{
   return ErrInvalidTransition
  }
  allowed:=false
  for _,id:=range plan.SourceIDs{if id==signer.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidTransition}
  var human int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
   JOIN workspace_memberships m ON m.workspace_id=?
     AND m.principal_id=p.id AND m.status='active'
   WHERE p.id=? AND p.status='active' AND p.principal_type='human'`,
   p.WorkspaceID,signer.ApprovedBy).Scan(&human)
  if err!=nil{return ErrPrincipalIneligible}
  var started int64
  var count int
  err=tx.QueryRowContext(ctx,`SELECT source_record_window_start,
   source_record_window_count FROM project_goal_observation_signers
   WHERE id=?`,signer.ID).Scan(&started,&count)
  if err!=nil{return err}
  if started>now{return ErrInvalidTransition} // Clock rollback cannot reset budget.
  if started==0||now-started>=60*1000{started=now;count=0}
  if count>=30{return ErrSourceReadingRateLimited}
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_observation_signers
   SET source_record_window_start=?,source_record_window_count=?,
    revision=revision+1
   WHERE id=? AND revision=? AND status='approved' AND expires_at>?`,
   started,count+1,signer.ID,signer.Revision,now)
  if err!=nil{return err}
  n,err:=result.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  sourceID=signer.SourceID
  tenantID=p.WorkspaceID
  return nil
 })
 if err!=nil{
  if errors.Is(err,sql.ErrNoRows){return SourceReadingReceipt{},ErrInvalidTransition}
  return SourceReadingReceipt{},err
 }
 actor:=c.ActorPrincipalID
 recorded,err:=observation.NewService(s.db,s.tx,s.clock).Record(ctx,observation.RecordCommand{
  WorkspaceID:tenantID,
  SubjectRef:"signed-source://"+c.SignerID,
  ObservationType:c.ObservationType,
  ProbeToolID:sourceID,ProbeToolVersion:"1",
  SourcePrincipalID:&actor,ActorPrincipalID:&actor,
  Value:append(json.RawMessage(nil),c.Value...),
  // Do not accept self-reported device trust or Node provenance.\n  // Default sensor payload residency to trusted local Nodes, never cloud.
  Label:policy.DataLabel{
   WorkspaceID:tenantID,Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyTrustedNodes,Trust:policy.TrustUnverifiedDerived,
  },
 })
 if err!=nil{return SourceReadingReceipt{},err}
 return SourceReadingReceipt{
  ObservationID:recorded.ID,IntegrityHash:recorded.IntegrityHash,
  SourceID:sourceID,ObservedAt:recorded.ObservedAt,Status:"unverified_awaiting_source_signature",
 },nil
}
