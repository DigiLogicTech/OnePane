package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/localai"
)

func (s *Server) cancelQueuedModelInstall(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 var in struct{WorkspaceID string `json:"workspace_id"`}
 if !decodeJSON(w,r,&in){return}
 in.WorkspaceID=strings.TrimSpace(in.WorkspaceID)
 if !s.authorize(w,r,i,in.WorkspaceID,"model.write"){return}
 manager,ok:=s.localAI.(interface{CancelQueuedInstallJob(context.Context,string,string)(localai.InstallJob,error)})
 if !ok{writeError(w,http.StatusNotImplemented,"queued job cancellation unavailable");return}
 job,err:=manager.CancelQueuedInstallJob(r.Context(),r.PathValue("jobID"),in.WorkspaceID)
 if err!=nil{writeError(w,http.StatusConflict,err.Error());return}
 writeJSON(w,http.StatusOK,job)
}
func (s *Server) deleteManagedModel(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 var in struct{WorkspaceID string `json:"workspace_id"`}
 if !decodeJSON(w,r,&in){return}
 in.WorkspaceID=strings.TrimSpace(in.WorkspaceID)
 if !s.authorize(w,r,i,in.WorkspaceID,"model.write"){return}
 manager,ok:=s.localAI.(interface{DeleteManagedDeployment(context.Context,string,string,string)error})
 if !ok{writeError(w,http.StatusNotImplemented,"managed deletion unavailable");return}
 if err:=manager.DeleteManagedDeployment(r.Context(),in.WorkspaceID,r.PathValue("deploymentID"),i.PrincipalID);err!=nil{
  writeError(w,http.StatusConflict,err.Error());return
 }
 writeJSON(w,http.StatusOK,map[string]any{"status":"removed"})
}
