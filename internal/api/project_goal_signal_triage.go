package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type goalSignalTriageService interface{
 GoalSignalTriage(context.Context,string,string)(projectworkspace.GoalSignalTriage,error)
}

// Informational only: human Project readers can examine bounded, already
// admitted signal metadata. No body fields, model tools or effects exist.
func(s *Server)getProjectGoalSignalTriage(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human evidence reviewer required")
  return
 }
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable")
  return
 }
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalSignalTriageService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"signal triage unavailable")
  return
 }
 result,err:=svc.GoalSignalTriage(r.Context(),p.ID,
  strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}
