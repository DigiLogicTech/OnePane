package api

import (
    "net/http"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/localai"
)

// Authorize against the deployment's real workspace, not a caller-provided
// workspace ID. Tier settings are never cross-workspace writable.
func (s *Server) authorizeColibriDeployment(w http.ResponseWriter, r *http.Request, workspace, deployment, capability string) bool {
    if s.localAI==nil {writeError(w,http.StatusServiceUnavailable,"local AI service unavailable");return false}
    if workspace==""||deployment=="" {writeError(w,http.StatusBadRequest,"workspace_id and deployment_id are required");return false}
    actual,err:=s.localAI.ManagedDeploymentWorkspace(r.Context(),deployment)
    if err!=nil||actual==nil||*actual!=workspace {
        writeError(w,http.StatusNotFound,"managed deployment not found in this workspace")
        return false
    }
    return true
}

func (s *Server) getColibriTier(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.read"){return}
    if !s.authorize(w,r,i,ws,"model.read"){return}
    out,err:=s.localAI.ColibriTier(r.Context(),dep)
    respondDomain(w,out,err,http.StatusOK)
}

func (s *Server) setColibriTier(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    var in struct{
        WorkspaceID string `json:"workspace_id"`
        Settings localai.ColibriTierSettings `json:"settings"`
    }
    if !decodeJSON(w,r,&in){return}
    ws:=strings.TrimSpace(in.WorkspaceID)
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.write"){return}
    if !s.authorize(w,r,i,ws,"model.write"){return}
    out,err:=s.localAI.SetColibriTier(r.Context(),localai.ColibriTierCommand{DeploymentID:dep,Settings:in.Settings})
    if err!=nil {writeError(w,http.StatusConflict,err.Error());return}
    writeJSON(w,http.StatusOK,out)
}

func (s *Server) planColibriTier(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.read"){return}
    if !s.authorize(w,r,i,ws,"model.read"){return}
    out,err:=s.localAI.ColibriPlan(r.Context(),dep)
    if err!=nil {writeError(w,http.StatusConflict,err.Error());return}
    writeJSON(w,http.StatusOK,out)
}

