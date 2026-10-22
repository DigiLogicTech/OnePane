package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalObservationReceipt is metadata-only, not proof that a goal is healthy.
// Values, subject names and credentials must never enter this response or
// the goal event payload. The underlying immutable observation stays scoped.
type GoalObservationReceipt struct {
 ID string `json:"id"`
 GoalID string `json:"goal_id"`
 ObservationID string `json:"observation_id"`
 SourceID string `json:"source_id"`
 IntegrityHash string `json:"integrity_hash"`
 ObservedAt int64 `json:"observed_at"`
 AttachedAt int64 `json:"attached_at"`
}

type AttachGoalObservationCommand struct {
 ProjectID string
 GoalID string
 ObservationID string
 ExpectedRevision int64
 ActorPrincipalID string
}

// AttachGoalObservation binds an already-recorded, integrity-verified,
// fresh observation to a specific configured L5 goal. It NEVER executes a
// probe, activates scheduling, evaluates health or infers remediation rights.
func (s *Service) AttachGoalObservation(ctx context.Context, cmd AttachGoalObservationCommand) (GoalObservationReceipt,error) {
 cmd.ProjectID=strings.TrimSpace(cmd.ProjectID)
 cmd.GoalID=strings.TrimSpace(cmd.GoalID)
 cmd.ObservationID=strings.TrimSpace(cmd.ObservationID)
 cmd.ActorPrincipalID=strings.TrimSpace(cmd.ActorPrincipalID)
 if cmd.ProjectID==""||cmd.GoalID==""||cmd.ObservationID==""||
  cmd.ActorPrincipalID==""||cmd.ExpectedRevision<1{return GoalObservationReceipt{},ErrInvalidCommand}
 // Scope admission must precede the expensive integrity/Get lookup:
 // neither an untrusted Observation ID nor a foreign tenant is allowed to
 // make this endpoint load or process another Workspace's sensor payload.
 // This read-only proof is NOT the final grant; the transaction below
 // rechecks current Project, principal, source and observation identity.
 var visible int
 err:=s.db.QueryRowContext(ctx,`SELECT 1
   FROM observations o
   JOIN projects p ON p.workspace_id=o.workspace_id AND p.id=?
   JOIN project_continuous_goals g ON g.project_id=p.id AND g.id=?
   JOIN project_workspaces pw ON pw.id=g.project_workspace_id
      AND pw.project_id=p.id AND pw.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN principals actor ON actor.id=? AND actor.status='active'
      AND actor.principal_type='human'
   JOIN workspace_memberships membership ON membership.workspace_id=p.workspace_id
      AND membership.principal_id=actor.id AND membership.status='active'
   WHERE o.id=? AND p.status='active' AND g.status='configured'
   LIMIT 1`,cmd.ProjectID,cmd.GoalID,cmd.ActorPrincipalID,cmd.ObservationID).Scan(&visible)
 if err!=nil{
  if errors.Is(err,sql.ErrNoRows){
   // Keep non-human attestation a permission denial (HTTP 403), not a
   // malformed request (400). This second lookup discloses only the
   // caller's own principal type, never another tenant's observation.
   var principalType string
   e:=s.db.QueryRowContext(ctx,
    "SELECT principal_type FROM principals WHERE id=? AND status='active'",
    cmd.ActorPrincipalID).Scan(&principalType)
   if e!=nil||principalType!="human"{
    return GoalObservationReceipt{},fmt.Errorf("%w: human attestation required",ErrPrincipalIneligible)
   }
   return GoalObservationReceipt{},fmt.Errorf("%w: observation or goal scope unavailable",ErrInvalidCommand)
  }
  return GoalObservationReceipt{},err
 }
 // Observation rows are immutable by schema; preflight verifies their full
 // stored JSON value's hash, without returning that value to the caller.
 source:=observation.NewService(s.db,s.tx,s.clock)
 if err:=source.VerifyIntegrity(ctx,cmd.ObservationID);err!=nil{
  return GoalObservationReceipt{},fmt.Errorf("%w: observation integrity unavailable: %v",ErrInvalidCommand,err)
 }
 proof,err:=source.Get(ctx,cmd.ObservationID)
 if err!=nil{return GoalObservationReceipt{},err}
 now:=s.clock.UnixMilli()
 if proof.ObservedAt<1||proof.ObservedAt>now||!goalSourceID.MatchString(proof.ProbeToolID){
  return GoalObservationReceipt{},ErrInvalidCommand
 }
 receiptID,err:=s.ids.New("gobs")
 if err!=nil{return GoalObservationReceipt{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return GoalObservationReceipt{},err}
 receipt:=GoalObservationReceipt{
  ID:receiptID,GoalID:cmd.GoalID,ObservationID:cmd.ObservationID,
  SourceID:proof.ProbeToolID,IntegrityHash:proof.IntegrityHash,
  ObservedAt:proof.ObservedAt,AttachedAt:now,
 }
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,cmd.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return fmt.Errorf("%w: active Level 5 Project required",ErrInvalidCommand)
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,cmd.ActorPrincipalID);err!=nil{return err}
  // Attestation is operator-mediated for now. Autonomous ingestion will
  // require a separate signed source admission and current tool grant.
  var principalType string
  if err=tx.QueryRowContext(ctx,"SELECT principal_type FROM principals WHERE id=? AND status='active'",cmd.ActorPrincipalID).Scan(&principalType);err!=nil{return err}
  if principalType!="human"{return fmt.Errorf("%w: human attestation required",ErrPrincipalIneligible)}
  var status,workspaceID,planRaw string
  var revision int64
  var cadence int
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.project_workspace_id,g.monitoring_plan_json,g.revision,g.cadence_minutes
    FROM project_continuous_goals g JOIN project_workspaces w
    ON w.id=g.project_workspace_id AND w.project_id=g.project_id AND w.status='active'
    WHERE g.id=? AND g.project_id=?`,cmd.GoalID,cmd.ProjectID).Scan(&status,&workspaceID,&planRaw,&revision,&cadence)
  if err!=nil{return err}
  if revision!=cmd.ExpectedRevision{return ErrRevisionConflict}
  if status!="configured"{return ErrInvalidTransition}
  // Enforce the same immutable-source allowlist parser at the final
  // evidence-attachment transaction boundary; an inconsistent stored
  // JSON row must not alter which sensor the operator authorised.
  canonical,err:=canonicalGoalPlan(json.RawMessage(planRaw))
  if err!=nil{return fmt.Errorf("%w: invalid stored monitoring plan: %v",ErrInvalidCommand,err)}
  var plan goalMonitorPlan
  if err=json.Unmarshal(canonical,&plan);err!=nil||plan.Version!=1{
   return fmt.Errorf("%w: invalid monitoring plan",ErrInvalidCommand)
  }
  permitted:=false
  for _,id:=range plan.SourceIDs{if id==receipt.SourceID{permitted=true;break}}
  if !permitted{return fmt.Errorf("%w: observation source is not in the approved goal plan",ErrInvalidCommand)}
  // The goal's cadence bounds the maximum age of evidence. The cap limits
  // late replay even for long-cadence goals. No future timestamps permitted.
  freshnessMs:=int64(cadence)*2*60*1000
  if freshnessMs<15*60*1000{freshnessMs=15*60*1000}
  if freshnessMs>24*60*60*1000{freshnessMs=24*60*60*1000}
  if now-receipt.ObservedAt>freshnessMs{return fmt.Errorf("%w: observation is stale",ErrInvalidCommand)}
  // Re-read scoped metadata inside the admission transaction, not from a
  // caller-provided Project/Workspace ID or a model-generated context.
  var tenantID,sourceID,hash string
  var observedAt int64
  err=tx.QueryRowContext(ctx,`SELECT workspace_id,probe_tool_id,integrity_hash,observed_at
   FROM observations WHERE id=?`,cmd.ObservationID).Scan(&tenantID,&sourceID,&hash,&observedAt)
  if err!=nil{return err}
  if tenantID!=project.WorkspaceID||sourceID!=receipt.SourceID||
    hash!=receipt.IntegrityHash||observedAt!=receipt.ObservedAt{
   return fmt.Errorf("%w: observation ownership or integrity mismatch",ErrCrossWorkspace)
  }
  var count int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_observation_receipts
    WHERE goal_id=? AND observation_id=?`,cmd.GoalID,cmd.ObservationID).Scan(&count);err!=nil{return err}
  if count!=0{return fmt.Errorf("%w: observation was already attached to this goal",ErrInvalidCommand)}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_observation_receipts
    (id,goal_id,observation_id,source_id,integrity_hash,observed_at,attached_at,attached_by)
    VALUES(?,?,?,?,?,?,?,?)`,
    receipt.ID,receipt.GoalID,receipt.ObservationID,receipt.SourceID,
    receipt.IntegrityHash,receipt.ObservedAt,receipt.AttachedAt,cmd.ActorPrincipalID)
  if err!=nil{return err}
  result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=? AND status='configured'`,
    now,cmd.GoalID,cmd.ProjectID,revision)
  if err!=nil{return err}
  changed,err:=result.RowsAffected()
  if err!=nil{return err}
  if changed!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "goal_id":cmd.GoalID,"project_id":cmd.ProjectID,"project_workspace_id":workspaceID,
   "observation_id":receipt.ObservationID,"source_id":receipt.SourceID,
   "integrity_hash":receipt.IntegrityHash,"goal_revision":revision+1,
   "status":"configured","health":"unobserved",
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_observation_attached",
   AggregateType:"project_continuous_goal",AggregateID:cmd.GoalID,
   ActorPrincipalID:&cmd.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{
  if errors.Is(err,sql.ErrNoRows){return GoalObservationReceipt{},fmt.Errorf("%w: goal or observation scope unavailable",ErrInvalidCommand)}
  return GoalObservationReceipt{},err
 }
 return receipt,nil
}

func(s *Service) GoalObservations(ctx context.Context,projectID,goalID string)([]GoalObservationReceipt,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{return nil,ErrInvalidCommand}
 var exists int
 if err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_continuous_goals
  WHERE id=? AND project_id=?`,goalID,projectID).Scan(&exists);err!=nil{return nil,err}
 if exists!=1{return nil,sql.ErrNoRows}
 rows,err:=s.db.QueryContext(ctx,`SELECT r.id,r.goal_id,r.observation_id,r.source_id,r.integrity_hash,r.observed_at,r.attached_at
  FROM project_goal_observation_receipts r
  WHERE r.goal_id=? ORDER BY r.attached_at DESC,r.id DESC LIMIT 200`,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalObservationReceipt{}
 for rows.Next(){
  var r GoalObservationReceipt
  if err:=rows.Scan(&r.ID,&r.GoalID,&r.ObservationID,&r.SourceID,&r.IntegrityHash,&r.ObservedAt,&r.AttachedAt);err!=nil{return nil,err}
  out=append(out,r)
 }
 return out,rows.Err()
}
