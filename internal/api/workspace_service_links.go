package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// These endpoints change only the control-plane grant registry. They do NOT
// forward a request, expose the verified loopback port or create an OCI
// network bridge. Only authenticated human Project operators can write.
type workspaceServiceLinkRegistry interface {
 WorkspaceServiceLink(context.Context,string)(projectworkspace.WorkspaceServiceLink,error)
 WorkspaceServiceLinks(context.Context,string)([]projectworkspace.WorkspaceServiceLink,error)
 CreateWorkspaceServiceLink(context.Context,projectworkspace.CreateWorkspaceServiceLinkCommand)(projectworkspace.WorkspaceServiceLink,error)
 SetWorkspaceServiceLink(context.Context,projectworkspace.SetWorkspaceServiceLinkCommand)(projectworkspace.WorkspaceServiceLink,error)
}

func (s *Server) workspaceServiceLinksRegistry(w http.ResponseWriter)(workspaceServiceLinkRegistry,bool){
 registry,ok:=s.projects.(workspaceServiceLinkRegistry)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace service grants unavailable");return nil,false}
 return registry,true
}
func (s *Server) projectWorkspaceServiceLinks(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 permission:="project.read"
 if r.Method==http.MethodPost{permission="project.write"}
 if !s.authorize(w,r,i,p.WorkspaceID,permission){return}
 registry,ok:=s.workspaceServiceLinksRegistry(w);if !ok{return}
 if r.Method==http.MethodGet{
  links,err:=registry.WorkspaceServiceLinks(r.Context(),projectID)
  respondDomain(w,links,err,http.StatusOK);return
 }
 var input struct{
  SourceWorkspaceID string `json:"source_workspace_id"`
  TargetWorkspaceID string `json:"target_workspace_id"`
  EndpointID string `json:"endpoint_id"`
  Name string `json:"name"`
  ApprovedPath string `json:"approved_path"`
  ExpiresAtMS int64 `json:"expires_at_ms"`
 }
 if !decodeJSON(w,r,&input){return}
 created,err:=registry.CreateWorkspaceServiceLink(r.Context(),projectworkspace.CreateWorkspaceServiceLinkCommand{
  ProjectID:projectID,SourceWorkspaceID:input.SourceWorkspaceID,
  TargetWorkspaceID:input.TargetWorkspaceID,EndpointID:input.EndpointID,
  Name:input.Name,ApprovedPath:input.ApprovedPath,
  ExpiresAtMS:input.ExpiresAtMS,ActorPrincipalID:i.PrincipalID,
 })
 respondDomain(w,created,err,http.StatusCreated)
}
func (s *Server) projectWorkspaceServiceLinkUpdate(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 registry,ok:=s.workspaceServiceLinksRegistry(w);if !ok{return}
 link,err:=registry.WorkspaceServiceLink(r.Context(),strings.TrimSpace(r.PathValue("linkID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 p,err:=s.projects.Project(r.Context(),link.ProjectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,i,p.WorkspaceID,"project.write"){return}
 var input struct{
  ExpectedRevision int64 `json:"expected_revision"`
  Enabled bool `json:"enabled"`
  ExpiresAtMS int64 `json:"expires_at_ms"`
 }
 if !decodeJSON(w,r,&input){return}
 changed,err:=registry.SetWorkspaceServiceLink(r.Context(),projectworkspace.SetWorkspaceServiceLinkCommand{
  LinkID:link.ID,ExpectedRevision:input.ExpectedRevision,
  Enabled:input.Enabled,ExpiresAtMS:input.ExpiresAtMS,
  ActorPrincipalID:i.PrincipalID,
 })
 respondDomain(w,changed,err,http.StatusOK)
}
