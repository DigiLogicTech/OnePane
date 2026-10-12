package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTCPBaselineService interface {
 SetGoalTCPBaseline(context.Context,projectworkspace.SetGoalTCPBaselineCommand)(projectworkspace.GoalTCPBaselineRevision,error)
 ListGoalTCPBaselines(context.Context,string,string)([]projectworkspace.GoalTCPBaselineRevision,error)
 GoalTCPReachabilityAssessment(context.Context,string,string,string)(projectworkspace.GoalTCPReachabilityAssessment,error)
}

// An operator's expected TCP handshake is only an observation baseline,
// not confirmation of security, compliance or operational health.
func(s *Server)setProjectGoalTCPBaseline(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPBaselineService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"reachability baseline unavailable");return}
 var input struct{
  PermitID string `json:"permit_id"`
  ExpectedTCP string `json:"expected_tcp"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedBaselineRevision int64 `json:"expected_baseline_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.SetGoalTCPBaseline(r.Context(),projectworkspace.SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:input.PermitID,
  ExpectedTCP:input.ExpectedTCP,
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedBaselineRevision:input.ExpectedBaselineRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusCreated)
}

func(s *Server)listProjectGoalTCPBaselines(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPBaselineService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"reachability baseline unavailable");return}
 result,err:=svc.ListGoalTCPBaselines(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)getProjectGoalTCPReachabilityAssessment(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPBaselineService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"reachability assessment unavailable");return}
 result,err:=svc.GoalTCPReachabilityAssessment(r.Context(),p.ID,
  strings.TrimSpace(r.PathValue("goalID")),strings.TrimSpace(r.PathValue("sourceID")))
 respondDomain(w,result,err,http.StatusOK)
}
