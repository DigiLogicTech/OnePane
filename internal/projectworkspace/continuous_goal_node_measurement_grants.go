package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalNodeMeasurementGrant is a HUMAN-approved, exact peer-mTLS telemetry
// authority. It does not authorise network scanning, raw host telemetry,
// a verified-health badge, Task execution or remediation.
type GoalNodeMeasurementGrant struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 PeerNodeID string `json:"peer_node_id"`
 PeerTLSFingerprint string `json:"peer_tls_fingerprint"`
 PlanSHA256 string `json:"plan_sha256"`
 PlanGeneration int64 `json:"plan_generation"`
 Status string `json:"status"`
 EffectiveTransportState string `json:"effective_transport_state,omitempty"`
 Revision int64 `json:"revision"`
 LastSequence int64 `json:"last_sequence"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}
type ApproveGoalNodeMeasurementGrantCommand struct {
 ProjectID,GoalID,SourceID,PeerNodeID,ActorPrincipalID string
 ExpectedGoalRevision,ExpiresAt int64
}
type RevokeGoalNodeMeasurementGrantCommand struct {
 ProjectID,GoalID,GrantID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedGrantRevision int64
}
const goalNodeMeasurementGrantColumns=`id,project_id,goal_id,project_workspace_id,
 source_id,peer_node_id,peer_tls_fingerprint,plan_sha256,plan_generation,status,revision,
 last_sequence,approved_by,approved_at,expires_at,revoked_at`

func scanGoalNodeMeasurementGrant(row scanner)(GoalNodeMeasurementGrant,error){
 var g GoalNodeMeasurementGrant
 err:=row.Scan(&g.ID,&g.ProjectID,&g.GoalID,&g.ProjectWorkspaceID,
  &g.SourceID,&g.PeerNodeID,&g.PeerTLSFingerprint,&g.PlanSHA256,&g.PlanGeneration,&g.Status,
  &g.Revision,&g.LastSequence,&g.ApprovedBy,&g.ApprovedAt,
  &g.ExpiresAt,&g.RevokedAt)
 return g,err
}

func(s *Service)ApproveGoalNodeMeasurementGrant(ctx context.Context,
 c ApproveGoalNodeMeasurementGrantCommand)(GoalNodeMeasurementGrant,error){
 if strings.TrimSpace(c.ProjectID)==""||strings.TrimSpace(c.GoalID)==""||
  strings.TrimSpace(c.PeerNodeID)==""||strings.TrimSpace(c.ActorPrincipalID)==""||
  !goalSourceID.MatchString(c.SourceID)||c.ExpectedGoalRevision<1{
  return GoalNodeMeasurementGrant{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gnode");if err!=nil{return GoalNodeMeasurementGrant{},err}
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalNodeMeasurementGrant{},err}
 var result GoalNodeMeasurementGrant
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+7*24*60*60*1000{
   return ErrInvalidCommand
  }
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"||p.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var workspaceID,status,planRaw string
  var revision,planGeneration int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,
   g.monitoring_plan_json,g.revision,g.plan_generation
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id AND w.project_id=g.project_id
   JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
   WHERE g.id=? AND g.project_id=? AND w.status='active'`,
   p.WorkspaceID,c.GoalID,c.ProjectID).
   Scan(&workspaceID,&status,&planRaw,&revision,&planGeneration)
  if err!=nil{return err}
  if status!="configured"{return ErrInvalidTransition}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  plan,planHash,err:=goalReviewPlan(planRaw);if err!=nil{return err}
  if planGeneration<1{return ErrInvalidTransition}
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==c.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidCommand}
  var fingerprint,nodeTrust,pairStatus string
  var locallyConfirmed,peerConfirmed int
  // One consistent read binds exact peer identity to its confirmed
  // mTLS pairing; a mere discovery fingerprint is never sufficient.
  err=tx.QueryRowContext(ctx,`SELECT n.trust_state,
   p.status,p.local_confirmed,p.peer_confirmed,p.peer_tls_fingerprint
   FROM harness_nodes n JOIN node_pairings p ON p.peer_node_id=n.id
   WHERE n.id=? AND n.local=0 AND p.status='paired'
   AND p.local_confirmed=1 AND p.peer_confirmed=1`,c.PeerNodeID).
   Scan(&nodeTrust,&pairStatus,&locallyConfirmed,&peerConfirmed,&fingerprint)
  if err!=nil{return ErrInvalidTransition}
  if nodeTrust!="paired"||pairStatus!="paired"||
   locallyConfirmed!=1||peerConfirmed!=1{return ErrInvalidTransition}
  if err!=nil||!strings.HasPrefix(fingerprint,"sha256:")||
   len(fingerprint)!=71{return ErrInvalidTransition}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_node_measurement_grants
   WHERE goal_id=? AND source_id=? AND peer_node_id=? AND status='approved'`,
   c.GoalID,c.SourceID,c.PeerNodeID).Scan(&active)
  if err!=nil{return err}
  if active>0{return ErrInvalidTransition}
  result=GoalNodeMeasurementGrant{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:workspaceID,SourceID:c.SourceID,
   PeerNodeID:c.PeerNodeID,PeerTLSFingerprint:fingerprint,
   PlanSHA256:planHash,PlanGeneration:planGeneration,
   Status:"approved",EffectiveTransportState:"active",Revision:1,ApprovedBy:c.ActorPrincipalID,
   ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_node_measurement_grants(
   id,project_id,goal_id,project_workspace_id,source_id,
   peer_node_id,peer_tls_fingerprint,plan_sha256,plan_generation,status,revision,last_sequence,
   approved_by,approved_at,expires_at,revoked_at)
   VALUES(?,?,?,?,?,?,?,?,?,'approved',1,0,?,?,?,0)`,
   result.ID,result.ProjectID,result.GoalID,result.ProjectWorkspaceID,
   result.SourceID,result.PeerNodeID,result.PeerTLSFingerprint,
   result.PlanSHA256,result.PlanGeneration,
   result.ApprovedBy,result.ApprovedAt,result.ExpiresAt)
  if err!=nil{return err}
  resultRevision,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  affected,err:=resultRevision.RowsAffected()
  if err!=nil{return err}
  if affected!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"grant_id":idv,"source_id":c.SourceID,
   "peer_node_id":c.PeerNodeID,"transport_kind":"pinned_peer_mtls",
   "monitoring_active":false,"device_health":"unverified",
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_node_measurement_grant_approved",
   AggregateType:"project_goal_node_measurement_grant",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalNodeMeasurementGrant{},err}
 return result,nil
}

func(s *Service)RevokeGoalNodeMeasurementGrant(ctx context.Context,
 c RevokeGoalNodeMeasurementGrantCommand)(GoalNodeMeasurementGrant,error){
 if c.ProjectID==""||c.GoalID==""||c.GrantID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedGrantRevision<1{return GoalNodeMeasurementGrant{},ErrInvalidCommand}
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalNodeMeasurementGrant{},err}
 var out GoalNodeMeasurementGrant
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  out,err=scanGoalNodeMeasurementGrant(tx.QueryRowContext(ctx,
   `SELECT `+goalNodeMeasurementGrantColumns+`
   FROM project_goal_node_measurement_grants
   WHERE id=? AND goal_id=? AND project_id=?`,c.GrantID,c.GoalID,c.ProjectID))
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
  if err!=nil{return err}
  if out.Status!="approved"||out.Revision!=c.ExpectedGrantRevision{return ErrRevisionConflict}
  var goalRevision int64
  err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&goalRevision)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  affected,err:=tx.ExecContext(ctx,`UPDATE project_goal_node_measurement_grants
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND revision=? AND status='approved'`,
   c.ActorPrincipalID,now,c.GrantID,c.ExpectedGrantRevision)
  if err!=nil{return err}
  n,err:=affected.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  affected,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err=affected.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  out.Status="revoked";out.EffectiveTransportState="revoked";out.Revision++;out.RevokedAt=now
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"grant_id":c.GrantID,
   "peer_node_id":out.PeerNodeID,"revoked":true,
   "monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_node_measurement_grant_revoked",
   AggregateType:"project_goal_node_measurement_grant",AggregateID:c.GrantID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalNodeMeasurementGrant{},err}
 return out,nil
}

func(s *Service)ListGoalNodeMeasurementGrants(ctx context.Context,
 projectID,goalID string)([]GoalNodeMeasurementGrant,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var planRaw string
 var activePlanGeneration int64
 err:=s.db.QueryRowContext(ctx,`SELECT g.monitoring_plan_json,g.plan_generation FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&planRaw,&activePlanGeneration)
 if err!=nil{return nil,err}
 _,activePlanHash,err:=goalReviewPlan(planRaw)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalNodeMeasurementGrantColumns+`,
  CASE
   WHEN status='revoked' THEN 'revoked'
   WHEN expires_at<=? THEN 'expired'
   WHEN NOT EXISTS(SELECT 1 FROM projects p
    JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
    JOIN project_continuous_goals goal ON goal.id=project_goal_node_measurement_grants.goal_id
     AND goal.project_id=p.id AND goal.status='configured'
    JOIN project_workspaces pw ON pw.id=project_goal_node_measurement_grants.project_workspace_id
     AND pw.project_id=p.id AND pw.status='active'
    WHERE p.id=project_goal_node_measurement_grants.project_id AND p.status='active'
     AND goal.project_workspace_id=project_goal_node_measurement_grants.project_workspace_id
     AND goal.plan_generation=project_goal_node_measurement_grants.plan_generation
     AND EXISTS(SELECT 1 FROM json_each(goal.monitoring_plan_json,'$.source_ids') source
      WHERE source.type='text' AND source.value=project_goal_node_measurement_grants.source_id))
    THEN 'scope_inactive'
   WHEN NOT EXISTS(SELECT 1 FROM principals pr
    JOIN projects p ON p.id=project_goal_node_measurement_grants.project_id
    JOIN workspace_memberships mem ON mem.workspace_id=p.workspace_id
     AND mem.principal_id=pr.id AND mem.status='active'
    WHERE pr.id=project_goal_node_measurement_grants.approved_by
     AND pr.status='active' AND pr.principal_type='human')
    THEN 'approver_inactive'
   WHEN NOT EXISTS(SELECT 1 FROM harness_nodes n
    JOIN node_pairings pairing ON pairing.peer_node_id=n.id
    WHERE n.id=project_goal_node_measurement_grants.peer_node_id
     AND n.local=0 AND n.trust_state='paired'
     AND pairing.status='paired' AND pairing.local_confirmed=1
     AND pairing.peer_confirmed=1
     AND pairing.peer_tls_fingerprint=project_goal_node_measurement_grants.peer_tls_fingerprint)
    THEN 'peer_unavailable'
   ELSE 'active'
  END
  FROM project_goal_node_measurement_grants
  WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,
  s.clock.UnixMilli(),projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalNodeMeasurementGrant{}
 for rows.Next(){
  var item GoalNodeMeasurementGrant
  err=rows.Scan(&item.ID,&item.ProjectID,&item.GoalID,
   &item.ProjectWorkspaceID,&item.SourceID,&item.PeerNodeID,
   &item.PeerTLSFingerprint,&item.PlanSHA256,&item.PlanGeneration,
   &item.Status,&item.Revision,
   &item.LastSequence,&item.ApprovedBy,&item.ApprovedAt,
   &item.ExpiresAt,&item.RevokedAt,&item.EffectiveTransportState)
  if err!=nil{return nil,err}
  if item.Status=="approved"&&item.ExpiresAt>s.clock.UnixMilli()&&
   (item.PlanGeneration!=activePlanGeneration||item.PlanSHA256!=activePlanHash){
   item.EffectiveTransportState="scope_inactive"
  }
  out=append(out,item)
 }
 return out,rows.Err()
}

type GoalNodeMeasurementReceipt struct{
 ID string `json:"id"`
 GrantID string `json:"grant_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 PeerNodeID string `json:"peer_node_id"`
 SourceID string `json:"source_id"`
 Sequence int64 `json:"sequence"`
 SignalType string `json:"signal_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 ReceivedAt int64 `json:"received_at"`
 TransportKind string `json:"transport_kind"`
}

// Operator view is deliberately limited to 100 recent immutable metadata
// receipts; no sender-provided raw values, hostnames or body bytes can leave.
func(s *Service)ListGoalNodeMeasurementReceipts(ctx context.Context,
 projectID,goalID string)([]GoalNodeMeasurementReceipt,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,grant_id,project_id,goal_id,
  project_workspace_id,peer_node_id,source_id,sequence,signal_type,outcome,
  observed_at,received_at,transport_kind
  FROM project_goal_node_measurement_receipts
  WHERE project_id=? AND goal_id=?
  ORDER BY received_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 results:=[]GoalNodeMeasurementReceipt{}
 for rows.Next(){
  var item GoalNodeMeasurementReceipt
  if err=rows.Scan(&item.ID,&item.GrantID,&item.ProjectID,&item.GoalID,
   &item.ProjectWorkspaceID,&item.PeerNodeID,&item.SourceID,&item.Sequence,
   &item.SignalType,&item.Outcome,&item.ObservedAt,&item.ReceivedAt,
   &item.TransportKind);err!=nil{return nil,err}
  results=append(results,item)
 }
 return results,rows.Err()
}
