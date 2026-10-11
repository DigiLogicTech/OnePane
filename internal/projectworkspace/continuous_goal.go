package projectworkspace

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "regexp"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/event"
    "github.com/DigiLogicTech/OnePane/internal/storage"
)

// A Level 5 goal is initially CONFIGURED and UNOBSERVED. Registration alone
// never activates probes or authorises network/host changes.
type ContinuousGoal struct{
    ID string `json:"id"`
    ProjectID string `json:"project_id"`
    ProjectWorkspaceID string `json:"project_workspace_id"`
    Objective string `json:"objective"`
    MonitoringPlanJSON json.RawMessage `json:"monitoring_plan"`
    Status string `json:"status"`
    Health string `json:"health"`
    CadenceMinutes int `json:"cadence_minutes"`
    LastCheckedAt int64 `json:"last_checked_at"`
    LastVerifiedAt int64 `json:"last_verified_at"`
    NextDueAt int64 `json:"next_due_at"`
    Revision int64 `json:"revision"`
    CreatedBy string `json:"created_by"`
    CreatedAt int64 `json:"created_at"`
    UpdatedAt int64 `json:"updated_at"`
}

type ConfigureContinuousGoalCommand struct {
    ProjectID string
    ProjectWorkspaceID string
    Objective string
    MonitoringPlanJSON json.RawMessage
    CadenceMinutes int
    ActorPrincipalID string
}

// TransitionContinuousGoalCommand only stops or retires a registered goal.
// Activation remains unavailable until verified monitor admission exists.
type TransitionContinuousGoalCommand struct {
    ProjectID string
    GoalID string
    TargetStatus string
    ExpectedRevision int64
    ActorPrincipalID string
}

// TransitionContinuousGoal performs a bounded, audited lifecycle change.
// This does not cancel already-started external effects: once operational
// monitoring exists, its execution leases must independently observe status.
func(s *Service)TransitionContinuousGoal(ctx context.Context,cmd TransitionContinuousGoalCommand)(ContinuousGoal,error){
    cmd.ProjectID=strings.TrimSpace(cmd.ProjectID)
    cmd.GoalID=strings.TrimSpace(cmd.GoalID)
    cmd.ActorPrincipalID=strings.TrimSpace(cmd.ActorPrincipalID)
    if cmd.ProjectID==""||cmd.GoalID==""||cmd.ActorPrincipalID==""||
       cmd.ExpectedRevision<1||(cmd.TargetStatus!="paused"&&cmd.TargetStatus!="retired"){
        return ContinuousGoal{},ErrInvalidCommand
    }
    eventID,err:=s.ids.New("evt")
    if err!=nil{return ContinuousGoal{},err}
    now:=s.clock.UnixMilli()
    err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
        project,err:=s.repo.ProjectTx(ctx,tx,cmd.ProjectID)
        if err!=nil{return err}
        if err=s.requireActor(ctx,tx,project.WorkspaceID,cmd.ActorPrincipalID);err!=nil{return err}
        // Retirement discards an objective. It must be explicitly human
        // directed, never self-authorised by a background Agent.
        if cmd.TargetStatus=="retired"{
            var principalType string
            if err=tx.QueryRowContext(ctx,`SELECT principal_type FROM principals
              WHERE id=? AND status='active'`,cmd.ActorPrincipalID).Scan(&principalType);err!=nil{return err}
            if principalType!="human"{return fmt.Errorf("%w: retirement requires human operator",ErrPrincipalIneligible)}
        }
        var currentStatus,workspaceID string
        var revision int64
        err=tx.QueryRowContext(ctx,`SELECT status,project_workspace_id,revision
         FROM project_continuous_goals WHERE id=? AND project_id=?`,
         cmd.GoalID,cmd.ProjectID).Scan(&currentStatus,&workspaceID,&revision)
        if err!=nil{return err}
        if revision!=cmd.ExpectedRevision{return ErrRevisionConflict}
        if currentStatus=="retired"||currentStatus==cmd.TargetStatus||
          (currentStatus!="configured"&&currentStatus!="active"&&currentStatus!="paused"){
            return ErrInvalidTransition
        }
        result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
          SET status=?,health='unobserved',next_due_at=0,revision=revision+1,updated_at=?
          WHERE id=? AND project_id=? AND revision=? AND status=?`,
          cmd.TargetStatus,now,cmd.GoalID,cmd.ProjectID,revision,currentStatus)
        if err!=nil{return err}
        affected,err:=result.RowsAffected()
        if err!=nil{return err}
        if affected!=1{return ErrRevisionConflict}
        payload,_:=json.Marshal(map[string]any{
          "goal_id":cmd.GoalID,"project_id":cmd.ProjectID,
          "project_workspace_id":workspaceID,"previous_status":currentStatus,
          "status":cmd.TargetStatus,"revision":revision+1,
        })
        return s.events.Append(ctx,tx,event.Event{
          ID:eventID,WorkspaceID:&project.WorkspaceID,
          Type:"project.goal_"+cmd.TargetStatus,
          AggregateType:"project_continuous_goal",AggregateID:cmd.GoalID,
          ActorPrincipalID:&cmd.ActorPrincipalID,Payload:payload,OccurredAt:now,
        })
    })
    if err!=nil{return ContinuousGoal{},err}
    return s.ContinuousGoal(ctx,cmd.GoalID)
}

var goalSourceID=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
type goalMonitorPlan struct {
    Version int `json:"version"`
    SourceIDs []string `json:"source_ids"`
}

func canonicalGoalPlan(raw json.RawMessage)(json.RawMessage,error){
    if len(raw)==0 {raw=json.RawMessage(`{"version":1,"source_ids":[]}`)}
    var obj map[string]json.RawMessage
    if err:=json.Unmarshal(raw,&obj);err!=nil||obj==nil{
        return nil,fmt.Errorf("monitoring plan must be an object")
    }
    for key:=range obj {
        if key!="version"&&key!="source_ids"{
            return nil,fmt.Errorf("unknown monitoring-plan field")
        }
    }
    var plan goalMonitorPlan
    if err:=json.Unmarshal(raw,&plan);err!=nil{return nil,fmt.Errorf("invalid monitoring plan: %w",err)}
    if plan.Version!=1||len(plan.SourceIDs)>32{
        return nil,fmt.Errorf("unsupported monitoring plan version or number of sources")
    }
    seen:=map[string]bool{}
    for _,id:=range plan.SourceIDs{
        if !goalSourceID.MatchString(id)||seen[id]{
            return nil,fmt.Errorf("invalid monitoring source identifier")
        }
        seen[id]=true
    }
    if plan.SourceIDs==nil {plan.SourceIDs=[]string{}}
    return json.Marshal(plan)
}

// ConfigureContinuousGoal persists an explicit, Workspace-bound objective.
// Only the user-authorised Project can own it; no backend monitoring
// operation is started and its first health state is always UNOBSERVED.
func(s *Service)ConfigureContinuousGoal(ctx context.Context,cmd ConfigureContinuousGoalCommand)(ContinuousGoal,error){
    cmd.ProjectID=strings.TrimSpace(cmd.ProjectID)
    cmd.ProjectWorkspaceID=strings.TrimSpace(cmd.ProjectWorkspaceID)
    cmd.Objective=strings.TrimSpace(cmd.Objective)
    if cmd.ProjectID==""||cmd.ProjectWorkspaceID==""||
        len(cmd.Objective)==0||len(cmd.Objective)>8192||
        strings.TrimSpace(cmd.ActorPrincipalID)==""{
        return ContinuousGoal{},ErrInvalidCommand
    }
    if cmd.CadenceMinutes==0{cmd.CadenceMinutes=60}
    if cmd.CadenceMinutes<5||cmd.CadenceMinutes>10080{return ContinuousGoal{},ErrInvalidCommand}
    plan,err:=canonicalGoalPlan(cmd.MonitoringPlanJSON)
    if err!=nil{return ContinuousGoal{},fmt.Errorf("%w: %v",ErrInvalidCommand,err)}
    gid,err:=s.ids.New("goal")
    if err!=nil{return ContinuousGoal{},err}
    eid,err:=s.ids.New("evt")
    if err!=nil{return ContinuousGoal{},err}
    now:=s.clock.UnixMilli()
    err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
        project,err:=s.repo.ProjectTx(ctx,tx,cmd.ProjectID)
        if err!=nil{return err}
        if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
            return fmt.Errorf("%w: Level 5 Project mode required",ErrInvalidCommand)
        }
        if err=s.requireActor(ctx,tx,project.WorkspaceID,cmd.ActorPrincipalID);err!=nil{return err}
        var workspaceStatus string
        if err=tx.QueryRowContext(ctx,
            "SELECT status FROM project_workspaces WHERE id=? AND project_id=?",
            cmd.ProjectWorkspaceID,project.ID).Scan(&workspaceStatus);err!=nil{
            return fmt.Errorf("%w: canonical Project Workspace unavailable",ErrInvalidCommand)
        }
        if workspaceStatus!="active"{
            return fmt.Errorf("%w: inactive Project Workspace",ErrInvalidCommand)
        }
        _,err=tx.ExecContext(ctx,`INSERT INTO project_continuous_goals(
           id,project_id,project_workspace_id,objective,monitoring_plan_json,status,
           health,cadence_minutes,last_checked_at,last_verified_at,next_due_at,
           revision,created_by,created_at,updated_at)
           VALUES(?,?,?,?,?,'configured','unobserved',?,0,0,0,1,?,?,?)`,
           gid,project.ID,cmd.ProjectWorkspaceID,cmd.Objective,string(plan),
           cmd.CadenceMinutes,cmd.ActorPrincipalID,now,now)
        if err!=nil{return err}
        payload,_:=json.Marshal(map[string]any{
            "goal_id":gid,"project_id":project.ID,
            "project_workspace_id":cmd.ProjectWorkspaceID,
            "status":"configured","health":"unobserved",
        })
        return s.events.Append(ctx,tx,event.Event{
            ID:eid,WorkspaceID:&project.WorkspaceID,
            Type:"project.goal_configured",AggregateType:"project_continuous_goal",
            AggregateID:gid,ActorPrincipalID:&cmd.ActorPrincipalID,
            Payload:payload,OccurredAt:now,
        })
    })
    if err!=nil{return ContinuousGoal{},err}
    return s.ContinuousGoal(ctx,gid)
}

func scanContinuousGoal(row scanner)(ContinuousGoal,error){
    var g ContinuousGoal
    var plan string
    err:=row.Scan(&g.ID,&g.ProjectID,&g.ProjectWorkspaceID,&g.Objective,&plan,
        &g.Status,&g.Health,&g.CadenceMinutes,&g.LastCheckedAt,
        &g.LastVerifiedAt,&g.NextDueAt,&g.Revision,&g.CreatedBy,
        &g.CreatedAt,&g.UpdatedAt)
    if err!=nil{return ContinuousGoal{},err}
    g.MonitoringPlanJSON=json.RawMessage(plan)
    return g,nil
}
const goalSelect=`SELECT id,project_id,project_workspace_id,objective,
 monitoring_plan_json,status,health,cadence_minutes,last_checked_at,
 last_verified_at,next_due_at,revision,created_by,created_at,updated_at
 FROM project_continuous_goals`

func(s *Service)ContinuousGoal(ctx context.Context,id string)(ContinuousGoal,error){
    if strings.TrimSpace(id)==""{return ContinuousGoal{},ErrInvalidCommand}
    return scanContinuousGoal(s.db.QueryRowContext(ctx,goalSelect+" WHERE id=?",id))
}
func(s *Service)ContinuousGoals(ctx context.Context,projectID string)([]ContinuousGoal,error){
    if strings.TrimSpace(projectID)==""{return nil,ErrInvalidCommand}
    rows,err:=s.db.QueryContext(ctx,goalSelect+
      " WHERE project_id=? ORDER BY created_at DESC,id",projectID)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]ContinuousGoal{}
    for rows.Next(){
        g,err:=scanContinuousGoal(rows)
        if err!=nil{return nil,err}
        out=append(out,g)
    }
    return out,rows.Err()
}

// Keep type-level parity with existing repo scanner, and make SQL null
// behaviour an explicit checked error rather than silently treating an
// absent Project Workspace as a project-wide execution grant.
var _ = sql.ErrNoRows
