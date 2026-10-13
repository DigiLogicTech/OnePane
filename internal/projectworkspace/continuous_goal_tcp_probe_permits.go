package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "net/netip"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalTCPProbePermit is distinct from a receipt-review permit: it requests
// permission for ONE exact outbound TCP-connect target. Registration alone
// does not perform network I/O or start a recurring monitor or Agent.
type GoalTCPProbePermit struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 PlanHash string `json:"plan_hash"`
 PlanGeneration int64 `json:"plan_generation"`
 ProbeKind string `json:"probe_kind"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 EffectClass string `json:"effect_class"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 RevokedBy *string `json:"revoked_by,omitempty"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}

type ApproveGoalTCPProbeCommand struct {
 ProjectID string
 GoalID string
 SourceID string
 TargetIPv4 string
 TargetPort int
 TimeoutMS int
 ExpiresAt int64
 ExpectedGoalRevision int64
 ActorPrincipalID string
}
type RevokeGoalTCPProbeCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedGoalRevision int64
 ExpectedPermitRevision int64
 ActorPrincipalID string
}

const tcpProbePermitCols = `id,project_id,goal_id,project_workspace_id,source_id,plan_hash,plan_generation,
probe_kind,target_ipv4,target_port,timeout_ms,effect_class,status,revision,
approved_by,revoked_by,approved_at,expires_at,revoked_at`

func scanGoalTCPProbePermit(r scanner)(GoalTCPProbePermit,error){
 var p GoalTCPProbePermit
 var revoked sql.NullString
 err:=r.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,&p.SourceID,
  &p.PlanHash,&p.PlanGeneration,&p.ProbeKind,&p.TargetIPv4,&p.TargetPort,&p.TimeoutMS,
  &p.EffectClass,&p.Status,&p.Revision,&p.ApprovedBy,&revoked,
  &p.ApprovedAt,&p.ExpiresAt,&p.RevokedAt)
 if revoked.Valid{p.RevokedBy=&revoked.String}
 return p,err
}

// Validate a SINGLE literal RFC1918 IPv4 address. No hostnames, DNS,
// interface zones, loopback, link-local, IPv6, cloud metadata addresses,
// public IPs, port ranges or CIDR expansions are admitted by this first
// read-only probe class. Never let a later URL/parser reinterpret a target.
func canonicalPrivateProbeIPv4(input string)(string,error){
 addr,err:=netip.ParseAddr(input)
 if err!=nil||!addr.Is4()||!addr.IsPrivate()||addr.IsLoopback()||
   addr.IsLinkLocalUnicast()||addr.String()!=input{
  return "",fmt.Errorf("%w: an exact private IPv4 address is required",ErrInvalidCommand)
 }
 return addr.String(),nil
}

// ApproveGoalTCPProbe registers only an explicit future-capability intent;
// there is deliberately NO scanner, socket or background-task creation.
func(s *Service)ApproveGoalTCPProbe(ctx context.Context,c ApproveGoalTCPProbeCommand)(GoalTCPProbePermit,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.SourceID=strings.TrimSpace(c.SourceID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 ip,err:=canonicalPrivateProbeIPv4(c.TargetIPv4)
 if err!=nil{return GoalTCPProbePermit{},err}
 if c.ProjectID==""||c.GoalID==""||c.ActorPrincipalID==""||
  !goalSourceID.MatchString(c.SourceID)||c.TargetPort<1||c.TargetPort>65535||
  c.TimeoutMS<100||c.TimeoutMS>1500||c.ExpectedGoalRevision<1{
  return GoalTCPProbePermit{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gprobe");if err!=nil{return GoalTCPProbePermit{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return GoalTCPProbePermit{},err}
 var result GoalTCPProbePermit
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+24*60*60*1000{
   return fmt.Errorf("%w: probe grant expiry must be more than 5 minutes and at most 24 hours",ErrInvalidCommand)
  }
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var workspaceID,status,planRaw string
  var revision,planGeneration int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json,g.revision,g.plan_generation
    FROM project_continuous_goals g JOIN project_workspaces w
      ON w.id=g.project_workspace_id AND w.project_id=g.project_id AND w.status='active'
    JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
    WHERE g.id=? AND g.project_id=?`,project.WorkspaceID,c.GoalID,c.ProjectID).
    Scan(&workspaceID,&status,&planRaw,&revision,&planGeneration)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"||planGeneration<1{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  permitted:=false
  for _,source:=range plan.SourceIDs{if source==c.SourceID{permitted=true;break}}
  if !permitted{return ErrInvalidCommand}
  var existing int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tcp_probe_permits
    WHERE goal_id=? AND source_id=? AND status='approved'`,c.GoalID,c.SourceID).Scan(&existing)
  if err!=nil{return err}
  if existing!=0{return ErrInvalidTransition}
  result=GoalTCPProbePermit{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,ProjectWorkspaceID:workspaceID,
   SourceID:c.SourceID,PlanHash:hash,PlanGeneration:planGeneration,ProbeKind:"tcp_connect",
   TargetIPv4:ip,TargetPort:c.TargetPort,TimeoutMS:c.TimeoutMS,
   EffectClass:"network_tcp_connect",Status:"approved",Revision:1,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_permits(
    id,project_id,goal_id,project_workspace_id,source_id,plan_hash,plan_generation,probe_kind,
    target_ipv4,target_port,timeout_ms,effect_class,status,revision,
    approved_by,approved_at,expires_at)
    VALUES(?,?,?,?,?,?,?,'tcp_connect',?,? ,?,'network_tcp_connect','approved',1,?,?,?)`,
    idv,c.ProjectID,c.GoalID,workspaceID,c.SourceID,hash,planGeneration,
    result.TargetIPv4,result.TargetPort,result.TimeoutMS,result.ApprovedBy,
    result.ApprovedAt,result.ExpiresAt)
  if err!=nil{return err}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=? AND status='configured'`,
    now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":idv,"source_id":c.SourceID,
   "target_ipv4":ip,"target_port":c.TargetPort,"timeout_ms":c.TimeoutMS,
   "effect_class":"network_tcp_connect","expires_at":c.ExpiresAt,
   "goal_revision":revision+1,"probe_executed":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_probe_permit_approved",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPProbePermit{},err}
 return result,nil
}

// RevokeGoalTCPProbe prevents subsequent dispatch admissions. Actual in-flight
// network calls must obey their own bounded cancellation and expiry checks.
func(s *Service)RevokeGoalTCPProbe(ctx context.Context,c RevokeGoalTCPProbeCommand)(GoalTCPProbePermit,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPermitRevision<1{return GoalTCPProbePermit{},ErrInvalidCommand}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalTCPProbePermit{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var status string
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.revision FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&status,&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status=="retired"{return ErrInvalidTransition}
  p,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
   ` FROM project_goal_tcp_probe_permits
    WHERE id=? AND project_id=? AND goal_id=?`,c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if p.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  if p.Status!="approved"{return ErrInvalidTransition}
  change,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_probe_permits SET
    status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
    WHERE id=? AND status='approved' AND revision=?`,
    c.ActorPrincipalID,now,p.ID,p.Revision)
  if err!=nil{return err}
  n,err:=change.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  change,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=?`,
    now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err=change.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":p.ID,"source_id":p.SourceID,
   "effect_class":"network_tcp_connect","goal_revision":revision+1,
   "revoked_at":now,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_probe_permit_revoked",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPProbePermit{},err}
 return scanGoalTCPProbePermit(s.db.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
  ` FROM project_goal_tcp_probe_permits WHERE id=?`,c.PermitID))
}

func(s *Service)ListGoalTCPProbePermits(ctx context.Context,projectID,goalID string)([]GoalTCPProbePermit,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
   JOIN projects p ON p.id=g.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+tcpProbePermitCols+
  ` FROM project_goal_tcp_probe_permits
  WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 64`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalTCPProbePermit{}
 for rows.Next(){
  permit,err:=scanGoalTCPProbePermit(rows)
  if err!=nil{return nil,err}
  out=append(out,permit)
 }
 return out,rows.Err()
}
