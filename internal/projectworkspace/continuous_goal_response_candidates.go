package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalResponseCandidate is NON-EXECUTABLE intent, not a playbook permit.
// Existing Operation Coordinator approval/leases/verification must be used
// by a future implementation. Migration and mode switches grant no effects.
type GoalResponseCandidate struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 IncidentReviewID string `json:"incident_review_id"`
 TCPAlertID string `json:"tcp_alert_id"`
 SourceID string `json:"source_id"`
 GoalPlanHash string `json:"goal_plan_hash"`
 GoalPlanGeneration int64 `json:"goal_plan_generation"`
 IncidentReviewRevision int64 `json:"incident_review_revision"`
 ActionCode string `json:"action_code"`
 EffectClass string `json:"effect_class"`
 RiskClass string `json:"risk_class"`
 ExecutionAuthorized bool `json:"execution_authorized"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ProposedBy string `json:"proposed_by"`
 ProposedAt int64 `json:"proposed_at"`
 ExpiresAt int64 `json:"expires_at"`
 WithdrawnBy *string `json:"withdrawn_by,omitempty"`
 WithdrawnAt int64 `json:"withdrawn_at"`
}
type GoalResponseCandidateReadiness struct {
 CandidateID string `json:"candidate_id"`
 State string `json:"state"`
 ExecutionAuthorized bool `json:"execution_authorized"`
 Blockers []string `json:"blockers"`
}
type ProposeGoalResponseCandidateCommand struct {
 ProjectID,GoalID,IncidentReviewID,ActorPrincipalID,ActionCode string
 ExpectedGoalRevision,ExpectedReviewRevision,ExpiresAt int64
}
type WithdrawGoalResponseCandidateCommand struct {
 ProjectID,GoalID,CandidateID,ActorPrincipalID string
 ExpectedRevision int64
}
const goalResponseCandidateColumns=`id,project_id,goal_id,project_workspace_id,
 incident_review_id,tcp_alert_id,source_id,goal_plan_hash,goal_plan_generation,
 incident_review_revision,action_code,effect_class,risk_class,execution_authorized,
 status,revision,proposed_by,proposed_at,expires_at,withdrawn_by,withdrawn_at`

func scanGoalResponseCandidate(row scanner)(GoalResponseCandidate,error){
 var a GoalResponseCandidate
 var raw int
 var withdrawn sql.NullString
 err:=row.Scan(&a.ID,&a.ProjectID,&a.GoalID,&a.ProjectWorkspaceID,
  &a.IncidentReviewID,&a.TCPAlertID,&a.SourceID,&a.GoalPlanHash,
  &a.GoalPlanGeneration,&a.IncidentReviewRevision,&a.ActionCode,
  &a.EffectClass,&a.RiskClass,&raw,&a.Status,&a.Revision,&a.ProposedBy,
  &a.ProposedAt,&a.ExpiresAt,&withdrawn,&a.WithdrawnAt)
 if withdrawn.Valid{a.WithdrawnBy=&withdrawn.String}
 a.ExecutionAuthorized=raw!=0
 return a,err
}

// Propose is an operator's immutable review intent. It NEVER grants
// a process, tool, Agent, Node or device mutation capability.
func(s *Service)ProposeGoalResponseCandidate(ctx context.Context,
 c ProposeGoalResponseCandidateCommand)(GoalResponseCandidate,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.IncidentReviewID=strings.TrimSpace(c.IncidentReviewID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.IncidentReviewID==""||
  c.ActorPrincipalID==""||c.ActionCode!="restart_local_observation_worker"||
  c.ExpectedGoalRevision<1||c.ExpectedReviewRevision<1{
  return GoalResponseCandidate{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("grc");if err!=nil{return GoalResponseCandidate{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return GoalResponseCandidate{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<now+5*60000||c.ExpiresAt>now+24*60*60000{
   return ErrInvalidCommand
  }
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status,rawPlan string
  var goalRevision,goalGeneration int64
  err=tx.QueryRowContext(ctx,`SELECT goal.project_workspace_id,goal.status,
   goal.monitoring_plan_json,goal.revision,goal.plan_generation
   FROM project_continuous_goals goal
   JOIN project_workspaces ws ON ws.id=goal.project_workspace_id
    AND ws.project_id=goal.project_id AND ws.status='active'
   JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
   WHERE goal.id=? AND goal.project_id=?`,
   project.WorkspaceID,c.GoalID,c.ProjectID).
   Scan(&wsID,&status,&rawPlan,&goalRevision,&goalGeneration)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"||goalGeneration<1{return ErrInvalidTransition}
  plan,planHash,err:=goalReviewPlan(rawPlan);if err!=nil{return err}
  var alertID,reviewWS,reviewSource,decision,reviewStatus string
  var reviewRevision int64
  err=tx.QueryRowContext(ctx,`SELECT review.tcp_alert_id,
   review.project_workspace_id,review.source_id,review.decision,
   review.status,review.revision
   FROM project_goal_incident_reviews review
   JOIN project_goal_tcp_reachability_alerts alert
    ON alert.id=review.tcp_alert_id AND alert.project_id=review.project_id
    AND alert.goal_id=review.goal_id
    AND alert.project_workspace_id=review.project_workspace_id
    AND alert.source_id=review.source_id
   WHERE review.id=? AND review.project_id=? AND review.goal_id=?`,
   c.IncidentReviewID,c.ProjectID,c.GoalID).
   Scan(&alertID,&reviewWS,&reviewSource,&decision,&reviewStatus,&reviewRevision)
  if err!=nil{return err}
  if reviewRevision!=c.ExpectedReviewRevision{return ErrRevisionConflict}
  if reviewWS!=wsID||reviewStatus!="reviewed"||
    decision!="needs_investigation"{return ErrInvalidTransition}
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==reviewSource{allowed=true;break}}
  if !allowed{return ErrInvalidTransition}
  var existing int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_response_candidates
   WHERE incident_review_id=? AND status='proposed'`,c.IncidentReviewID).
   Scan(&existing)
  if err!=nil{return err}
  if existing!=0{return ErrInvalidTransition}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_response_candidates(
    id,project_id,goal_id,project_workspace_id,incident_review_id,
    tcp_alert_id,source_id,goal_plan_hash,goal_plan_generation,
    incident_review_revision,action_code,effect_class,risk_class,
    execution_authorized,status,revision,proposed_by,proposed_at,expires_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,
    ?,'host_process_control','requires_separate_approval',0,'proposed',1,?,?,?)`,
   idv,c.ProjectID,c.GoalID,wsID,c.IncidentReviewID,alertID,reviewSource,
   planHash,goalGeneration,reviewRevision,c.ActionCode,c.ActorPrincipalID,now,c.ExpiresAt)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "candidate_id":idv,"incident_review_id":c.IncidentReviewID,
   "action_code":c.ActionCode,"execution_authorized":false,
   "no_adapter_attached":true,"no_effect_performed":true,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_response_intent_proposed",
   AggregateType:"project_goal_response_candidate",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalResponseCandidate{},err}
 return s.GoalResponseCandidate(ctx,c.ProjectID,c.GoalID,idv)
}

func(s *Service)GoalResponseCandidate(ctx context.Context,
 projectID,goalID,id string)(GoalResponseCandidate,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""||
  strings.TrimSpace(id)==""{return GoalResponseCandidate{},ErrInvalidCommand}
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&active)
 if err!=nil{return GoalResponseCandidate{},err}
 return scanGoalResponseCandidate(s.db.QueryRowContext(ctx,
  `SELECT `+goalResponseCandidateColumns+`
   FROM project_goal_response_candidates
   WHERE id=? AND project_id=? AND goal_id=?`,id,projectID,goalID))
}

func(s *Service)ListGoalResponseCandidates(ctx context.Context,
 projectID,goalID string)([]GoalResponseCandidate,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&active)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalResponseCandidateColumns+`
  FROM project_goal_response_candidates WHERE project_id=? AND goal_id=?
  ORDER BY proposed_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalResponseCandidate{}
 for rows.Next(){v,e:=scanGoalResponseCandidate(rows);if e!=nil{return nil,e};out=append(out,v)}
 return out,rows.Err()
}

func(s *Service)WithdrawGoalResponseCandidate(ctx context.Context,
 c WithdrawGoalResponseCandidateCommand)(GoalResponseCandidate,error){
 if strings.TrimSpace(c.ProjectID)==""||strings.TrimSpace(c.GoalID)==""||
  strings.TrimSpace(c.CandidateID)==""||strings.TrimSpace(c.ActorPrincipalID)==""||
  c.ExpectedRevision<1{return GoalResponseCandidate{},ErrInvalidCommand}
 eventID,err:=s.ids.New("evt");if err!=nil{return GoalResponseCandidate{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var revision int64
  var status string
  var created int64
  err=tx.QueryRowContext(ctx,`SELECT c.revision,c.status,c.proposed_at
    FROM project_goal_response_candidates c
    JOIN project_continuous_goals g ON g.id=c.goal_id
      AND g.project_id=c.project_id
      AND g.project_workspace_id=c.project_workspace_id
    JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=g.project_id AND w.status='active'
    WHERE c.id=? AND c.project_id=? AND c.goal_id=?`,
   c.CandidateID,c.ProjectID,c.GoalID).Scan(&revision,&status,&created)
  if err!=nil{return err}
  if revision!=c.ExpectedRevision{return ErrRevisionConflict}
  if status!="proposed"{return ErrInvalidTransition}
  now:=s.clock.UnixMilli();if now<created{now=created}
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_response_candidates
   SET status='withdrawn',revision=revision+1,withdrawn_by=?,withdrawn_at=?
   WHERE id=? AND project_id=? AND goal_id=? AND revision=? AND status='proposed'`,
   c.ActorPrincipalID,now,c.CandidateID,c.ProjectID,c.GoalID,revision)
  if err!=nil{return err}
  affected,err:=result.RowsAffected();if err!=nil{return err}
  if affected!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "candidate_id":c.CandidateID,"no_effect_performed":true,
   "execution_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_response_intent_withdrawn",
   AggregateType:"project_goal_response_candidate",AggregateID:c.CandidateID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalResponseCandidate{},err}
 return s.GoalResponseCandidate(ctx,c.ProjectID,c.GoalID,c.CandidateID)
}

// Readiness reports blockers, NEVER returns an execution token or a
// pass/fail that a caller can treat as authorization. It intentionally lacks
// ANY dispatch, task or mutation method. A future adapter requires a new
// independently scoped approval, not a conversion of this old record.
func(s *Service)GoalResponseCandidateReadiness(ctx context.Context,
 projectID,goalID,id string)(GoalResponseCandidateReadiness,error){
 c,err:=s.GoalResponseCandidate(ctx,projectID,goalID,id)
 if err!=nil{return GoalResponseCandidateReadiness{},err}
 out:=GoalResponseCandidateReadiness{
  CandidateID:id,State:"blocked",Blockers:[]string{
   "not_an_execution_grant",
   "no_validated_reversible_effect_adapter",
   "no_independent_target_or_postcondition_verification",
  },
 }
 if c.Status!="proposed"{out.Blockers=append(out.Blockers,"candidate_withdrawn")}
 if c.ExpiresAt<=s.clock.UnixMilli(){out.Blockers=append(out.Blockers,"candidate_expired")}
 var rawPlan,goalStatus,reviewStatus,reviewDecision string
 var planGeneration,reviewRevision int64
 err=s.db.QueryRowContext(ctx,`SELECT g.monitoring_plan_json,g.plan_generation,g.status,
  r.status,r.decision,r.revision
  FROM project_continuous_goals g
  JOIN project_goal_incident_reviews r ON r.goal_id=g.id AND r.project_id=g.project_id
   AND r.project_workspace_id=g.project_workspace_id
  WHERE g.id=? AND g.project_id=? AND r.id=?`,
  goalID,projectID,c.IncidentReviewID).
  Scan(&rawPlan,&planGeneration,&goalStatus,&reviewStatus,&reviewDecision,&reviewRevision)
 if err!=nil{return GoalResponseCandidateReadiness{},err}
 _,hash,e:=goalReviewPlan(rawPlan)
 if e!=nil||hash!=c.GoalPlanHash||planGeneration!=c.GoalPlanGeneration||
  goalStatus!="configured"{out.Blockers=append(out.Blockers,"goal_scope_changed")}
 if reviewStatus!="reviewed"||reviewDecision!="needs_investigation"||
  reviewRevision!=c.IncidentReviewRevision{
  out.Blockers=append(out.Blockers,"incident_review_changed")
 }
 project,err:=s.repo.Project(ctx,projectID)
 if err!=nil{return GoalResponseCandidateReadiness{},err}
 if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
  out.Blockers=append(out.Blockers,"project_mode_changed")
 }
 // A candidate never authorises execution, but operator diagnostics must
 // still show when the original probe authority has expired or been
 // withdrawn. The original permit cannot be revived by an old case.
 var livePermit int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*)
   FROM project_goal_tcp_reachability_alerts alert
   JOIN project_goal_tcp_probe_attempts attempt ON attempt.id=alert.attempt_id
    AND attempt.project_id=alert.project_id AND attempt.goal_id=alert.goal_id
    AND attempt.project_workspace_id=alert.project_workspace_id
    AND attempt.source_id=alert.source_id
   JOIN project_goal_tcp_probe_permits permit ON permit.id=attempt.permit_id
    AND permit.project_id=attempt.project_id AND permit.goal_id=attempt.goal_id
    AND permit.project_workspace_id=attempt.project_workspace_id
    AND permit.source_id=attempt.source_id
    AND permit.revision=attempt.permit_revision
    AND permit.plan_hash=? AND permit.plan_generation=?
    AND permit.status='approved' AND permit.expires_at>?
   JOIN principals reviewer ON reviewer.id=permit.approved_by
    AND reviewer.principal_type='human' AND reviewer.status='active'
   JOIN workspace_memberships member ON member.principal_id=reviewer.id
    AND member.workspace_id=? AND member.status='active'
   WHERE alert.id=? AND alert.project_id=? AND alert.goal_id=?
    AND alert.project_workspace_id=? AND alert.source_id=?`,
   c.GoalPlanHash,c.GoalPlanGeneration,s.clock.UnixMilli(),
   project.WorkspaceID,c.TCPAlertID,projectID,goalID,
   c.ProjectWorkspaceID,c.SourceID).Scan(&livePermit)
 if err!=nil{return GoalResponseCandidateReadiness{},err}
 if livePermit!=1{out.Blockers=append(out.Blockers,"original_observation_permission_inactive")}
 var activeProposer int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM principals human
  JOIN workspace_memberships member ON member.principal_id=human.id
   AND member.workspace_id=? AND member.status='active'
  WHERE human.id=? AND human.status='active' AND human.principal_type='human'`,
  project.WorkspaceID,c.ProposedBy).Scan(&activeProposer)
 if err!=nil{return GoalResponseCandidateReadiness{},err}
 if activeProposer!=1{out.Blockers=append(out.Blockers,"proposing_operator_inactive")}
 return out,nil
}
