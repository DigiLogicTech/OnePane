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
 TransitionContinuousGoal(context.Context,projectworkspace.TransitionContinuousGoalCommand)(projectworkspace.ContinuousGoal,error)
 AttachGoalObservation(context.Context,projectworkspace.AttachGoalObservationCommand)(projectworkspace.GoalObservationReceipt,error)
 GoalObservations(context.Context,string,string)([]projectworkspace.GoalObservationReceipt,error)
 GoalEvidenceCoverage(context.Context,string,string)(projectworkspace.GoalEvidenceCoverage,error)
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


// Pause and retire never start a monitor. They use a revision precondition and
// the same canonical Project authorization as goal creation.
func(s *Server)transitionProjectContinuousGoal(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 var in struct{ExpectedRevision int64 `json:"expected_revision"`}
 if !decodeJSON(w,r,&in){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"continuous goal service unavailable");return}
 goal,err:=svc.TransitionContinuousGoal(r.Context(),projectworkspace.TransitionContinuousGoalCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),TargetStatus:r.PathValue("transition"),
  ExpectedRevision:in.ExpectedRevision,ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,goal,err,http.StatusOK)
}

// Retained evidence APIs intentionally return only source, time, immutable
// observation ID and hash. Raw sensor payloads remain in the Observation
// store under independent access policies.
func(s *Server)listProjectGoalObservations(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"goal observation service unavailable");return}
 receipts,err:=svc.GoalObservations(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,receipts,err,http.StatusOK)
}
func(s *Server)attachProjectGoalObservation(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 var input struct{
  ObservationID string `json:"observation_id"`
  ExpectedRevision int64 `json:"expected_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"goal observation service unavailable");return}
 receipt,err:=svc.AttachGoalObservation(r.Context(),projectworkspace.AttachGoalObservationCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),ObservationID:input.ObservationID,
  ExpectedRevision:input.ExpectedRevision,ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,receipt,err,http.StatusCreated)
}


// Coverage is a read-only projection of attached evidence, not proof of health.
func(s *Server)getProjectGoalEvidenceCoverage(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(continuousGoalService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"goal evidence coverage unavailable");return}
 coverage,err:=svc.GoalEvidenceCoverage(r.Context(),project.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,coverage,err,http.StatusOK)
}
