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

// GoalTCPObservationFence is an operator-owned, per-Goal pause latch.
// An absent row means no additional pause; it NEVER grants a TCP permit.
// Resume only removes this additional denial and requires fresh human action.
type GoalTCPObservationFence struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ChangedBy string `json:"changed_by"`
 ChangedAt int64 `json:"changed_at"`
 ObservationAuthorized bool `json:"observation_authorized"`
}
type SetGoalTCPObservationFenceCommand struct {
 ProjectID,GoalID,ActorPrincipalID,TargetStatus string
 // Non-empty only for a separately approved Operation Coordinator tool.
 // A direct human API call is independently authenticated as project.write.
 AttachedTaskID string
 ExpectedGoalRevision,ExpectedFenceRevision int64
}
func scanGoalTCPObservationFence(row scanner)(GoalTCPObservationFence,error){
 var a GoalTCPObservationFence
 err:=row.Scan(&a.ProjectID,&a.GoalID,&a.ProjectWorkspaceID,
  &a.Status,&a.Revision,&a.ChangedBy,&a.ChangedAt)
 // A status of running still requires a distinct, live human manual +
 // recurring permit, runtime admission, and one exact cadence window.
 return a,err
}

// GetGoalTCPObservationFence returns an explicit non-authorising status,
// even when no operator has ever created a pause latch.
func(s *Service)GetGoalTCPObservationFence(ctx context.Context,
 projectID,goalID string)(GoalTCPObservationFence,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return GoalTCPObservationFence{},ErrInvalidCommand}
 var wsID string
 err:=s.db.QueryRowContext(ctx,`SELECT g.project_workspace_id
  FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces ws ON ws.id=g.project_workspace_id
   AND ws.project_id=p.id AND ws.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&wsID)
 if err!=nil{return GoalTCPObservationFence{},err}
 a,err:=scanGoalTCPObservationFence(s.db.QueryRowContext(ctx,
  `SELECT project_id,goal_id,project_workspace_id,status,revision,changed_by,changed_at
   FROM project_goal_tcp_observation_fences WHERE project_id=? AND goal_id=?`,
  projectID,goalID))
 if errors.Is(err,sql.ErrNoRows){
  return GoalTCPObservationFence{
   ProjectID:projectID,GoalID:goalID,ProjectWorkspaceID:wsID,
   Status:"running",Revision:0,
  },nil
 }
 return a,err
}

// SetGoalTCPObservationFence changes ONLY future unattended TCP scheduling.
// The human cannot use this method to create a permit, raise frequency,
// turn on L5, restart a process or cancel a DialContext already in flight.
func(s *Service)SetGoalTCPObservationFence(ctx context.Context,
 c SetGoalTCPObservationFenceCommand)(GoalTCPObservationFence,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedFenceRevision<0||
  (c.TargetStatus!="paused"&&c.TargetStatus!="running"){
  return GoalTCPObservationFence{},ErrInvalidCommand
 }
 eventID,err:=s.ids.New("evt");if err!=nil{return GoalTCPObservationFence{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status string
  var goalRevision int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.revision
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,
   c.GoalID,c.ProjectID).Scan(&wsID,&status,&goalRevision)
  if err!=nil{return err}
  if c.ExpectedGoalRevision!=goalRevision{return ErrRevisionConflict}
  // A paused Goal's own status is a separate, stronger stop: this latch
  // cannot reactivate it by modifying only the worker's dispatch fence.
  if status!="configured"{return ErrInvalidTransition}
  if c.AttachedTaskID!=""{
   // Recheck canonical Task ownership in the SAME writer transaction as
   // the dispatch-fence mutation. A Task reassigned after Gateway policy
   // evaluation may not borrow another Project's approved observer action.
   var scoped int
   err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks t
    WHERE t.id=? AND t.workspace_id=? AND t.project_id=?
     AND t.project_workspace_id=? AND t.state='running'`,
    c.AttachedTaskID,project.WorkspaceID,c.ProjectID,wsID).Scan(&scoped)
   if err!=nil{return err}
   if scoped!=1{return ErrInvalidTransition}
  }
  var oldStatus string
  var oldRevision int64
  err=tx.QueryRowContext(ctx,`SELECT status,revision
   FROM project_goal_tcp_observation_fences WHERE goal_id=? AND project_id=?`,
   c.GoalID,c.ProjectID).Scan(&oldStatus,&oldRevision)
  if errors.Is(err,sql.ErrNoRows){oldStatus="running";oldRevision=0
  }else if err!=nil{return err}
  if oldRevision!=c.ExpectedFenceRevision{return ErrRevisionConflict}
  if oldStatus==c.TargetStatus{return ErrInvalidTransition}
  now:=s.clock.UnixMilli()
  if oldRevision==0{
   if c.TargetStatus!="paused"{return ErrInvalidTransition}
   _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tcp_observation_fences(
    goal_id,project_id,project_workspace_id,status,revision,changed_by,changed_at)
    VALUES(?,?,?,'paused',1,?,?)`,c.GoalID,c.ProjectID,wsID,c.ActorPrincipalID,now)
  }else{
   result,e:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_observation_fences
    SET status=?,revision=revision+1,changed_by=?,changed_at=?
    WHERE goal_id=? AND project_id=? AND status=? AND revision=?`,
    c.TargetStatus,c.ActorPrincipalID,now,
    c.GoalID,c.ProjectID,oldStatus,oldRevision)
   if e!=nil{return e}
   n,e:=result.RowsAffected();if e!=nil{return e}
   if n!=1{return ErrRevisionConflict}
  }
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"previous_status":oldStatus,
   "status":c.TargetStatus,"revision":oldRevision+1,
   "creates_network_permission":false,"cancel_inflight":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_observer_"+c.TargetStatus,
   AggregateType:"project_goal_tcp_observation_fence",
   AggregateID:c.GoalID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTCPObservationFence{},err}
 return s.GetGoalTCPObservationFence(ctx,c.ProjectID,c.GoalID)
}

func(s *Service)tcpObservationPaused(ctx context.Context,projectID,goalID string)(bool,error){
 var blocked int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tcp_observation_fences
  WHERE project_id=? AND goal_id=? AND status='paused'`,projectID,goalID).
  Scan(&blocked)
 return blocked!=0,err
}
