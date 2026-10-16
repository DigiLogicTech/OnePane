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

// TCPRecurringGrant is independently approved permission for recurring,
// narrowly bounded TCP connects. A manual TCP grant alone NEVER creates it.
// No background network dispatch is implied merely by storing this record.
type TCPRecurringGrant struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ManualPermitID string `json:"manual_permit_id"`
 ManualPermitRevision int64 `json:"manual_permit_revision"`
 SourceID string `json:"source_id"`
 PlanHash string `json:"plan_hash"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 CadenceMinutes int64 `json:"cadence_minutes"`
 EffectClass string `json:"effect_class"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 RevokedBy *string `json:"revoked_by,omitempty"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}
type ApproveTCPRecurringGrantCommand struct{
 ProjectID string
 GoalID string
 ManualPermitID string
 ExpectedGoalRevision int64
 ExpectedManualPermitRevision int64
 ExpiresAt int64
 ActorPrincipalID string
}
type RevokeTCPRecurringGrantCommand struct{
 ProjectID string
 GoalID string
 GrantID string
 ExpectedGoalRevision int64
 ExpectedGrantRevision int64
 ActorPrincipalID string
}
const recurringGrantColumns=`id,project_id,goal_id,project_workspace_id,manual_permit_id,
 manual_permit_revision,source_id,plan_hash,target_ipv4,target_port,timeout_ms,
 cadence_minutes,effect_class,status,revision,approved_by,revoked_by,
 approved_at,expires_at,revoked_at`

func scanRecurringGrant(r scanner)(TCPRecurringGrant,error){
 var p TCPRecurringGrant
 var revoked sql.NullString
 err:=r.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,
  &p.ManualPermitID,&p.ManualPermitRevision,&p.SourceID,&p.PlanHash,
  &p.TargetIPv4,&p.TargetPort,&p.TimeoutMS,&p.CadenceMinutes,
  &p.EffectClass,&p.Status,&p.Revision,&p.ApprovedBy,&revoked,
  &p.ApprovedAt,&p.ExpiresAt,&p.RevokedAt)
 if revoked.Valid{p.RevokedBy=&revoked.String}
 return p,err
}

func(s *Service)ApproveTCPRecurringGrant(ctx context.Context,c ApproveTCPRecurringGrantCommand)(TCPRecurringGrant,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.ManualPermitID=strings.TrimSpace(c.ManualPermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.ManualPermitID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedManualPermitRevision<1{return TCPRecurringGrant{},ErrInvalidCommand}
 idv,err:=s.ids.New("gtcpr")
 if err!=nil{return TCPRecurringGrant{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return TCPRecurringGrant{},err}
 var grant TCPRecurringGrant
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+15*60*1000||c.ExpiresAt>now+24*60*60*1000{
   return ErrInvalidCommand
  }
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,goalStatus,planRaw string
  var goalRevision,cadence int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,
    g.monitoring_plan_json,g.revision,g.cadence_minutes
    FROM project_continuous_goals g
    JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=g.project_id AND w.status='active'
    JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
    WHERE g.id=? AND g.project_id=?`,project.WorkspaceID,c.GoalID,c.ProjectID).
    Scan(&wsID,&goalStatus,&planRaw,&goalRevision,&cadence)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if goalStatus!="configured"||cadence<15||cadence>10080{return ErrInvalidTransition}
  manual,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
    ` FROM project_goal_tcp_probe_permits WHERE id=? AND project_id=? AND goal_id=?`,
    c.ManualPermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if manual.Revision!=c.ExpectedManualPermitRevision{return ErrRevisionConflict}
  if manual.Status!="approved"||manual.ExpiresAt<=now||manual.ExpiresAt<c.ExpiresAt||
    manual.ProjectWorkspaceID!=wsID||manual.ProbeKind!="tcp_connect"||
    manual.EffectClass!="network_tcp_connect"||
    manual.TimeoutMS<100||manual.TimeoutMS>1500||
    manual.TargetPort<1||manual.TargetPort>65535{return ErrInvalidTransition}
  if _,err=canonicalPrivateProbeIPv4(manual.TargetIPv4);err!=nil{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  if manual.PlanHash!=hash{return ErrInvalidTransition}
  inPlan:=false
  for _,source:=range plan.SourceIDs{if source==manual.SourceID{inPlan=true;break}}
  if !inPlan{return ErrInvalidTransition}
  var validApprover int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
    JOIN workspace_memberships membership ON
      membership.workspace_id=? AND membership.principal_id=p.id
      AND membership.status='active'
    WHERE p.id=? AND p.principal_type='human' AND p.status='active'`,
    project.WorkspaceID,manual.ApprovedBy).Scan(&validApprover)
  if err!=nil{return ErrPrincipalIneligible}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tcp_recurring_grants
    WHERE goal_id=? AND source_id=? AND status='approved'`,
    c.GoalID,manual.SourceID).Scan(&active)
  if err!=nil{return err}
  if active!=0{return ErrInvalidTransition}
  grant=TCPRecurringGrant{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:wsID,ManualPermitID:manual.ID,
   ManualPermitRevision:manual.Revision,SourceID:manual.SourceID,
   PlanHash:hash,TargetIPv4:manual.TargetIPv4,TargetPort:manual.TargetPort,
   TimeoutMS:manual.TimeoutMS,CadenceMinutes:cadence,
   EffectClass:"recurring_tcp_connect",Status:"approved",Revision:1,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_recurring_grants(
    id,project_id,goal_id,project_workspace_id,manual_permit_id,
    manual_permit_revision,source_id,plan_hash,target_ipv4,target_port,
    timeout_ms,cadence_minutes,effect_class,status,revision,approved_by,
    approved_at,expires_at)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'recurring_tcp_connect','approved',1,?,?,?)`,
    grant.ID,grant.ProjectID,grant.GoalID,grant.ProjectWorkspaceID,
    grant.ManualPermitID,grant.ManualPermitRevision,grant.SourceID,
    grant.PlanHash,grant.TargetIPv4,grant.TargetPort,grant.TimeoutMS,
    grant.CadenceMinutes,grant.ApprovedBy,grant.ApprovedAt,grant.ExpiresAt)
  if err!=nil{return err}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=? AND status='configured'`,
    now,c.GoalID,c.ProjectID,goalRevision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "grant_id":grant.ID,"goal_id":c.GoalID,"source_id":grant.SourceID,
   "manual_permit_id":manual.ID,"target_ipv4":grant.TargetIPv4,
   "target_port":grant.TargetPort,"cadence_minutes":cadence,
   "effect_class":grant.EffectClass,"expires_at":grant.ExpiresAt,
   "goal_revision":goalRevision+1,"probe_executed":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_recurring_grant_approved",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return TCPRecurringGrant{},err}
 return grant,nil
}

func(s *Service)RevokeTCPRecurringGrant(ctx context.Context,c RevokeTCPRecurringGrantCommand)(TCPRecurringGrant,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID);c.GoalID=strings.TrimSpace(c.GoalID)
 c.GrantID=strings.TrimSpace(c.GrantID);c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.GrantID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedGrantRevision<1{return TCPRecurringGrant{},ErrInvalidCommand}
 evt,err:=s.ids.New("evt")
 if err!=nil{return TCPRecurringGrant{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var current int64
  var status string
  err=tx.QueryRowContext(ctx,`SELECT g.revision,g.status
    FROM project_continuous_goals g
    JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=g.project_id AND w.status='active'
    WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&current,&status)
  if err!=nil{return err}
  if current!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status=="retired"{return ErrInvalidTransition}
  p,err:=scanRecurringGrant(tx.QueryRowContext(ctx,`SELECT `+recurringGrantColumns+
    ` FROM project_goal_tcp_recurring_grants
      WHERE id=? AND project_id=? AND goal_id=?`,
    c.GrantID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if p.Revision!=c.ExpectedGrantRevision{return ErrRevisionConflict}
  if p.Status!="approved"{return ErrInvalidTransition}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_recurring_grants
    SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
    WHERE id=? AND status='approved' AND revision=?`,
    c.ActorPrincipalID,now,c.GrantID,p.Revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  changed,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=?`,now,c.GoalID,c.ProjectID,current)
  if err!=nil{return err}
  n,err=changed.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "grant_id":p.ID,"goal_id":p.GoalID,"source_id":p.SourceID,
   "effect_class":p.EffectClass,"revoked_at":now,
   "goal_revision":current+1,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_recurring_grant_revoked",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return TCPRecurringGrant{},err}
 return scanRecurringGrant(s.db.QueryRowContext(ctx,`SELECT `+recurringGrantColumns+
  ` FROM project_goal_tcp_recurring_grants WHERE id=?`,c.GrantID))
}
func(s *Service)ListTCPRecurringGrants(ctx context.Context,projectID,goalID string)([]TCPRecurringGrant,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
   JOIN projects p ON p.id=g.project_id AND p.status='active'
   JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
     AND w.project_id=p.id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+recurringGrantColumns+
    ` FROM project_goal_tcp_recurring_grants WHERE project_id=? AND goal_id=?
    ORDER BY approved_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]TCPRecurringGrant{}
 for rows.Next(){
  p,err:=scanRecurringGrant(rows)
  if err!=nil{return nil,err}
  out=append(out,p)
 }
 return out,rows.Err()
}

// TCPRecurringGrantAllowed checks current scopes without taking an outbound
// action. No scanner uses this as an implicit substitute for an atomic claim.
func(s *Service)TCPRecurringGrantAllowed(ctx context.Context,grantID string)(bool,error){
 var projectID,goalID string
 err:=s.db.QueryRowContext(ctx,`SELECT project_id,goal_id
  FROM project_goal_tcp_recurring_grants WHERE id=?`,grantID).Scan(&projectID,&goalID)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 project,err:=s.repo.Project(ctx,projectID)
 if err!=nil{return false,err}
 if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{return false,nil}
 var g TCPRecurringGrant
 g,err=scanRecurringGrant(s.db.QueryRowContext(ctx,`SELECT `+recurringGrantColumns+
  ` FROM project_goal_tcp_recurring_grants WHERE id=?`,grantID))
 if err!=nil{return false,err}
 if g.Status!="approved"||g.ExpiresAt<=s.clock.UnixMilli(){return false,nil}
 var status,raw string
 var rev,cadence,planGeneration int64
 err=s.db.QueryRowContext(ctx,`SELECT goal.status,goal.monitoring_plan_json,
   goal.revision,goal.cadence_minutes,goal.plan_generation FROM project_continuous_goals goal
   JOIN project_workspaces w ON w.id=goal.project_workspace_id
     AND w.project_id=goal.project_id AND w.status='active'
   JOIN workspaces t ON t.id=? AND t.status='active'
   WHERE goal.id=? AND goal.project_id=?
   AND goal.project_workspace_id=?`,project.WorkspaceID,goalID,projectID,
  g.ProjectWorkspaceID).Scan(&status,&raw,&rev,&cadence,&planGeneration)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if status!="configured"||cadence!=g.CadenceMinutes||planGeneration<1{return false,nil}
 plan,hash,err:=goalReviewPlan(raw)
 if err!=nil||hash!=g.PlanHash{return false,nil}
 allowed:=false
 for _,source:=range plan.SourceIDs{if source==g.SourceID{allowed=true;break}}
 if !allowed{return false,nil}
 manual,err:=scanGoalTCPProbePermit(s.db.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
  ` FROM project_goal_tcp_probe_permits WHERE id=? AND goal_id=?
   AND project_id=?`,g.ManualPermitID,goalID,projectID))
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 // The approved plan hash can recur when a human edits and restores the
 // same source list. Generation is monotonic: historical TCP permission
 // must never reappear as live on an identical restored plan.
 if manual.Status!="approved"||manual.Revision!=g.ManualPermitRevision||
  manual.ExpiresAt<=s.clock.UnixMilli()||manual.PlanHash!=g.PlanHash||
  manual.PlanGeneration!=planGeneration||manual.PlanGeneration<1||
  manual.ProjectWorkspaceID!=g.ProjectWorkspaceID||
  manual.ProbeKind!="tcp_connect"||manual.EffectClass!="network_tcp_connect"||
  manual.TargetPort<1||manual.TargetPort>65535||
  manual.TimeoutMS<100||manual.TimeoutMS>1500||
  manual.SourceID!=g.SourceID||manual.TargetIPv4!=g.TargetIPv4||
  manual.TargetPort!=g.TargetPort||manual.TimeoutMS!=g.TimeoutMS{
  return false,nil
 }
 for _,principal:=range []string{g.ApprovedBy,manual.ApprovedBy}{
  var active int
  err=s.db.QueryRowContext(ctx,`SELECT 1 FROM principals p
    JOIN workspace_memberships m ON m.principal_id=p.id
      AND m.workspace_id=? AND m.status='active'
    WHERE p.id=? AND p.principal_type='human' AND p.status='active'`,
    project.WorkspaceID,principal).Scan(&active)
  if errors.Is(err,sql.ErrNoRows){return false,nil}
  if err!=nil{return false,err}
 }
 return true,nil
}
