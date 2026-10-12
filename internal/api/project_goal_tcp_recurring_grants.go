package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Recurring network consent is NOT the human's manual test permit.
// Even where separately approved this API never executes a TCP connect.
type goalTCPRecurringGrantService interface {
 ApproveTCPRecurringGrant(context.Context,projectworkspace.ApproveTCPRecurringGrantCommand)(projectworkspace.TCPRecurringGrant,error)
 RevokeTCPRecurringGrant(context.Context,projectworkspace.RevokeTCPRecurringGrantCommand)(projectworkspace.TCPRecurringGrant,error)
 ListTCPRecurringGrants(context.Context,string,string)([]projectworkspace.TCPRecurringGrant,error)
}

func(s *Server)listProjectGoalTCPRecurringGrants(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPRecurringGrantService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"recurring probe consent unavailable");return}
 result,err:=svc.ListTCPRecurringGrants(r.Context(),project.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)approveProjectGoalTCPRecurringGrant(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPRecurringGrantService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"recurring probe consent unavailable");return}
 var body struct{
  ManualPermitID string `json:"manual_permit_id"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedManualPermitRevision int64 `json:"expected_manual_permit_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeJSON(w,r,&body){return}
 grant,err:=svc.ApproveTCPRecurringGrant(r.Context(),projectworkspace.ApproveTCPRecurringGrantCommand{
  ProjectID:project.ID,GoalID:r.PathValue("goalID"),
  ManualPermitID:body.ManualPermitID,
  ExpectedGoalRevision:body.ExpectedGoalRevision,
  ExpectedManualPermitRevision:body.ExpectedManualPermitRevision,
  ExpiresAt:body.ExpiresAt,ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,grant,err,http.StatusCreated)
}

func(s *Server)revokeProjectGoalTCPRecurringGrant(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPRecurringGrantService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"recurring probe consent unavailable");return}
 var body struct {
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedGrantRevision int64 `json:"expected_grant_revision"`
 }
 if !decodeJSON(w,r,&body){return}
 result,err:=svc.RevokeTCPRecurringGrant(r.Context(),projectworkspace.RevokeTCPRecurringGrantCommand{
  ProjectID:project.ID,GoalID:r.PathValue("goalID"),
  GrantID:r.PathValue("grantID"),ExpectedGoalRevision:body.ExpectedGoalRevision,
  ExpectedGrantRevision:body.ExpectedGrantRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}
