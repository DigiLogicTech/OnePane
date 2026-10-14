package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "net"
 "strconv"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// EnsureTCPRecurringWorkerPrincipal creates a dedicated, audited system
// principal. It cannot approve a grant, grant itself tools, or switch goal
// modes. A human must already have approved BOTH exact-target permissions.
func(s *Service)EnsureTCPRecurringWorkerPrincipal(ctx context.Context)error{
 now:=s.clock.UnixMilli()
 evt,err:=s.ids.New("evt")
 if err!=nil{return err}
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var kind,status string
  err:=tx.QueryRowContext(ctx,`SELECT principal_type,status FROM principals
   WHERE id=?`,GoalTCPRecurringWorkerPrincipal).Scan(&kind,&status)
  if err==nil{
   if kind!="system"||status!="active"{return ErrPrincipalIneligible}
   return nil
  }
  if !errors.Is(err,sql.ErrNoRows){return err}
  _,err=tx.ExecContext(ctx,`INSERT INTO principals(
    id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES(?,'system','OnePane consented TCP observer','active',1,?,?)`,
    GoalTCPRecurringWorkerPrincipal,now,now)
  if err!=nil{return err}
  actor:=GoalTCPRecurringWorkerPrincipal
  payload,_:=json.Marshal(map[string]any{
   "principal_id":actor,"network_effect_class":"recurring_tcp_connect",
   "requires_independent_human_grant":true,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,Type:"system_principal.registered",
   AggregateType:"principal",AggregateID:actor,
   ActorPrincipalID:&actor,Payload:payload,OccurredAt:now,
  })
 })
}

type dueTCPRecurringGrant struct{
 projectID string
 goalID string
 grantID string
 manualPermitID string
 goalRevision int64
 permitRevision int64
}

func(s *Service)dueTCPRecurringGrants(ctx context.Context,now int64,after string)([]dueTCPRecurringGrant,error){
 // The first filter is a bounded SQLite candidate query, not admission.
 // Every candidate is rechecked against current human grants and canonical
 // Project permissions in the writer TX and once before the socket.
 rows,err:=s.db.QueryContext(ctx,`SELECT r.project_id,r.goal_id,r.id,
  r.manual_permit_id,g.revision,p.revision
  FROM project_goal_tcp_recurring_grants r
  JOIN project_goal_tcp_probe_permits p ON p.id=r.manual_permit_id
   AND p.project_id=r.project_id AND p.goal_id=r.goal_id
   AND p.project_workspace_id=r.project_workspace_id
   AND p.source_id=r.source_id AND p.status='approved'
   AND p.revision=r.manual_permit_revision AND p.expires_at>?
  JOIN project_continuous_goals g ON g.id=r.goal_id
   AND g.project_id=r.project_id AND g.project_workspace_id=r.project_workspace_id
   AND g.status='configured' AND g.cadence_minutes=r.cadence_minutes
  JOIN projects pr ON pr.id=r.project_id AND pr.status='active'
  JOIN workspaces tenant ON tenant.id=pr.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=r.project_workspace_id
   AND w.project_id=pr.id AND w.status='active'
  WHERE r.status='approved' AND r.expires_at>? AND r.id>?
   AND NOT EXISTS(
    SELECT 1 FROM project_goal_tcp_observation_fences fence
    WHERE fence.project_id=r.project_id AND fence.goal_id=r.goal_id
     AND fence.project_workspace_id=r.project_workspace_id AND fence.status='paused')
   AND NOT EXISTS(
    SELECT 1 FROM project_goal_tcp_probe_attempts a
     WHERE a.goal_id=r.goal_id AND a.source_id=r.source_id
      AND a.cadence_window=CAST(?/(r.cadence_minutes*60000) AS INTEGER))
  ORDER BY r.id LIMIT 32`,now,now,after,now)
 if err!=nil{return nil,err}
 defer rows.Close()
 results:=make([]dueTCPRecurringGrant,0,32)
 for rows.Next(){
  var c dueTCPRecurringGrant
  if err=rows.Scan(&c.projectID,&c.goalID,&c.grantID,
   &c.manualPermitID,&c.goalRevision,&c.permitRevision);err!=nil{return nil,err}
  results=append(results,c)
 }
 return results,rows.Err()
}

func(s *Service)advanceTCPRecurringCursor(ctx context.Context,revision int64,after string)error{
 _,err:=s.db.ExecContext(ctx,`UPDATE goal_tcp_recurring_worker_cursors
  SET after_grant_id=?,revision=revision+1,updated_at=?
  WHERE worker_id='exact_tcp_connect' AND revision=?`,
  after,s.clock.UnixMilli(),revision)
 return err // competing poller wins; pointer alone confers no authority
}

// A named grant cannot silently become a general TCP scanner. Exactly one
// connection to the previously human-approved IPv4 literal and port is
// attempted inside one goal/source/cadence window, with <=1500ms deadline.
func(s *Service)runRecurringGoalTCPProbe(ctx context.Context,
 c dueTCPRecurringGrant,dialer tcpConnectDialer)(GoalTCPProbeAttempt,error){
 if dialer==nil{return GoalTCPProbeAttempt{},ErrInvalidCommand}
 select{
 case manualTCPProbeSlots<-struct{}{}:
  defer func(){<-manualTCPProbeSlots}()
 case <-ctx.Done():
  return GoalTCPProbeAttempt{},ctx.Err()
 }
 claim,err:=s.claimGoalTCPAttempt(ctx,RunGoalTCPProbeCommand{
  ProjectID:c.projectID,GoalID:c.goalID,PermitID:c.manualPermitID,
  ExpectedGoalRevision:c.goalRevision,
  ExpectedPermitRevision:c.permitRevision,
  ActorPrincipalID:GoalTCPRecurringWorkerPrincipal,
  RecurringGrantID:c.grantID,
 })
 if err!=nil{return GoalTCPProbeAttempt{},err}
 ok,err:=s.tcpRecurringClaimStillAuthorised(ctx,claim,c.grantID)
 if err!=nil||!ok{
  return s.completeTCPAttempt(ctx,claim,"cancelled")
 }
 dialCtx,cancel:=context.WithTimeout(ctx,time.Duration(claim.TimeoutMS)*time.Millisecond)
 defer cancel()
 conn,dialErr:=dialer.DialContext(dialCtx,"tcp4",
  net.JoinHostPort(claim.TargetIPv4,strconv.Itoa(claim.TargetPort)))
 if conn!=nil{_ = conn.Close()}
 outcome:=tcpDialOutcome(ctx,dialCtx,conn,dialErr)
 return s.completeTCPAttempt(ctx,claim,outcome)
}

// TickDueTCPRecurringProbes is deliberately bounded. No opt-in grant means
// NO network effects. It never backfills missed windows after downtime,
// never tries another address, and never reports a healthy Project. The
// daemon may invoke it only after the dedicated worker identity is installed.
func(s *Service)TickDueTCPRecurringProbes(ctx context.Context,limit int)([]GoalTCPProbeAttempt,error){
 return s.tickDueTCPRecurringProbes(ctx,limit,&net.Dialer{})
}
func(s *Service)tickDueTCPRecurringProbes(ctx context.Context,limit int,
 dialer tcpConnectDialer)([]GoalTCPProbeAttempt,error){
 if limit<1||limit>4||dialer==nil{return nil,ErrInvalidCommand}
 var after string
 var revision int64
 err:=s.db.QueryRowContext(ctx,`SELECT after_grant_id,revision
   FROM goal_tcp_recurring_worker_cursors
   WHERE worker_id='exact_tcp_connect'`).Scan(&after,&revision)
 if err!=nil{return nil,err}
 due,err:=s.dueTCPRecurringGrants(ctx,s.clock.UnixMilli(),after)
 if err!=nil{return nil,err}
 if len(due)==0&&after!=""{
  due,err=s.dueTCPRecurringGrants(ctx,s.clock.UnixMilli(),"")
  if err!=nil{return nil,err}
  if len(due)==0{
   err=s.advanceTCPRecurringCursor(ctx,revision,"")
   return []GoalTCPProbeAttempt{},err
  }
 }
 out:=make([]GoalTCPProbeAttempt,0,limit)
 last:=""
 for _,c:=range due{
  if ctx.Err()!=nil{return out,ctx.Err()}
  if len(out)>=limit{break}
  last=c.grantID
  result,callErr:=s.runRecurringGoalTCPProbe(ctx,c,dialer)
  if callErr!=nil{
   if ctx.Err()!=nil{return out,ctx.Err()}
   // A denied/stale grant cannot starve later valid sources. Do not
   // retry this source in a tight loop after an unexpected denial.
   continue
  }
  out=append(out,result)
 }
 if last!=""{
  if err=s.advanceTCPRecurringCursor(ctx,revision,last);err!=nil{return out,err}
 }
 return out,nil
}
