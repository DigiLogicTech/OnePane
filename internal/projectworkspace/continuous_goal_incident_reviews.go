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

// GoalIncidentReview is an operator case referring to a TCP socket drift.
// Neither pending_review nor reviewed implies a verified security incident,
// a healthy device, or any authority to execute remediation.
type GoalIncidentReview struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 TCPAlertID string `json:"tcp_alert_id"`
 SourceID string `json:"source_id"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 Decision string `json:"decision"`
 OpenedBy string `json:"opened_by"`
 OpenedAt int64 `json:"opened_at"`
 ReviewedBy *string `json:"reviewed_by,omitempty"`
 ReviewedAt int64 `json:"reviewed_at"`
 DeviceHealth string `json:"device_health"`
 RemediationAuthorized bool `json:"remediation_authorized"`
}
type OpenGoalIncidentReviewCommand struct {
 ProjectID,GoalID,TCPAlertID,ActorPrincipalID string
 ExpectedAlertRevision int64
}
type CompleteGoalIncidentReviewCommand struct {
 ProjectID,GoalID,ReviewID,ActorPrincipalID,Decision string
 ExpectedRevision int64
}
const goalIncidentReviewColumns=`id,project_id,goal_id,project_workspace_id,tcp_alert_id,
 source_id,status,revision,decision,opened_by,opened_at,reviewed_by,reviewed_at`

func scanGoalIncidentReview(row scanner)(GoalIncidentReview,error){
 var item GoalIncidentReview
 var reviewer sql.NullString
 err:=row.Scan(&item.ID,&item.ProjectID,&item.GoalID,&item.ProjectWorkspaceID,
  &item.TCPAlertID,&item.SourceID,&item.Status,&item.Revision,
  &item.Decision,&item.OpenedBy,&item.OpenedAt,&reviewer,&item.ReviewedAt)
 if reviewer.Valid{item.ReviewedBy=&reviewer.String}
 item.DeviceHealth="unverified"
 return item,err
}

// OpenGoalIncidentReview never interprets a connectivity alert as a
// confirmed incident. A human explicitly selects an existing immutable
// alert, in the same canonical Goal and active Workspace.
func(s *Service)OpenGoalIncidentReview(ctx context.Context,
 c OpenGoalIncidentReviewCommand)(GoalIncidentReview,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.TCPAlertID=strings.TrimSpace(c.TCPAlertID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.TCPAlertID==""||
  c.ActorPrincipalID==""||c.ExpectedAlertRevision<1{
  return GoalIncidentReview{},ErrInvalidCommand
 }
 reviewID,err:=s.ids.New("gir");if err!=nil{return GoalIncidentReview{},err}
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalIncidentReview{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"||p.OperatingMode!=OperatingContinuousOperations{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,source string
  var alertRevision int64
  err=tx.QueryRowContext(ctx,`SELECT alert.project_workspace_id,alert.source_id,alert.revision
   FROM project_goal_tcp_reachability_alerts alert
   JOIN project_continuous_goals goal ON goal.id=alert.goal_id
    AND goal.project_id=alert.project_id
    AND goal.project_workspace_id=alert.project_workspace_id
   JOIN project_workspaces workspace ON workspace.id=goal.project_workspace_id
    AND workspace.project_id=goal.project_id AND workspace.status='active'
   WHERE alert.id=? AND alert.project_id=? AND alert.goal_id=?`,
   c.TCPAlertID,c.ProjectID,c.GoalID).Scan(&wsID,&source,&alertRevision)
  if err!=nil{return err}
  if alertRevision!=c.ExpectedAlertRevision{return ErrRevisionConflict}
  var oldID string
  err=tx.QueryRowContext(ctx,`SELECT id FROM project_goal_incident_reviews
   WHERE tcp_alert_id=?`,c.TCPAlertID).Scan(&oldID)
  if err==nil{return ErrInvalidTransition}
  if !errors.Is(err,sql.ErrNoRows){return err}
  now:=s.clock.UnixMilli()
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_incident_reviews(
   id,project_id,goal_id,project_workspace_id,tcp_alert_id,source_id,
   status,revision,decision,opened_by,opened_at)
   VALUES(?,?,?,?,?,?,'pending_review',1,'unassessed',?,?)`,
   reviewID,c.ProjectID,c.GoalID,wsID,c.TCPAlertID,source,c.ActorPrincipalID,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "review_id":reviewID,"goal_id":c.GoalID,"alert_id":c.TCPAlertID,
   "source_id":source,"device_health":"unverified",
   "incident_confirmed":false,"remediation_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_incident_review_opened",
   AggregateType:"project_goal_incident_review",AggregateID:reviewID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalIncidentReview{},err}
 return scanGoalIncidentReview(s.db.QueryRowContext(ctx,`SELECT `+
  goalIncidentReviewColumns+` FROM project_goal_incident_reviews WHERE id=?`,reviewID))
}

// A human decision is a terminal review classification ONLY. It never
// acknowledges an existing TCP alert or automatically closes external
// monitoring/recovery tasks.
func(s *Service)CompleteGoalIncidentReview(ctx context.Context,
 c CompleteGoalIncidentReviewCommand)(GoalIncidentReview,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.ReviewID=strings.TrimSpace(c.ReviewID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.ReviewID==""||
  c.ActorPrincipalID==""||c.ExpectedRevision<1||
  (c.Decision!="needs_investigation"&&c.Decision!="insufficient_evidence"&&
   c.Decision!="referred_out_of_band"){return GoalIncidentReview{},ErrInvalidCommand}
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalIncidentReview{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"||p.OperatingMode!=OperatingContinuousOperations{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  a,err:=scanGoalIncidentReview(tx.QueryRowContext(ctx,`SELECT `+
   goalIncidentReviewColumns+` FROM project_goal_incident_reviews review
   JOIN project_continuous_goals goal ON goal.id=review.goal_id
    AND goal.project_id=review.project_id
    AND goal.project_workspace_id=review.project_workspace_id
   JOIN project_workspaces workspace ON workspace.id=goal.project_workspace_id
    AND workspace.project_id=goal.project_id AND workspace.status='active'
   WHERE review.id=? AND review.project_id=? AND review.goal_id=?`,
   c.ReviewID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if a.Revision!=c.ExpectedRevision{return ErrRevisionConflict}
  if a.Status!="pending_review"{return ErrInvalidTransition}
  now:=s.clock.UnixMilli();if now<a.OpenedAt{now=a.OpenedAt}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_goal_incident_reviews
   SET status='reviewed',decision=?,reviewed_by=?,reviewed_at=?,revision=revision+1
   WHERE id=? AND project_id=? AND goal_id=? AND revision=? AND status='pending_review'`,
   c.Decision,c.ActorPrincipalID,now,c.ReviewID,c.ProjectID,c.GoalID,a.Revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "review_id":a.ID,"goal_id":a.GoalID,"alert_id":a.TCPAlertID,
   "decision":c.Decision,"device_health":"unverified",
   "remediation_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_incident_review_completed",
   AggregateType:"project_goal_incident_review",AggregateID:a.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalIncidentReview{},err}
 return scanGoalIncidentReview(s.db.QueryRowContext(ctx,`SELECT `+
  goalIncidentReviewColumns+` FROM project_goal_incident_reviews WHERE id=?`,c.ReviewID))
}

func(s *Service)ListGoalIncidentReviews(ctx context.Context,
 projectID,goalID string)([]GoalIncidentReview,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var valid int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals goal
  JOIN projects project ON project.id=goal.project_id AND project.status='active'
  JOIN workspaces tenant ON tenant.id=project.workspace_id AND tenant.status='active'
  JOIN project_workspaces workspace ON workspace.id=goal.project_workspace_id
   AND workspace.project_id=project.id AND workspace.status='active'
  WHERE goal.id=? AND goal.project_id=?`,goalID,projectID).Scan(&valid)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalIncidentReviewColumns+
  ` FROM project_goal_incident_reviews
  WHERE project_id=? AND goal_id=?
  ORDER BY opened_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalIncidentReview{}
 for rows.Next(){
  a,err:=scanGoalIncidentReview(rows)
  if err!=nil{return nil,err}
  out=append(out,a)
 }
 return out,rows.Err()
}
