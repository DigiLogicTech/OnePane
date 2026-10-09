package api

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type workspaceRuntimeService interface {
 RuntimeByProjectWorkspace(context.Context,string,string)(projectworkspace.ProjectRuntime,error)
}

func (s *Server) workspaceRuntimeContext(w http.ResponseWriter,r *http.Request,write bool)(projectworkspace.Project,string,string,bool){
 i,ok:=s.authenticate(w,r);if !ok{return projectworkspace.Project{},"","",false}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 workspaceID:=strings.TrimSpace(r.PathValue("workspaceID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return projectworkspace.Project{},"","",false}
 permission:="project.read";if write{permission="project.write"}
 if !s.authorize(w,r,i,p.WorkspaceID,permission){return projectworkspace.Project{},"","",false}
 reader,ok:=s.projects.(projectWorkspaceViewReader)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Project Workspace registry unavailable");return projectworkspace.Project{},"","",false}
 workspace,err:=reader.WorkspaceView(r.Context(),workspaceID)
 if err!=nil{respondDomain(w,nil,err,0);return projectworkspace.Project{},"","",false}
 if workspace.ProjectID!=p.ID||workspace.Status!="active"{
  writeError(w,http.StatusForbidden,"Workspace does not belong to active Project")
  return projectworkspace.Project{},"","",false
 }
 return p,workspaceID,i.PrincipalID,true
}

func (s *Server) getWorkspaceRuntime(w http.ResponseWriter,r *http.Request){
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 reader,ok:=s.projects.(workspaceRuntimeService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace runtime service unavailable");return}
 runtime,err:=reader.RuntimeByProjectWorkspace(r.Context(),p.ID,workspaceID)
 respondDomain(w,runtime,err,http.StatusOK)
}
func (s *Server) createWorkspaceRuntime(w http.ResponseWriter,r *http.Request){
 p,workspaceID,principal,ok:=s.workspaceRuntimeContext(w,r,true);if !ok{return}
 var in struct{
  NodeID *string `json:"node_id"`
  IsolationMode projectworkspace.IsolationMode `json:"isolation_mode"`
  DesiredState projectworkspace.RuntimeDesiredState `json:"desired_state"`
  RuntimeSpecJSON json.RawMessage `json:"runtime_spec"`
  ResourceLimitsJSON json.RawMessage `json:"resource_limits"`
  EnvironmentBindingsJSON json.RawMessage `json:"environment_bindings"`
 }
 if !decodeJSON(w,r,&in){return}
 // Existing reconciliation/observation and governed tool paths are reused;
 // no new direct shell, engine socket or host privilege is introduced.
 runtime,err:=s.projects.CreateRuntime(r.Context(),projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&workspaceID,NodeID:in.NodeID,
  IsolationMode:in.IsolationMode,DesiredState:in.DesiredState,
  RuntimeSpecJSON:in.RuntimeSpecJSON,ResourceLimitsJSON:in.ResourceLimitsJSON,
  EnvironmentBindingsJSON:in.EnvironmentBindingsJSON,CreatedBy:principal,
  RequestID:headerPtr(r,"X-Request-ID"),TraceID:headerPtr(r,"X-Trace-ID")})
 respondDomain(w,runtime,err,http.StatusCreated)
}
