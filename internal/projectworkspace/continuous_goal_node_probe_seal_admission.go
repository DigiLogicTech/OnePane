package projectworkspace

import (
 "context"
 "crypto/sha256"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// SealGoalNodeProbeExecutionPermitCommand requests a fresh OnePane issuer
// seal on the ORIGINAL, still-active, human-authorised Node TCP4 action.
// No Node network operation is launched by this operation.
type SealGoalNodeProbeExecutionPermitCommand struct {
 ProjectID,GoalID,PermitID,ActorPrincipalID string
}

// SealGoalNodeProbeExecutionPermit reserves the Goal's SQLite writer lock
// before checking the live effective grant, signs it WHILE retaining that
// lock, and audits the signed envelope before transaction commit. A raced
// revocation must serialize before or after the seal and cannot slip
// between a stale inventory read and the newly minted server signature.
//
// seal MUST use an independently enrolled existing OnePane server signing
// identity. This only attests software issuer approval at seal time, NOT
// current offline revocation, physical Node identity, device health,
// independent observation or any remediation authority.
func(s *Service)SealGoalNodeProbeExecutionPermit(
 ctx context.Context,c SealGoalNodeProbeExecutionPermitCommand,
 seal func(GoalNodeProbeExecutionPermit)([]byte,error))([]byte,error){
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||
  c.ActorPrincipalID==""||seal==nil{
  return nil,ErrInvalidCommand
 }
 eventID,err:=s.ids.New("evt")
 if err!=nil{return nil,err}
 var signed []byte
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  // A real write to the Goal row acquires the SQLite single-writer lock
  // before the snapshot read. This writes no changed field or plan data.
  result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET updated_at=updated_at
   WHERE id=? AND project_id=? AND status='configured'`,
   c.GoalID,c.ProjectID)
  if err!=nil{return err}
  count,err:=result.RowsAffected()
  if err!=nil{return err}
  if count!=1{return ErrInvalidTransition}
  // Exactly the SAME live case classification that backs human inventory.
  grants,err:=s.listGoalNodeProbeExecutionPermitsWith(
   ctx,tx,c.ProjectID,c.GoalID,now)
  if err!=nil{return err}
  var original *GoalNodeProbeExecutionPermit
  for i:=range grants{
   g:=&grants[i]
   if g.ID==c.PermitID&&g.ApprovedBy==c.ActorPrincipalID{
    original=g
    break
   }
  }
  if original==nil||original.EffectiveExecutionState!="active"{
   return ErrInvalidTransition
  }
  envelope,err:=seal(*original)
  if err!=nil{return err}
  if len(envelope)==0||len(envelope)>8192||!json.Valid(envelope){
   return errors.New("original OnePane server seal is not bounded canonical JSON")
  }
  digest:=sha256.Sum256(envelope)
  payload,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":c.PermitID,
   "server_seal_sha256":fmt.Sprintf("sha256:%x",digest[:]),
   "peer_node_id":original.PeerNodeID,
   "monitoring_active":false,"device_identity_verified":false,
   "remediation_authorized":false,
  })
  if err=s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_node_probe_execution_sealed",
   AggregateType:"project_goal_node_probe_execution_permit",
   AggregateID:c.PermitID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  });err!=nil{return err}
  signed=envelope
  return nil
 })
 if err!=nil{return nil,err}
 return signed,nil
}
