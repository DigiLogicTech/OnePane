package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTCPAttemptService interface {
 RunManualGoalTCPProbe(context.Context,projectworkspace.RunGoalTCPProbeCommand)(projectworkspace.GoalTCPProbeAttempt,error)
 ListGoalTCPProbeAttempts(context.Context,string,string)([]projectworkspace.GoalTCPProbeAttempt,error)
}

// This endpoint is a HUMAN initiated, bounded, one-off TCP handshake.
// It never starts a recurring network scheduler or verifies device health.
func(s *Server)runProjectGoalTCPProbe(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPAttemptService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP probe execution unavailable");return}
 var input struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.RunManualGoalTCPProbe(r.Context(),projectworkspace.RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:r.PathValue("permitID"),
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedPermitRevision:input.ExpectedPermitRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)listProjectGoalTCPProbeAttempts(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPAttemptService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP probe history unavailable");return}
 result,err:=svc.ListGoalTCPProbeAttempts(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}
