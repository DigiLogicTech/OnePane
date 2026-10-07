package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/localai"
)

// Managed llmfit is node-local and advisory; callers must still have the
// Workspace model permission that authorizes hardware/model management.
type llmfitManager interface{
 ManagedLLMFit(context.Context)(localai.ManagedLLMFitStatus,error)
 InstallManagedLLMFit(context.Context)error
 StartManagedLLMFit(context.Context)error
 StopManagedLLMFit(context.Context)error
 RemoveManagedLLMFit(context.Context)error
}
func (s *Server) managedLLMFitRequest(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 workspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 if r.Method==http.MethodPost{
  var in struct{WorkspaceID string `json:"workspace_id"`}
  if !decodeJSON(w,r,&in){return}
  workspace=strings.TrimSpace(in.WorkspaceID)
 }
 if workspace==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
 permission:="model.read";if r.Method==http.MethodPost{permission="model.write"}
 if !s.authorize(w,r,identity,workspace,permission){return}
 mgr,ok:=s.localAI.(llmfitManager)
 if !ok{writeError(w,http.StatusNotImplemented,"managed llmfit service is unavailable");return}
 if r.Method==http.MethodPost {
  var err error
  switch r.PathValue("action"){
  case "install":err=mgr.InstallManagedLLMFit(r.Context())
  case "start":err=mgr.StartManagedLLMFit(r.Context())
  case "stop":err=mgr.StopManagedLLMFit(r.Context())
  case "remove":err=mgr.RemoveManagedLLMFit(r.Context())
  default:writeError(w,http.StatusBadRequest,"unsupported llmfit action");return
  }
  if err!=nil{writeError(w,http.StatusConflict,err.Error());return}
 }
 state,err:=mgr.ManagedLLMFit(r.Context())
 respondDomain(w,state,err,http.StatusOK)
}
