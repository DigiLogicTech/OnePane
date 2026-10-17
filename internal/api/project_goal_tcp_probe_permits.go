package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTCPProbePermitService interface {
 ApproveGoalTCPProbe(context.Context,projectworkspace.ApproveGoalTCPProbeCommand)(projectworkspace.GoalTCPProbePermit,error)
 RevokeGoalTCPProbe(context.Context,projectworkspace.RevokeGoalTCPProbeCommand)(projectworkspace.GoalTCPProbePermit,error)
 ListGoalTCPProbePermits(context.Context,string,string)([]projectworkspace.GoalTCPProbePermit,error)
}

func(s *Server)listProjectGoalTCPProbePermits(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPProbePermitService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"L5 source grants unavailable");return}
 out,err:=svc.ListGoalTCPProbePermits(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,out,err,http.StatusOK)
}

func(s *Server)approveProjectGoalTCPProbe(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPProbePermitService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"L5 source grants unavailable");return}
 var input struct {
  SourceID string `json:"source_id"`
  TargetIPv4 string `json:"target_ipv4"`
  TargetPort int `json:"target_port"`
  TimeoutMS int `json:"timeout_ms"`
  ExpiresAt int64 `json:"expires_at"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.ApproveGoalTCPProbe(r.Context(),projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),
  SourceID:input.SourceID,TargetIPv4:input.TargetIPv4,
  TargetPort:input.TargetPort,TimeoutMS:input.TimeoutMS,
  ExpiresAt:input.ExpiresAt,ExpectedGoalRevision:input.ExpectedGoalRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,out,err,http.StatusCreated)
}

func(s *Server)revokeProjectGoalTCPProbe(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPProbePermitService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"L5 source grants unavailable");return}
 var input struct {
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.RevokeGoalTCPProbe(r.Context(),projectworkspace.RevokeGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:r.PathValue("permitID"),
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedPermitRevision:input.ExpectedPermitRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,out,err,http.StatusOK)
}
