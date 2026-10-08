package api

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/localai"
)

type colibriTierService interface {
    ColibriTier(context.Context, string) (localai.ColibriTierState, error)
    SetColibriTier(context.Context, localai.ColibriTierCommand) (localai.ColibriTierState, error)
    ColibriPlan(context.Context, string) (json.RawMessage, error)
}

func (s *Server) requireColibriTier(w http.ResponseWriter) (colibriTierService,bool) {
    service,ok:=s.localAI.(colibriTierService)
    if !ok {writeError(w,http.StatusServiceUnavailable,"Colibri tier controls unavailable");return nil,false}
    return service,true
}

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
    tier,ready:=s.requireColibriTier(w);if !ready{return}
    out,err:=tier.ColibriTier(r.Context(),dep)
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
    tier,ready:=s.requireColibriTier(w);if !ready{return}
    out,err:=tier.SetColibriTier(r.Context(),localai.ColibriTierCommand{DeploymentID:dep,Settings:in.Settings})
    if err!=nil {writeError(w,http.StatusConflict,err.Error());return}
    writeJSON(w,http.StatusOK,out)
}

func (s *Server) planColibriTier(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.read"){return}
    if !s.authorize(w,r,i,ws,"model.read"){return}
    tier,ready:=s.requireColibriTier(w);if !ready{return}
    out,err:=tier.ColibriPlan(r.Context(),dep)
    if err!=nil {writeError(w,http.StatusConflict,err.Error());return}
    writeJSON(w,http.StatusOK,out)
}

