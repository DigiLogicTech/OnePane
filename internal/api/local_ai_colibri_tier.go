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

type colibriPinService interface {
 ColibriPin(context.Context,string)(localai.ColibriPinState,error)
 SetColibriPin(context.Context,string,bool)(localai.ColibriPinState,error)
}
func(s *Server)getColibriPin(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 dep:=strings.TrimSpace(r.PathValue("deploymentID"))
 if !s.authorizeColibriDeployment(w,r,ws,dep,"model.read"){return}
 if !s.authorize(w,r,i,ws,"model.read"){return}
 svc,ok:=s.localAI.(colibriPinService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Colibri pin unavailable");return}
 out,err:=svc.ColibriPin(r.Context(),dep)
 respondDomain(w,out,err,http.StatusOK)
}
func(s *Server)setColibriPin(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 var in struct{
  WorkspaceID string `json:"workspace_id"`
  Pinned *bool `json:"pinned"`
 }
 if !decodeJSON(w,r,&in){return}
 if in.Pinned==nil{writeError(w,http.StatusBadRequest,"pinned must be a boolean");return}
 ws:=strings.TrimSpace(in.WorkspaceID)
 dep:=strings.TrimSpace(r.PathValue("deploymentID"))
 if !s.authorizeColibriDeployment(w,r,ws,dep,"model.write"){return}
 if !s.authorize(w,r,i,ws,"model.write"){return}
 svc,ok:=s.localAI.(colibriPinService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Colibri pin unavailable");return}
 out,err:=svc.SetColibriPin(r.Context(),dep,*in.Pinned)
 if err!=nil{
  writeError(w,http.StatusConflict,"Colibri pin unavailable: check model compatibility and existing Node pin")
  return
 }
 writeJSON(w,http.StatusOK,out)
}

type colibriSwapService interface {
    ColibriHotSwap(context.Context, string) (localai.ColibriSwapState, error)
    ColibriHotSwapStatus(context.Context, string) (localai.ColibriSwapState, error)
}
func (s *Server) requireColibriSwap(w http.ResponseWriter) (colibriSwapService,bool) {
    service,ok:=s.localAI.(colibriSwapService)
    if !ok {writeError(w,http.StatusServiceUnavailable,"Colibri hot swapping unavailable");return nil,false}
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


func (s *Server) getColibriSwap(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.read"){return}
    if !s.authorize(w,r,i,ws,"model.read"){return}
    swap,ready:=s.requireColibriSwap(w);if !ready{return}
    out,err:=swap.ColibriHotSwapStatus(r.Context(),dep)
    respondDomain(w,out,err,http.StatusOK)
}

func (s *Server) activateColibriSwap(w http.ResponseWriter, r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    var in struct{ WorkspaceID string `json:"workspace_id"` }
    if !decodeJSON(w,r,&in){return}
    ws:=strings.TrimSpace(in.WorkspaceID)
    dep:=strings.TrimSpace(r.PathValue("deploymentID"))
    if !s.authorizeColibriDeployment(w,r,ws,dep,"model.write"){return}
    if !s.authorize(w,r,i,ws,"model.write"){return}
    swap,ready:=s.requireColibriSwap(w);if !ready{return}
    out,err:=swap.ColibriHotSwap(r.Context(),dep)
    if err!=nil{writeError(w,http.StatusConflict,err.Error());return}
    writeJSON(w,http.StatusOK,out)
}
