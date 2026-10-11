package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Optional capability: old API test stubs are not required to implement the
// new governed lifecycle method until they opt into these routes.
type applicationLifecycleService interface {
 Application(context.Context,string)(projectworkspace.Application,error)
 SetApplicationDesiredState(context.Context,projectworkspace.SetApplicationDesiredStateCommand)(projectworkspace.Application,error)
}

func (s *Server) setApplicationDesired(w http.ResponseWriter,r *http.Request){
 principal,ok:=s.authenticate(w,r);if !ok{return}
 svc,ok:=s.projects.(applicationLifecycleService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Project application lifecycle is unavailable");return}
 runtime,err:=s.projects.Runtime(r.Context(),r.PathValue("runtimeID"))
 if err!=nil{respondDomain(w,nil,err,0);return}
 application,err:=svc.Application(r.Context(),r.PathValue("applicationID"))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if application.ProjectRuntimeID!=runtime.ID{
  writeError(w,http.StatusForbidden,"Application does not belong to this runtime")
  return
 }
 project,err:=s.projects.Project(r.Context(),runtime.ProjectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,principal,project.WorkspaceID,"project.run"){return}
 var input struct{
  ExpectedRevision int64 `json:"expected_revision"`
  DesiredState projectworkspace.AppDesiredState `json:"desired_state"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.SetApplicationDesiredState(r.Context(),projectworkspace.SetApplicationDesiredStateCommand{
  ApplicationID:application.ID,ExpectedRevision:input.ExpectedRevision,
  DesiredState:input.DesiredState,ActorPrincipalID:principal.PrincipalID,
  RequestID:headerPtr(r,"X-Request-ID"),TraceID:headerPtr(r,"X-Trace-ID"),
 })
 respondDomain(w,result,err,http.StatusOK)
}
