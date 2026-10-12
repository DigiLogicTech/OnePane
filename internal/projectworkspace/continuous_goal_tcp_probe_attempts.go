package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "net"
 "os"
 "strconv"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalTCPProbeAttempt is only a TCP handshake reachability receipt.
// Even status=connected does NOT establish the service is trusted, secure,
// authenticated, patched, healthy, or performing its intended function.
type GoalTCPProbeAttempt struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 PermitID string `json:"permit_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 CadenceWindow int64 `json:"cadence_window"`
 GoalRevision int64 `json:"goal_revision"`
 PermitRevision int64 `json:"permit_revision"`
 RequestedBy string `json:"requested_by"`
 Status string `json:"status"`
 ClaimedAt int64 `json:"claimed_at"`
 CompletedAt int64 `json:"completed_at"`
 MonitoringActive bool `json:"monitoring_active"`
}

type RunGoalTCPProbeCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedGoalRevision int64
 ExpectedPermitRevision int64
 ActorPrincipalID string
 // Only a service-internal scheduled path may supply this. The public
 // one-shot HTTP request has no corresponding JSON field.
 RecurringGrantID string
}

type tcpConnectDialer interface {
 DialContext(context.Context,string,string)(net.Conn,error)
}

// Constrained-hardware default: at most two manual outbound TCP handshake
// attempts concurrently in one OnePane process. Waiting callers do not
// claim an attempt, create a socket, or consume the source cadence quota.
var manualTCPProbeSlots=make(chan struct{},2)


const tcpAttemptColumns=`id,project_id,goal_id,permit_id,project_workspace_id,source_id,
target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,permit_revision,
requested_by,status,claimed_at,completed_at`

func scanTCPAttempt(r scanner)(GoalTCPProbeAttempt,error){
 var a GoalTCPProbeAttempt
 err:=r.Scan(&a.ID,&a.ProjectID,&a.GoalID,&a.PermitID,&a.ProjectWorkspaceID,
  &a.SourceID,&a.TargetIPv4,&a.TargetPort,&a.TimeoutMS,
  &a.CadenceWindow,&a.GoalRevision,&a.PermitRevision,
  &a.RequestedBy,&a.Status,&a.ClaimedAt,&a.CompletedAt)
 return a,err
}

// Claim one immutable attempt BEFORE any socket operation, with an exact
// source, Project/Workspace, current human operator, revocable permit,
// strict target and short deadline revalidated under the SQLite writer lock.
func(s *Service)claimGoalTCPAttempt(ctx context.Context,c RunGoalTCPProbeCommand)(GoalTCPProbeAttempt,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPermitRevision<1{
  return GoalTCPProbeAttempt{},ErrInvalidCommand
 }
 attemptID,err:=s.ids.New("gtcp")
 if err!=nil{return GoalTCPProbeAttempt{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return GoalTCPProbeAttempt{},err}
 var attempt GoalTCPProbeAttempt
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if c.RecurringGrantID==""{
   if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  }else if c.ActorPrincipalID!=GoalTCPRecurringWorkerPrincipal{
   return ErrPrincipalIneligible
  }
  var wsID,status,planRaw string
  var revision int64
  var cadence int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json,
    g.revision,g.cadence_minutes FROM project_continuous_goals g
    JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=g.project_id AND w.status='active'
    JOIN workspaces t ON t.id=? AND t.status='active'
    WHERE g.id=? AND g.project_id=?`,project.WorkspaceID,c.GoalID,c.ProjectID).
    Scan(&wsID,&status,&planRaw,&revision,&cadence)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"||cadence<5||cadence>10080{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  permit,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
    ` FROM project_goal_tcp_probe_permits WHERE id=? AND project_id=? AND goal_id=?`,
    c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  // A grant from a human whose tenant membership has since been revoked
  // is no longer live network authority, even before its expiry time.
  var activeApprover int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals approver
    JOIN workspace_memberships membership ON
      membership.principal_id=approver.id AND membership.workspace_id=?
      AND membership.status='active'
    WHERE approver.id=? AND approver.principal_type='human'
      AND approver.status='active'`,project.WorkspaceID,permit.ApprovedBy).Scan(&activeApprover)
  if err!=nil{return fmt.Errorf("%w: network approver no longer active",ErrPrincipalIneligible)}
  if permit.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  if permit.Status!="approved"||permit.ExpiresAt<=now||
    permit.ProjectWorkspaceID!=wsID||permit.PlanHash!=hash||
    permit.ProbeKind!="tcp_connect"||permit.EffectClass!="network_tcp_connect"||
    permit.TimeoutMS<100||permit.TimeoutMS>1500||
    permit.TargetPort<1||permit.TargetPort>65535{
   return ErrInvalidTransition
  }
  if _,err=canonicalPrivateProbeIPv4(permit.TargetIPv4);err!=nil{return ErrInvalidTransition}
  if c.RecurringGrantID!=""{
   if err=s.authorizeRecurringTCPClaimTx(ctx,tx,c.RecurringGrantID,c.ProjectID,c.GoalID,
       wsID,permit,hash,cadence,now);err!=nil{return err}
  }
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==permit.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidTransition}
  window:=now/(cadence*60000)
  var exists int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tcp_probe_attempts
   WHERE goal_id=? AND source_id=? AND cadence_window=?`,
   c.GoalID,permit.SourceID,window).Scan(&exists);err!=nil{return err}
  if exists!=0{return ErrInvalidTransition}
  attempt=GoalTCPProbeAttempt{
   ID:attemptID,ProjectID:c.ProjectID,GoalID:c.GoalID,
   PermitID:permit.ID,ProjectWorkspaceID:wsID,SourceID:permit.SourceID,
   TargetIPv4:permit.TargetIPv4,TargetPort:permit.TargetPort,
   TimeoutMS:permit.TimeoutMS,CadenceWindow:window,GoalRevision:revision,
   PermitRevision:permit.Revision,RequestedBy:c.ActorPrincipalID,
   Status:"claimed",ClaimedAt:now,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_attempts(
    id,project_id,goal_id,permit_id,project_workspace_id,source_id,
    target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,
    permit_revision,requested_by,status,claimed_at,completed_at)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'claimed',?,0)`,
    attempt.ID,attempt.ProjectID,attempt.GoalID,attempt.PermitID,
    attempt.ProjectWorkspaceID,attempt.SourceID,attempt.TargetIPv4,
    attempt.TargetPort,attempt.TimeoutMS,attempt.CadenceWindow,
    attempt.GoalRevision,attempt.PermitRevision,attempt.RequestedBy,now)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":permit.ID,"attempt_id":attempt.ID,
   "source_id":permit.SourceID,"target_ipv4":permit.TargetIPv4,
   "target_port":permit.TargetPort,"status":"claimed","goal_revision":revision,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_probe_claimed",
   AggregateType:"project_goal_tcp_probe_attempt",AggregateID:attempt.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPProbeAttempt{},err}
 return attempt,nil
}

// A fresh check immediately before DialContext closes the ordinary window
// between claim and network dispatch. A revoke after a socket starts cannot
// retroactively undo its handshake, but each attempt lasts at most 1500ms.
func(s *Service)tcpAttemptStillAuthorised(ctx context.Context,a GoalTCPProbeAttempt)(bool,error){
 // Project mode is canonicalised from policy_json by Project(); it is
 // NOT a projects.operating_mode SQLite column.
 project,err:=s.repo.Project(ctx,a.ProjectID)
 if err!=nil{return false,err}
 if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
  return false,nil
 }
 var status,permitStatus,hash,goalPlan,source,ip,effect,kind string
 var permitRevision,goalRevision,expiresAt int64
 var port,timeout int
 err=s.db.QueryRowContext(ctx,`SELECT g.status,p.status,p.plan_hash,g.monitoring_plan_json,
    p.source_id,p.target_ipv4,p.effect_class,p.probe_kind,
    p.revision,g.revision,p.expires_at,p.target_port,p.timeout_ms
   FROM project_goal_tcp_probe_permits p
   JOIN project_continuous_goals g ON g.id=p.goal_id
    AND g.project_id=p.project_id AND g.project_workspace_id=p.project_workspace_id
   JOIN projects pr ON pr.id=p.project_id AND pr.status='active'
   JOIN workspaces tenant ON tenant.id=pr.workspace_id AND tenant.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=pr.id AND w.status='active'
   JOIN principals approver ON approver.id=p.approved_by
     AND approver.principal_type='human' AND approver.status='active'
   JOIN workspace_memberships approver_member ON
     approver_member.workspace_id=pr.workspace_id
     AND approver_member.principal_id=approver.id
     AND approver_member.status='active'
   JOIN principals human ON human.id=? AND human.principal_type='human'
    AND human.status='active'
   JOIN workspace_memberships membership ON membership.workspace_id=pr.workspace_id
    AND membership.principal_id=human.id AND membership.status='active'
   WHERE p.id=? AND p.project_id=? AND p.goal_id=?`,
  a.RequestedBy,a.PermitID,a.ProjectID,a.GoalID).
  Scan(&status,&permitStatus,&hash,&goalPlan,&source,&ip,&effect,&kind,
   &permitRevision,&goalRevision,&expiresAt,&port,&timeout)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if status!="configured"||permitStatus!="approved"||expiresAt<=s.clock.UnixMilli()||
  permitRevision!=a.PermitRevision||goalRevision!=a.GoalRevision||
  effect!="network_tcp_connect"||kind!="tcp_connect"||
  source!=a.SourceID||ip!=a.TargetIPv4||
  port!=a.TargetPort||timeout!=a.TimeoutMS{return false,nil}
 plan,actualHash,err:=goalReviewPlan(goalPlan)
 if err!=nil||hash!=actualHash{return false,nil}
 if _,err=canonicalPrivateProbeIPv4(ip);err!=nil{return false,nil}
 for _,value:=range plan.SourceIDs{if value==source{return true,nil}}
 return false,nil
}

func(s *Service)completeTCPAttempt(ctx context.Context,a GoalTCPProbeAttempt,status string)(GoalTCPProbeAttempt,error){
 if status!="connected"&&status!="not_connected"&&
   status!="timed_out"&&status!="cancelled"{return GoalTCPProbeAttempt{},ErrInvalidCommand}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalTCPProbeAttempt{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if now<a.ClaimedAt{now=a.ClaimedAt}
  change,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_probe_attempts
    SET status=?,completed_at=? WHERE id=? AND status='claimed'`,status,now,a.ID)
  if err!=nil{return err}
  n,err:=change.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrInvalidTransition}
  body,_:=json.Marshal(map[string]any{
   "attempt_id":a.ID,"goal_id":a.GoalID,"permit_id":a.PermitID,
   "source_id":a.SourceID,"status":status,"reachability_only":true,
   "verified_device_health":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,Type:"project.goal_tcp_probe_finished",
   AggregateType:"project_goal_tcp_probe_attempt",AggregateID:a.ID,
   ActorPrincipalID:&a.RequestedBy,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPProbeAttempt{},err}
 return scanTCPAttempt(s.db.QueryRowContext(ctx,`SELECT `+tcpAttemptColumns+
  ` FROM project_goal_tcp_probe_attempts WHERE id=?`,a.ID))
}

// Only a direct human-requested one-shot test is implemented. No recurring
// scheduler invokes this method. The net.Dialer cannot resolve hostnames
// because the target was stored and revalidated as an exact IPv4 literal.
func(s *Service)RunManualGoalTCPProbe(ctx context.Context,c RunGoalTCPProbeCommand)(GoalTCPProbeAttempt,error){
 return s.runManualGoalTCPProbe(ctx,c,&net.Dialer{})
}

func(s *Service)runManualGoalTCPProbe(ctx context.Context,c RunGoalTCPProbeCommand,dialer tcpConnectDialer)(GoalTCPProbeAttempt,error){
 if dialer==nil{return GoalTCPProbeAttempt{},ErrInvalidCommand}
 // Do not queue unbounded sockets or Claims when a small CPU/VM is busy.
 select{
 case manualTCPProbeSlots<-struct{}{}:
  defer func(){<-manualTCPProbeSlots}()
 case <-ctx.Done():
  return GoalTCPProbeAttempt{},ctx.Err()
 }
 attempt,err:=s.claimGoalTCPAttempt(ctx,c)
 if err!=nil{return GoalTCPProbeAttempt{},err}
 allowed,e:=s.tcpAttemptStillAuthorised(ctx,attempt)
 if e!=nil||!allowed{
  // Once claimed, never retry this window automatically on uncertain
  // admission: cancellation remains a durable, visible audit result.
  return s.completeTCPAttempt(ctx,attempt,"cancelled")
 }
 connectCtx,cancel:=context.WithTimeout(ctx,time.Duration(attempt.TimeoutMS)*time.Millisecond)
 defer cancel()
 conn,dialErr:=dialer.DialContext(connectCtx,"tcp4",
  net.JoinHostPort(attempt.TargetIPv4,strconv.Itoa(attempt.TargetPort)))
 if conn!=nil{_ = conn.Close()}
 outcome:="connected"
 if dialErr!=nil{
  outcome="not_connected"
  if errors.Is(dialErr,context.DeadlineExceeded)||
    errors.Is(dialErr,os.ErrDeadlineExceeded)||connectCtx.Err()==context.DeadlineExceeded{
   outcome="timed_out"
  }
 }
 // A failing outcome is not retried within this cadence window.
 return s.completeTCPAttempt(ctx,attempt,outcome)
}

func(s *Service)ListGoalTCPProbeAttempts(ctx context.Context,projectID,goalID string)([]GoalTCPProbeAttempt,error){
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
 rows,err:=s.db.QueryContext(ctx,`SELECT `+tcpAttemptColumns+
  ` FROM project_goal_tcp_probe_attempts
    WHERE project_id=? AND goal_id=?
    ORDER BY claimed_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=[]GoalTCPProbeAttempt{}
 for rows.Next(){
  item,err:=scanTCPAttempt(rows)
  if err!=nil{return nil,err}
  result=append(result,item)
 }
 return result,rows.Err()
}

// A claimed attempt that never completed due to service termination is
// explicitly unknown; it is not evidence of a successful or failed probe.
func (a GoalTCPProbeAttempt) OutcomeDescription() string {
 if a.Status=="claimed"{return "unknown_after_interruption"}
 return fmt.Sprintf("tcp_reachability_%s",a.Status)
}
