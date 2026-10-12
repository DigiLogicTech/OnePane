package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTCPRecurringConsentService interface {
 ApproveTCPRecurringConsent(context.Context,projectworkspace.ApproveTCPRecurringConsentCommand)(projectworkspace.TCPRecurringConsent,error)
 RevokeTCPRecurringConsent(context.Context,projectworkspace.RevokeTCPRecurringConsentCommand)(projectworkspace.TCPRecurringConsent,error)
 ListTCPRecurringConsents(context.Context,string,string)([]projectworkspace.TCPRecurringConsent,error)
}

// Explicitly separately approved, expiring network authority. Existing
// one-shot TCP grants do not imply permission for recurring connections.
func(s *Server)approveProjectGoalTCPRecurringConsent(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPRecurringConsentService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP recurring grants unavailable");return}
 var input struct {
  PermitID string `json:"permit_id"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.ApproveTCPRecurringConsent(r.Context(),projectworkspace.ApproveTCPRecurringConsentCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:input.PermitID,
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedPermitRevision:input.ExpectedPermitRevision,
  ExpiresAt:input.ExpiresAt,ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusCreated)
}

func(s *Server)revokeProjectGoalTCPRecurringConsent(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalTCPRecurringConsentService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP recurring grants unavailable");return}
 var input struct {
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedConsentRevision int64 `json:"expected_consent_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.RevokeTCPRecurringConsent(r.Context(),projectworkspace.RevokeTCPRecurringConsentCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),ConsentID:r.PathValue("consentID"),
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedConsentRevision:input.ExpectedConsentRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)listProjectGoalTCPRecurringConsents(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalTCPRecurringConsentService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP recurring grants unavailable");return}
 result,err:=svc.ListTCPRecurringConsents(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}
