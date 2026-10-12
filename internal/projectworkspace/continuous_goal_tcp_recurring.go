package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// TCPRecurringConsent explicitly authorises repeat read-only TCP handshakes,
// separate from an existing ONE-SHOT network target permit. No remediation,
// network discovery, credentials, DNS, web requests or health verification.
type TCPRecurringConsent struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 PermitID string `json:"permit_id"`
 SourceID string `json:"source_id"`
 PlanHash string `json:"plan_hash"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 CadenceMinutes int `json:"cadence_minutes"`
 EffectClass string `json:"effect_class"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 RevokedBy *string `json:"revoked_by,omitempty"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}

type ApproveTCPRecurringConsentCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedGoalRevision int64
 ExpectedPermitRevision int64
 ExpiresAt int64
 ActorPrincipalID string
}

type RevokeTCPRecurringConsentCommand struct {
 ProjectID string
 GoalID string
 ConsentID string
 ExpectedGoalRevision int64
 ExpectedConsentRevision int64
 ActorPrincipalID string
}

const tcpRecurringConsentCols = `id,project_id,goal_id,project_workspace_id,permit_id,source_id,
plan_hash,target_ipv4,target_port,timeout_ms,cadence_minutes,effect_class,
status,revision,approved_by,revoked_by,approved_at,expires_at,revoked_at`

func scanTCPRecurringConsent(row scanner)(TCPRecurringConsent,error){
 var item TCPRecurringConsent
 var revoked sql.NullString
 err:=row.Scan(&item.ID,&item.ProjectID,&item.GoalID,
  &item.ProjectWorkspaceID,&item.PermitID,&item.SourceID,&item.PlanHash,
  &item.TargetIPv4,&item.TargetPort,&item.TimeoutMS,&item.CadenceMinutes,
  &item.EffectClass,&item.Status,&item.Revision,&item.ApprovedBy,&revoked,
  &item.ApprovedAt,&item.ExpiresAt,&item.RevokedAt)
 if revoked.Valid{item.RevokedBy=&revoked.String}
 return item,err
}

func(s *Service)ApproveTCPRecurringConsent(ctx context.Context,c ApproveTCPRecurringConsentCommand)(TCPRecurringConsent,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||
   c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
   c.ExpectedPermitRevision<1{return TCPRecurringConsent{},ErrInvalidCommand}
 id,err:=s.ids.New("gtcpconsent")
 if err!=nil{return TCPRecurringConsent{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return TCPRecurringConsent{},err}
 var result TCPRecurringConsent
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var goalStatus,wsID,rawPlan string
  var revision int64
  var cadence int
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.project_workspace_id,
   g.monitoring_plan_json,g.revision,g.cadence_minutes
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
   WHERE g.project_id=? AND g.id=?`,
   project.WorkspaceID,c.ProjectID,c.GoalID).
   Scan(&goalStatus,&wsID,&rawPlan,&revision,&cadence)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if goalStatus!="configured"||cadence<15||cadence>10080{
   return ErrInvalidTransition
  }
  _,hash,err:=goalReviewPlan(rawPlan)
  if err!=nil{return err}
  permit,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
   ` FROM project_goal_tcp_probe_permits WHERE id=? AND project_id=? AND goal_id=?`,
   c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if permit.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  if permit.Status!="approved"||permit.ExpiresAt<=now||
   permit.PlanHash!=hash||permit.ProjectWorkspaceID!=wsID||
   permit.EffectClass!="network_tcp_connect"||permit.ProbeKind!="tcp_connect"||
   permit.TimeoutMS<100||permit.TimeoutMS>1500{
   return ErrInvalidTransition
  }
  if _,err=canonicalPrivateProbeIPv4(permit.TargetIPv4);err!=nil{
   return ErrInvalidTransition
  }
  var approvedBy int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals human
   JOIN workspace_memberships member ON member.principal_id=human.id
    AND member.workspace_id=? AND member.status='active'
   WHERE human.id=? AND human.principal_type='human' AND human.status='active'`,
   project.WorkspaceID,permit.ApprovedBy).Scan(&approvedBy)
  if err!=nil{return ErrPrincipalIneligible}
  // A recurring network grant must NEVER outlive the separately granted
  // exact target, or remain active beyond 24h from its own human approval.
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+24*60*60*1000||
   c.ExpiresAt>permit.ExpiresAt{
   return fmt.Errorf("%w: recurring consent must expire with the exact target within 24 hours",ErrInvalidCommand)
  }
  var active int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tcp_recurring_consents
   WHERE goal_id=? AND source_id=? AND status='approved'`,
   c.GoalID,permit.SourceID).Scan(&active);err!=nil{return err}
  if active!=0{return ErrInvalidTransition}
  result=TCPRecurringConsent{
   ID:id,ProjectID:c.ProjectID,GoalID:c.GoalID,PermitID:permit.ID,
   ProjectWorkspaceID:wsID,SourceID:permit.SourceID,PlanHash:hash,
   TargetIPv4:permit.TargetIPv4,TargetPort:permit.TargetPort,
   TimeoutMS:permit.TimeoutMS,CadenceMinutes:cadence,
   EffectClass:"network_tcp_connect_recurring",Status:"approved",
   Revision:1,ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_recurring_consents(
   id,project_id,goal_id,project_workspace_id,permit_id,source_id,
   plan_hash,target_ipv4,target_port,timeout_ms,cadence_minutes,effect_class,
   status,revision,approved_by,approved_at,expires_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,'network_tcp_connect_recurring','approved',1,?,?,?)`,
   id,c.ProjectID,c.GoalID,wsID,permit.ID,permit.SourceID,
   hash,permit.TargetIPv4,permit.TargetPort,permit.TimeoutMS,cadence,
   c.ActorPrincipalID,now,c.ExpiresAt)
  if err!=nil{return err}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=? WHERE id=? AND project_id=?
   AND revision=? AND status='configured'`,
   now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"consent_id":id,"permit_id":permit.ID,
   "source_id":permit.SourceID,"target_ipv4":permit.TargetIPv4,
   "target_port":permit.TargetPort,"cadence_minutes":cadence,
   "expires_at":c.ExpiresAt,"effect_class":result.EffectClass,
   "goal_revision":revision+1,"remediation_authorised":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_recurring_consent_approved",
   AggregateType:"project_goal_tcp_recurring_consent",AggregateID:id,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return TCPRecurringConsent{},err}
 return result,nil
}

func(s *Service)RevokeTCPRecurringConsent(ctx context.Context,c RevokeTCPRecurringConsentCommand)(TCPRecurringConsent,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.ConsentID=strings.TrimSpace(c.ConsentID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.ConsentID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedConsentRevision<1{return TCPRecurringConsent{},ErrInvalidCommand}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return TCPRecurringConsent{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var goalStatus string
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.revision FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id AND w.project_id=g.project_id
    AND w.status='active' WHERE g.id=? AND g.project_id=?`,
   c.GoalID,c.ProjectID).Scan(&goalStatus,&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if goalStatus=="retired"{return ErrInvalidTransition}
  consent,err:=scanTCPRecurringConsent(tx.QueryRowContext(ctx,`SELECT `+
   tcpRecurringConsentCols+` FROM project_goal_tcp_recurring_consents
   WHERE id=? AND project_id=? AND goal_id=?`,
   c.ConsentID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if consent.Revision!=c.ExpectedConsentRevision{return ErrRevisionConflict}
  if consent.Status!="approved"{return ErrInvalidTransition}
  res,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_recurring_consents
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND status='approved' AND revision=?`,
   c.ActorPrincipalID,now,consent.ID,consent.Revision)
  if err!=nil{return err}
  n,err:=res.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  res,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err=res.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"consent_id":consent.ID,
   "source_id":consent.SourceID,"revoked_at":now,
   "goal_revision":revision+1,"remediation_authorised":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_recurring_consent_revoked",
   AggregateType:"project_goal_tcp_recurring_consent",AggregateID:consent.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return TCPRecurringConsent{},err}
 return scanTCPRecurringConsent(s.db.QueryRowContext(ctx,`SELECT `+
  tcpRecurringConsentCols+` FROM project_goal_tcp_recurring_consents WHERE id=?`,c.ConsentID))
}

func(s *Service)ListTCPRecurringConsents(ctx context.Context,projectID,goalID string)([]TCPRecurringConsent,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+tcpRecurringConsentCols+
  ` FROM project_goal_tcp_recurring_consents
   WHERE project_id=? AND goal_id=? ORDER BY approved_at DESC,id DESC LIMIT 64`,
   projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]TCPRecurringConsent{}
 for rows.Next(){
  item,err:=scanTCPRecurringConsent(rows)
  if err!=nil{return nil,err}
  out=append(out,item)
 }
 return out,rows.Err()
}
