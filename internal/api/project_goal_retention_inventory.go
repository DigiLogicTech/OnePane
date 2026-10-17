package api

import (
 "context"
 "net/http"
 "strings"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type goalRetentionInventoryService interface{
 GoalRetentionInventory(context.Context,string,string)(projectworkspace.GoalRetentionInventory,error)
}
func(s *Server)getProjectGoalRetentionInventory(w http.ResponseWriter,r *http.Request){
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
 service,ok:=s.projects.(goalRetentionInventoryService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"retention inventory unavailable");return
 }
 item,e:=service.GoalRetentionInventory(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,item,e,http.StatusOK)
}
