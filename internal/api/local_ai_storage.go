package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/localai"
)
type storageManager interface {
 ManagedStorageReport(context.Context)(localai.StorageReport,error)
 CleanupOwnedStorage(context.Context)(localai.StorageReport,error)
}
func (s *Server) managedStorageRequest(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 workspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 if r.Method==http.MethodPost{
  var in struct { WorkspaceID string `json:"workspace_id"` }
  if !decodeJSON(w,r,&in){return}
  workspace=strings.TrimSpace(in.WorkspaceID)
 }
 if workspace==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
 permission:="model.read";if r.Method==http.MethodPost{permission="model.write"}
 if !s.authorize(w,r,identity,workspace,permission){return}
 mgr,ok:=s.localAI.(storageManager)
 if !ok{writeError(w,http.StatusNotImplemented,"storage diagnostics unavailable");return}
 var out localai.StorageReport;var err error
 if r.Method==http.MethodPost{out,err=mgr.CleanupOwnedStorage(r.Context())}else{out,err=mgr.ManagedStorageReport(r.Context())}
 if err!=nil{writeError(w,http.StatusConflict,err.Error());return}
 writeJSON(w,http.StatusOK,out)
}
