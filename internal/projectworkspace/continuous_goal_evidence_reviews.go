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

// GoalEvidenceReviewRun is an audit of existing receipt *metadata* only.
// Even recent evidence does not establish that a device is safe or monitored.
type GoalEvidenceReviewRun struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 PermitID string `json:"permit_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 CadenceWindow int64 `json:"cadence_window"`
 EvidenceState string `json:"evidence_state"`
 LastObservedAt int64 `json:"last_observed_at"`
 GoalRevision int64 `json:"goal_revision"`
 PermitRevision int64 `json:"permit_revision"`
 ReviewedBy string `json:"reviewed_by"`
 ReviewedAt int64 `json:"reviewed_at"`
 MonitoringActive bool `json:"monitoring_active"`
}

type ReviewGoalEvidenceCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedGoalRevision int64
 ExpectedPermitRevision int64
 ActorPrincipalID string
}

const goalReviewRunColumns = `id,project_id,goal_id,permit_id,project_workspace_id,source_id,cadence_window,evidence_state,last_observed_at,goal_revision,permit_revision,reviewed_by,reviewed_at`

func scanGoalEvidenceReviewRun(row scanner)(GoalEvidenceReviewRun,error){
 var r GoalEvidenceReviewRun
 err:=row.Scan(&r.ID,&r.ProjectID,&r.GoalID,&r.PermitID,&r.ProjectWorkspaceID,
  &r.SourceID,&r.CadenceWindow,&r.EvidenceState,&r.LastObservedAt,
  &r.GoalRevision,&r.PermitRevision,&r.ReviewedBy,&r.ReviewedAt)
 // There is no active probe, scheduled monitor or verified device health
 // in this record; monitoring_active is explicitly false.
 return r,err
}

func reviewFreshnessMillis(cadence int64) int64{
 minutes:=cadence*2
 if minutes<15{minutes=15}
 if minutes>24*60{minutes=24*60}
 return minutes*60*1000
}

// ReviewGoalEvidence takes exactly one bounded, repeatable snapshot of
// previously attached immutable receipt timestamps. Same permit + cadence
// window yields the same result across retries and process restarts.
// No sensor, tool, network, LLM, or remediation API is ever invoked.
func(s *Service)ReviewGoalEvidence(ctx context.Context,c ReviewGoalEvidenceCommand)(GoalEvidenceReviewRun,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPermitRevision<1{
  return GoalEvidenceReviewRun{},ErrInvalidCommand
 }
 now:=s.clock.UnixMilli()
 idv,err:=s.ids.New("grun");if err!=nil{return GoalEvidenceReviewRun{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return GoalEvidenceReviewRun{},err}
 var completed GoalEvidenceReviewRun
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  // Refresh the clock AFTER acquiring the writer transaction: an expiring
  // permit cannot remain usable just because SQLite writer contention
  // delayed its last controllable review-admission boundary.
  now=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  // Both Agent and human reviews are bounded to the existing tenant
  // membership, but only a human can CREATE or REVOKE the permit itself.
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status,planRaw string
  var goalRevision int64
  var cadence int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json,
      g.revision,g.cadence_minutes
    FROM project_continuous_goals g JOIN project_workspaces w
     ON w.id=g.project_workspace_id AND w.project_id=g.project_id AND w.status='active'
    WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).
    Scan(&wsID,&status,&planRaw,&goalRevision,&cadence)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"&&status!="active"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  permit,err:=scanReviewPermit(tx.QueryRowContext(ctx,`SELECT `+reviewPermitColumns+
    ` FROM project_goal_evidence_review_permits
      WHERE id=? AND project_id=? AND goal_id=?`,c.PermitID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if permit.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  if permit.Status!="approved"||permit.ExpiresAt<=now||
    permit.EffectClass!="existing_receipt_metadata"||
    permit.ProjectWorkspaceID!=wsID||permit.PlanHash!=hash{
   return ErrInvalidTransition
  }
  inPlan:=false
  for _,source:=range plan.SourceIDs{if source==permit.SourceID{inPlan=true;break}}
  if !inPlan{return ErrInvalidTransition}
  if cadence<5||cadence>10080{return ErrInvalidCommand}
  window:=now/(cadence*60*1000)
  // A duplicate within the same cadence period must return the original
  // immutable receipt instead of evaluating an evolving source twice.
  old,err:=scanGoalEvidenceReviewRun(tx.QueryRowContext(ctx,
   `SELECT `+goalReviewRunColumns+` FROM project_goal_evidence_review_runs
    WHERE permit_id=? AND cadence_window=?`,permit.ID,window))
  if err==nil{completed=old;return nil}
  if !errors.Is(err,sql.ErrNoRows){return err}
  var latest sql.NullInt64
  err=tx.QueryRowContext(ctx,`SELECT MAX(observed_at) FROM project_goal_observation_receipts
    WHERE goal_id=? AND source_id=?`,c.GoalID,permit.SourceID).Scan(&latest)
  if err!=nil{return err}
  observed:=int64(0)
  if latest.Valid{observed=latest.Int64}
  evidenceState:="missing"
  if observed>0{
   evidenceState="recent_attached"
   if observed>now||now-observed>reviewFreshnessMillis(cadence){
    evidenceState="stale_attached"
   }
  }
  completed=GoalEvidenceReviewRun{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,PermitID:permit.ID,
   ProjectWorkspaceID:wsID,SourceID:permit.SourceID,
   CadenceWindow:window,EvidenceState:evidenceState,LastObservedAt:observed,
   GoalRevision:goalRevision,PermitRevision:permit.Revision,
   ReviewedBy:c.ActorPrincipalID,ReviewedAt:now,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_evidence_review_runs(
   id,project_id,goal_id,permit_id,project_workspace_id,source_id,
   cadence_window,evidence_state,last_observed_at,goal_revision,permit_revision,
   reviewed_by,reviewed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
   completed.ID,completed.ProjectID,completed.GoalID,completed.PermitID,
   completed.ProjectWorkspaceID,completed.SourceID,completed.CadenceWindow,
   completed.EvidenceState,completed.LastObservedAt,completed.GoalRevision,
   completed.PermitRevision,completed.ReviewedBy,completed.ReviewedAt)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":permit.ID,"source_id":permit.SourceID,
   "evidence_state":evidenceState,"cadence_window":window,
   "review_run_id":completed.ID,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_receipt_metadata_reviewed",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalEvidenceReviewRun{},err}
 return completed,nil
}

// Historical review metadata remains available for audit after revocation,
// while each new review requires a freshly approved current permit.
func(s *Service)ListGoalEvidenceReviewRuns(ctx context.Context,projectID,goalID string)([]GoalEvidenceReviewRun,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{return nil,ErrInvalidCommand}
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id AND w.project_id=p.id
    AND w.status='active'
  WHERE g.id=? AND g.project_id=? LIMIT 1`,goalID,projectID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalReviewRunColumns+
   ` FROM project_goal_evidence_review_runs
     WHERE goal_id=? AND project_id=? ORDER BY reviewed_at DESC,id DESC LIMIT 100`,
  goalID,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 results:=make([]GoalEvidenceReviewRun,0)
 for rows.Next(){
  item,err:=scanGoalEvidenceReviewRun(rows)
  if err!=nil{return nil,err}
  results=append(results,item)
 }
 return results,rows.Err()
}

// Evidence review is a metadata assessment, not a verified health check,
// and MUST NOT modify continuous goal status, health, next_due_at or
// last_verified_at. Those transitions require a separate monitor and
// postcondition-verification implementation.
func (r GoalEvidenceReviewRun) ValidateEvidenceOnly() error {
 if r.MonitoringActive||r.EvidenceState=="healthy"||r.EvidenceState=="verified"{
  return fmt.Errorf("receipt review cannot claim active monitoring or verified health")
 }
 return nil
}
