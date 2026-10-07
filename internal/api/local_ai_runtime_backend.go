package api

import (
 "context"
 "net/http"
 "strings"
)
func (s *Server) removeLlamaBackend(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 var in struct{WorkspaceID string `json:"workspace_id"`}
 if !decodeJSON(w,r,&in){return}
 workspace:=strings.TrimSpace(in.WorkspaceID)
 if workspace==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
 if !s.authorize(w,r,identity,workspace,"model.write"){return}
 provider,ok:=s.localAI.(interface{RemoveLlamaBackend(context.Context,string)error})
 if !ok{writeError(w,http.StatusNotImplemented,"backend management unavailable");return}
 backend:=strings.ToLower(strings.TrimSpace(r.PathValue("backend")))
 if err:=provider.RemoveLlamaBackend(r.Context(),backend);err!=nil{writeError(w,http.StatusConflict,err.Error());return}
 writeJSON(w,http.StatusOK,map[string]any{"backend":backend,"status":"removed","model_weights_preserved":true})
}
