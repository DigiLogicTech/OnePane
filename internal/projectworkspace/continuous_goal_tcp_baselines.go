package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalTCPBaselineRevision is an immutable operator expectation for an exact
// source AND permit. It is not a claim that the protected system is healthy.
type GoalTCPBaselineRevision struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 PermitID string `json:"permit_id"`
 SourceID string `json:"source_id"`
 PlanHash string `json:"plan_hash"`
 ExpectedTCP string `json:"expected_tcp"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
}

type SetGoalTCPBaselineCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedTCP string
 ExpectedGoalRevision int64
 ExpectedBaselineRevision int64
 ActorPrincipalID string
}

const goalTCPBaselineCols=`id,project_id,goal_id,project_workspace_id,permit_id,
source_id,plan_hash,expected_tcp,revision,approved_by,approved_at`

func scanTCPBaseline(r scanner)(GoalTCPBaselineRevision,error){
 var b GoalTCPBaselineRevision
 err:=r.Scan(&b.ID,&b.ProjectID,&b.GoalID,&b.ProjectWorkspaceID,
  &b.PermitID,&b.SourceID,&b.PlanHash,&b.ExpectedTCP,
  &b.Revision,&b.ApprovedBy,&b.ApprovedAt)
 return b,err
}

func(s *Service)SetGoalTCPBaseline(ctx context.Context,c SetGoalTCPBaselineCommand)(GoalTCPBaselineRevision,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedBaselineRevision<0||
  (c.ExpectedTCP!="connected"&&c.ExpectedTCP!="not_connected"){
  return GoalTCPBaselineRevision{},ErrInvalidCommand
 }
 baselineID,err:=s.ids.New("gtcpbase")
 if err!=nil{return GoalTCPBaselineRevision{},err}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalTCPBaselineRevision{},err}
 var output GoalTCPBaselineRevision
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var status,workspaceID,planRaw string
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.project_workspace_id,g.monitoring_plan_json,g.revision
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
   WHERE g.id=? AND g.project_id=?`,
   project.WorkspaceID,c.GoalID,c.ProjectID).Scan(&status,&workspaceID,&planRaw,&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  permit,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,`SELECT `+tcpProbePermitCols+
   ` FROM project_goal_tcp_probe_permits WHERE id=? AND project_id=? AND goal_id=?`,
   c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if permit.Status!="approved"||permit.ExpiresAt<=now||permit.PlanHash!=hash||
    permit.ProjectWorkspaceID!=workspaceID||
    permit.EffectClass!="network_tcp_connect"||permit.ProbeKind!="tcp_connect"{
   return ErrInvalidTransition
  }
  if _,err=canonicalPrivateProbeIPv4(permit.TargetIPv4);err!=nil{return ErrInvalidTransition}
  allowed:=false
  for _,id:=range plan.SourceIDs{if id==permit.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidTransition}
  var activeApprover int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals approver
    JOIN workspace_memberships membership
     ON membership.principal_id=approver.id AND membership.workspace_id=?
     AND membership.status='active'
    WHERE approver.id=? AND approver.principal_type='human'
     AND approver.status='active'`,project.WorkspaceID,permit.ApprovedBy).Scan(&activeApprover)
  if err!=nil{return ErrPrincipalIneligible}
  var current int64
  err=tx.QueryRowContext(ctx,`SELECT revision FROM project_goal_tcp_baseline_heads
   WHERE goal_id=? AND source_id=?`,c.GoalID,permit.SourceID).Scan(&current)
  if errors.Is(err,sql.ErrNoRows){current=0
  }else if err!=nil{return err}
  if current!=c.ExpectedBaselineRevision{return ErrRevisionConflict}
  output=GoalTCPBaselineRevision{
   ID:baselineID,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:workspaceID,PermitID:permit.ID,SourceID:permit.SourceID,
   PlanHash:hash,ExpectedTCP:c.ExpectedTCP,Revision:current+1,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_baseline_revisions(
    id,project_id,goal_id,project_workspace_id,permit_id,source_id,
    plan_hash,expected_tcp,revision,approved_by,approved_at)
    VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
    output.ID,output.ProjectID,output.GoalID,output.ProjectWorkspaceID,
    output.PermitID,output.SourceID,output.PlanHash,output.ExpectedTCP,
    output.Revision,output.ApprovedBy,output.ApprovedAt)
  if err!=nil{return err}
  if current==0{
   _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_baseline_heads(
    goal_id,source_id,revision,baseline_id) VALUES(?,?,?,?)`,
    c.GoalID,permit.SourceID,output.Revision,output.ID)
  }else{
   changed,e:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_baseline_heads
    SET revision=?,baseline_id=? WHERE goal_id=? AND source_id=? AND revision=?`,
    output.Revision,output.ID,c.GoalID,permit.SourceID,current)
   err=e
   if err==nil{
    n,e:=changed.RowsAffected()
    if e!=nil{return e}
    if n!=1{return ErrRevisionConflict}
   }
  }
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
   "goal_id":c.GoalID,"baseline_id":output.ID,"source_id":permit.SourceID,
   "permit_id":permit.ID,"expected_tcp":c.ExpectedTCP,
   "baseline_revision":output.Revision,"goal_revision":revision+1,
   "verified_device_health":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_baseline_revised",
   AggregateType:"project_goal_tcp_baseline",AggregateID:output.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPBaselineRevision{},err}
 return output,nil
}

// GoalTCPReachabilityAssessment is a strictly local comparison to the
// LAST bounded TCP handshake. It cannot assert device health/zero trust
// compliance and it cannot dispatch network traffic or remediation.
type GoalTCPReachabilityAssessment struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 SourceID string `json:"source_id"`
 ExpectedTCP string `json:"expected_tcp"`
 ObservedTCP string `json:"observed_tcp"`
 LastTestAt int64 `json:"last_test_at"`
 AssessedAt int64 `json:"assessed_at"`
 Assessment string `json:"assessment"`
 BaselineRevision int64 `json:"baseline_revision"`
 MonitoringActive bool `json:"monitoring_active"`
 DeviceHealth string `json:"device_health"`
}

func(s *Service)GoalTCPReachabilityAssessment(ctx context.Context,projectID,goalID,sourceID string)(GoalTCPReachabilityAssessment,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 sourceID=strings.TrimSpace(sourceID)
 if projectID==""||goalID==""||!goalSourceID.MatchString(sourceID){
  return GoalTCPReachabilityAssessment{},ErrInvalidCommand
 }
 now:=s.clock.UnixMilli()
 out:=GoalTCPReachabilityAssessment{
  ProjectID:projectID,GoalID:goalID,SourceID:sourceID,
  AssessedAt:now,Assessment:"unconfigured_baseline",
  MonitoringActive:false,DeviceHealth:"unverified",
 }
 var goalStatus,planRaw,workspaceID string
 var cadence int64
 err:=s.db.QueryRowContext(ctx,`SELECT g.status,g.monitoring_plan_json,
   g.project_workspace_id,g.cadence_minutes FROM project_continuous_goals g
   JOIN projects p ON p.id=g.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,goalID,projectID).
  Scan(&goalStatus,&planRaw,&workspaceID,&cadence)
 if err!=nil{return GoalTCPReachabilityAssessment{},err}
 plan,hash,err:=goalReviewPlan(planRaw)
 if err!=nil{return GoalTCPReachabilityAssessment{},err}
 planSource:=false
 for _,id:=range plan.SourceIDs{if id==sourceID{planSource=true;break}}
 if !planSource{return out,nil}
 var expected,permitID,planHash,baselineWorkspace string
 err=s.db.QueryRowContext(ctx,`SELECT b.expected_tcp,b.permit_id,b.plan_hash,b.project_workspace_id,
  h.revision FROM project_goal_tcp_baseline_heads h
  JOIN project_goal_tcp_baseline_revisions b ON b.id=h.baseline_id
  WHERE h.goal_id=? AND h.source_id=? AND b.project_id=?`,
  goalID,sourceID,projectID).Scan(&expected,&permitID,&planHash,
  &baselineWorkspace,&out.BaselineRevision)
 if errors.Is(err,sql.ErrNoRows){return out,nil}
 if err!=nil{return GoalTCPReachabilityAssessment{},err}
 out.ExpectedTCP=expected
 if baselineWorkspace!=workspaceID||planHash!=hash||goalStatus!="configured"{
  out.Assessment="baseline_scope_inactive";return out,nil
 }
 var permitStatus string
 var permitExpiry int64
 err=s.db.QueryRowContext(ctx,`SELECT status,expires_at FROM
  project_goal_tcp_probe_permits WHERE id=? AND project_id=? AND goal_id=?
  AND source_id=? AND project_workspace_id=? AND plan_hash=?
  AND effect_class='network_tcp_connect'`,
  permitID,projectID,goalID,sourceID,workspaceID,hash).
  Scan(&permitStatus,&permitExpiry)
 if errors.Is(err,sql.ErrNoRows){out.Assessment="permission_inactive";return out,nil}
 if err!=nil{return GoalTCPReachabilityAssessment{},err}
 if permitStatus!="approved"||permitExpiry<=now{
  out.Assessment="permission_inactive";return out,nil
 }
 var outcome string
 var last int64
 err=s.db.QueryRowContext(ctx,`SELECT status,claimed_at FROM
  project_goal_tcp_probe_attempts WHERE goal_id=? AND project_id=?
  AND source_id=? AND permit_id=?
  ORDER BY claimed_at DESC,id DESC LIMIT 1`,
  goalID,projectID,sourceID,permitID).Scan(&outcome,&last)
 if errors.Is(err,sql.ErrNoRows){
  out.Assessment="missing_evidence";return out,nil
 }
 if err!=nil{return GoalTCPReachabilityAssessment{},err}
 out.ObservedTCP=outcome
 out.LastTestAt=last
 if last>now||now-last>reviewFreshnessMillis(cadence){
  out.Assessment="stale_test";return out,nil
 }
 if outcome=="claimed"||outcome=="cancelled"||outcome=="timed_out"{
  out.Assessment="inconclusive";return out,nil
 }
 if outcome!=expected{
  out.Assessment="unexpected_tcp_reachability"
 }else{
  out.Assessment="matches_last_tcp_test"
 }
 return out,nil
}

func(s *Service)ListGoalTCPBaselines(ctx context.Context,projectID,goalID string)([]GoalTCPBaselineRevision,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalTCPBaselineCols+
  ` FROM project_goal_tcp_baseline_revisions WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalTCPBaselineRevision{}
 for rows.Next(){
  row,err:=scanTCPBaseline(rows)
  if err!=nil{return nil,err}
  out=append(out,row)
 }
 return out,rows.Err()
}

func (b GoalTCPBaselineRevision) ValidateExpectation() error {
 if b.ExpectedTCP!="connected"&&b.ExpectedTCP!="not_connected"{
  return fmt.Errorf("invalid TCP reachability baseline expectation")
 }
 return nil
}
