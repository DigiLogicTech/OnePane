package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "regexp"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// This identity is declared by a human, NOT independently attested by a
// real device. A successful TCP handshake proves only socket reachability.
type GoalTCPDeclaredTarget struct{
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 PermitID string `json:"permit_id"`
 PermitRevision int64 `json:"permit_revision"`
 PlanHash string `json:"plan_hash"`
 DisplayLabel string `json:"display_label"`
 IdentityClass string `json:"identity_class"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
 // Never return the target IP/port in machine-readable triage or metadata;
 // those remain available in the existing separately authorised permit API.
 TargetIPv4 string `json:"-"`
 TargetPort int `json:"-"`
}
type DeclareGoalTCPTargetCommand struct{
 ProjectID,GoalID,PermitID,DisplayLabel,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedPermitRevision,ExpiresAt int64
}
type RevokeGoalTCPTargetCommand struct{
 ProjectID,GoalID,TargetID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedTargetRevision int64
}

var declaredTargetLabel=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)
const declaredTargetColumns=`id,project_id,goal_id,project_workspace_id,
 source_id,permit_id,permit_revision,plan_hash,target_ipv4,target_port,
 display_label,identity_class,status,revision,approved_by,approved_at,
 expires_at,revoked_at`

func scanDeclaredTCPTarget(row scanner)(GoalTCPDeclaredTarget,error){
 var d GoalTCPDeclaredTarget
 err:=row.Scan(&d.ID,&d.ProjectID,&d.GoalID,&d.ProjectWorkspaceID,
  &d.SourceID,&d.PermitID,&d.PermitRevision,&d.PlanHash,
  &d.TargetIPv4,&d.TargetPort,&d.DisplayLabel,&d.IdentityClass,
  &d.Status,&d.Revision,&d.ApprovedBy,&d.ApprovedAt,&d.ExpiresAt,
  &d.RevokedAt)
 return d,err
}

func(s *Service)DeclareGoalTCPTarget(ctx context.Context,
 c DeclareGoalTCPTargetCommand)(GoalTCPDeclaredTarget,error){
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedPermitRevision<1||
  !declaredTargetLabel.MatchString(c.DisplayLabel)||
  strings.TrimSpace(c.DisplayLabel)!=c.DisplayLabel{
  return GoalTCPDeclaredTarget{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gtarget")
 if err!=nil{return GoalTCPDeclaredTarget{},err}
 evt,err:=s.ids.New("evt")
 if err!=nil{return GoalTCPDeclaredTarget{},err}
 var target GoalTCPDeclaredTarget
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+24*60*60*1000{
   return ErrInvalidCommand
  }
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"||p.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{
   return err
  }
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var workspaceID,status,planJSON string
  var goalRevision int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,
   g.monitoring_plan_json,g.revision
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
   WHERE g.id=? AND g.project_id=?`,
   p.WorkspaceID,c.GoalID,c.ProjectID).
   Scan(&workspaceID,&status,&planJSON,&goalRevision)
  if err!=nil{return err}
  if status!="configured"{return ErrInvalidTransition}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  plan,hash,err:=goalReviewPlan(planJSON)
  if err!=nil{return err}
  permit,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,
   `SELECT `+tcpProbePermitCols+`
    FROM project_goal_tcp_probe_permits
    WHERE id=? AND project_id=? AND goal_id=?`,
   c.PermitID,c.ProjectID,c.GoalID))
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
  if err!=nil{return err}
  if permit.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  if permit.Status!="approved"||permit.ExpiresAt<=now||
   permit.ExpiresAt<c.ExpiresAt||permit.ApprovedAt>now||
   permit.ProjectWorkspaceID!=workspaceID||permit.PlanHash!=hash||
   permit.ProbeKind!="tcp_connect"||
   permit.EffectClass!="network_tcp_connect"||
   permit.TimeoutMS<100||permit.TimeoutMS>1500||
   permit.TargetPort<1||permit.TargetPort>65535{
   return ErrInvalidTransition
  }
  if _,err=canonicalPrivateProbeIPv4(permit.TargetIPv4);err!=nil{
   return ErrInvalidTransition
  }
  inPlan:=false
  for _,source:=range plan.SourceIDs{
   if source==permit.SourceID{inPlan=true;break}
  }
  if !inPlan{return ErrInvalidTransition}
  var human int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals operator
   JOIN workspace_memberships m ON m.principal_id=operator.id
    AND m.workspace_id=? AND m.status='active'
   WHERE operator.id=? AND operator.principal_type='human'
    AND operator.status='active'`,
   p.WorkspaceID,permit.ApprovedBy).Scan(&human)
  if err!=nil{return ErrPrincipalIneligible}
  var existing int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*)
   FROM project_goal_tcp_declared_target_bindings
   WHERE permit_id=? AND status='approved'`,c.PermitID).Scan(&existing)
  if err!=nil{return err}
  if existing>0{return ErrInvalidTransition}
  target=GoalTCPDeclaredTarget{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:workspaceID,SourceID:permit.SourceID,
   PermitID:permit.ID,PermitRevision:permit.Revision,PlanHash:hash,
   TargetIPv4:permit.TargetIPv4,TargetPort:permit.TargetPort,
   DisplayLabel:c.DisplayLabel,IdentityClass:"operator_declared_tcp_endpoint",
   Status:"approved",Revision:1,ApprovedBy:c.ActorPrincipalID,
   ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_declared_target_bindings(
   id,project_id,goal_id,project_workspace_id,source_id,permit_id,
   permit_revision,plan_hash,target_ipv4,target_port,display_label,
   identity_class,status,revision,approved_by,approved_at,expires_at,
   revoked_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'approved',1,?,?,?,0)`,
   target.ID,target.ProjectID,target.GoalID,target.ProjectWorkspaceID,
   target.SourceID,target.PermitID,target.PermitRevision,target.PlanHash,
   target.TargetIPv4,target.TargetPort,target.DisplayLabel,
   target.IdentityClass,target.ApprovedBy,target.ApprovedAt,target.ExpiresAt)
  if err!=nil{return err}
  updated,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err:=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"target_id":target.ID,"permit_id":permit.ID,
   "source_id":permit.SourceID,"identity_class":target.IdentityClass,
   "device_identity_verified":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_tcp_declared_target_approved",
   AggregateType:"project_goal_tcp_declared_target",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPDeclaredTarget{},err}
 return target,nil
}

func(s *Service)RevokeGoalTCPTarget(ctx context.Context,
 c RevokeGoalTCPTargetCommand)(GoalTCPDeclaredTarget,error){
 if c.ProjectID==""||c.GoalID==""||c.TargetID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedTargetRevision<1{return GoalTCPDeclaredTarget{},ErrInvalidCommand}
 evt,err:=s.ids.New("evt")
 if err!=nil{return GoalTCPDeclaredTarget{},err}
 var out GoalTCPDeclaredTarget
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  out,err=scanDeclaredTCPTarget(tx.QueryRowContext(ctx,
   `SELECT `+declaredTargetColumns+`
   FROM project_goal_tcp_declared_target_bindings
   WHERE id=? AND project_id=? AND goal_id=?`,
   c.TargetID,c.ProjectID,c.GoalID))
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
  if err!=nil{return err}
  if out.Status!="approved"||out.Revision!=c.ExpectedTargetRevision{
   return ErrRevisionConflict
  }
  var goalRevision int64
  err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&goalRevision)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  update,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_declared_target_bindings
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND revision=? AND status='approved'`,
   c.ActorPrincipalID,now,c.TargetID,c.ExpectedTargetRevision)
  if err!=nil{return err}
  n,err:=update.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  update,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err=update.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  out.Status="revoked";out.Revision++;out.RevokedAt=now
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"target_id":c.TargetID,"revoked":true,
   "device_identity_verified":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_tcp_declared_target_revoked",
   AggregateType:"project_goal_tcp_declared_target",AggregateID:c.TargetID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPDeclaredTarget{},err}
 return out,nil
}

func(s *Service)ListGoalTCPTargets(ctx context.Context,
 projectID,goalID string)([]GoalTCPDeclaredTarget,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+declaredTargetColumns+`
  FROM project_goal_tcp_declared_target_bindings
  WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalTCPDeclaredTarget{}
 for rows.Next(){
  item,err:=scanDeclaredTCPTarget(rows)
  if err!=nil{return nil,err}
  out=append(out,item)
 }
 return out,rows.Err()
}
