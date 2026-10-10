package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type researchFrozenSessionLister interface{
 FrozenResearchEvidenceSessions(context.Context,string,string,string)(
  []projectworkspace.ResearchFrozenSession,error)
}
func(s *Server) listFrozenResearchEvidenceSessions(w http.ResponseWriter,r *http.Request){
 project,workspaceID,principalID,ok:=s.workspaceRuntimeContext(w,r,false)
 if !ok{return}
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"team.read"){return}
 svc,ok:=s.projects.(researchFrozenSessionLister)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"Research approval history unavailable")
  return
 }
 rows,err:=svc.FrozenResearchEvidenceSessions(r.Context(),project.ID,workspaceID,principalID)
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,"Scoped Research approval history unavailable")
  return
 }
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("X-Content-Type-Options","nosniff")
 writeJSON(w,http.StatusOK,rows)
}
