package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type goalObserverRecoveryListService interface{
 ListGoalObserverRecoveryCases(context.Context,string,string)([]projectworkspace.GoalObserverRecoveryCase,error)
}
func(s *Server)listProjectGoalObserverRecoveryCases(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator required");return
 }
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return
 }
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 service,ok:=s.projects.(goalObserverRecoveryListService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"observer recovery unavailable");return
 }
 items,e:=service.ListGoalObserverRecoveryCases(r.Context(),p.ID,
  strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,items,e,http.StatusOK)
}
