package api

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Goal configurators expose only metadata for the authenticated Project.
// Creating a Level 5 objective does NOT start a monitoring loop or permit
// network scans, file access, device quarantine or other side effects.
type continuousGoalService interface{
 ContinuousGoals(context.Context,string)([]projectworkspace.ContinuousGoal,error)
 ConfigureContinuousGoal(context.Context,projectworkspace.ConfigureContinuousGoalCommand)(projectworkspace.ContinuousGoal,error)
}

func(s *Server)listProjectContinuousGoals(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"continuous goal service unavailable");return}
 goals,err:=svc.ContinuousGoals(r.Context(),p.ID)
 respondDomain(w,goals,err,http.StatusOK)
}

func(s *Server)configureProjectContinuousGoal(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 var in struct{
  ProjectWorkspaceID string `json:"project_workspace_id"`
  Objective string `json:"objective"`
  MonitoringPlanJSON json.RawMessage `json:"monitoring_plan"`
  CadenceMinutes int `json:"cadence_minutes"`
 }
 if !decodeJSON(w,r,&in){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"continuous goal service unavailable");return}
 cmd:=projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:in.ProjectWorkspaceID,
  Objective:in.Objective,MonitoringPlanJSON:in.MonitoringPlanJSON,
  CadenceMinutes:in.CadenceMinutes,ActorPrincipalID:identity.PrincipalID,
 }
 goal,err:=svc.ConfigureContinuousGoal(r.Context(),cmd)
 respondDomain(w,goal,err,http.StatusCreated)
}
