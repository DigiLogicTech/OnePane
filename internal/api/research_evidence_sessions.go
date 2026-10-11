package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type researchEvidenceSessionLister interface{
 EligibleResearchEvidenceSessions(context.Context,string,string,string)(
  []projectworkspace.ResearchEvidenceEligibleSession,error)
}
func(s *Server) listEligibleResearchEvidenceSessions(w http.ResponseWriter,r *http.Request){
 project,workspaceID,principalID,ok:=s.workspaceRuntimeContext(w,r,false)
 if !ok{return}
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"team.read"){return}
 svc,ok:=s.projects.(researchEvidenceSessionLister)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"Scoped Research sessions unavailable")
  return
 }
 rows,err:=svc.EligibleResearchEvidenceSessions(r.Context(),project.ID,
  workspaceID,principalID)
 respondDomain(w,rows,err,http.StatusOK)
}
