package api

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Canonical Workspace IDs must be minted by the backend. Legacy dashboard IDs
// remain compatibility mappings inside state_json, never runtime identifiers.
type workspaceViewCreator interface {
 CreateWorkspaceView(context.Context,projectworkspace.CreateWorkspaceViewCommand)(projectworkspace.WorkspaceView,error)
}
func (s *Server) createCanonicalProjectWorkspace(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,i,p.WorkspaceID,"project.write"){return}
 writer,ok:=s.projects.(workspaceViewCreator)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Canonical Workspace creation unavailable");return}
 var input struct {
  Name string `json:"name"`
  LegacyWorkspaceID string `json:"legacy_workspace_id"`
 }
 if !decodeJSON(w,r,&input){return}
 input.Name=strings.TrimSpace(input.Name)
 input.LegacyWorkspaceID=strings.TrimSpace(input.LegacyWorkspaceID)
 if len(input.Name)<1||len(input.Name)>120||len(input.LegacyWorkspaceID)<1||len(input.LegacyWorkspaceID)>128{
  writeError(w,http.StatusBadRequest,"invalid Workspace name or legacy ID");return
 }
 // Reconcile by mapping, rather than guessing from an independently generated UI ID.
 reader,ok:=s.projects.(projectWorkspaceViewReader)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace registry unavailable");return}
 existing,err:=reader.WorkspaceViews(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 for _,view:=range existing {
  var state struct{LegacyID string `json:"legacy_workspace_id"`}
  _=json.Unmarshal(view.StateJSON,&state)
  if state.LegacyID==input.LegacyWorkspaceID {
   writeJSON(w,http.StatusOK,view);return
  }
 }
 state,_:=json.Marshal(map[string]any{"legacy_workspace_id":input.LegacyWorkspaceID})
 out,err:=writer.CreateWorkspaceView(r.Context(),projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:projectID,Name:input.Name,StateJSON:state,ActorPrincipalID:i.PrincipalID})
 respondDomain(w,out,err,http.StatusCreated)
}

// Project Workspace links are explicit, revocable *artifact manifest* channels.
// Their endpoints do not grant sandbox filesystem mounts, executables or secrets.
type workspaceLinkReader interface {
 WorkspaceLink(ctx context.Context,id string)(projectworkspace.WorkspaceLink,error)
 WorkspaceLinks(ctx context.Context,projectID string)([]projectworkspace.WorkspaceLink,error)
 CreateWorkspaceLink(ctx context.Context,c projectworkspace.CreateWorkspaceLinkCommand)(projectworkspace.WorkspaceLink,error)
 SetWorkspaceLinkEnabled(ctx context.Context,c projectworkspace.ToggleWorkspaceLinkCommand)(projectworkspace.WorkspaceLink,error)
 PublishWorkspaceAsset(ctx context.Context,c projectworkspace.PublishWorkspaceAssetCommand)(projectworkspace.WorkspacePublication,error)
 WorkspacePublications(ctx context.Context,linkID string)([]projectworkspace.WorkspacePublication,error)
}

func (s *Server) workspaceLinksService(w http.ResponseWriter) (workspaceLinkReader,bool){
 svc,ok:=s.projects.(workspaceLinkReader)
 if !ok {writeError(w,http.StatusServiceUnavailable,"Project Workspace sharing is not available");return nil,false}
 return svc,true
}
func (s *Server) projectWorkspaceLinks(w http.ResponseWriter,r *http.Request) {
 i,ok:=s.authenticate(w,r);if !ok{return}
 projectID:=strings.TrimSpace(r.PathValue("projectID"))
 p,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 permission:="project.read";if r.Method==http.MethodPost{permission="project.write"}
 if !s.authorize(w,r,i,p.WorkspaceID,permission){return}
 svc,ok:=s.workspaceLinksService(w);if !ok{return}
 if r.Method==http.MethodGet {
  result,err:=svc.WorkspaceLinks(r.Context(),projectID)
  respondDomain(w,result,err,http.StatusOK);return
 }
 var input struct{
  SourceWorkspaceID string `json:"source_workspace_id"`
  TargetWorkspaceID string `json:"target_workspace_id"`
  Name string `json:"name"`
  Enable bool `json:"enable"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.CreateWorkspaceLink(r.Context(),projectworkspace.CreateWorkspaceLinkCommand{
  ProjectID:projectID,SourceWorkspaceID:input.SourceWorkspaceID,TargetWorkspaceID:input.TargetWorkspaceID,
  Name:input.Name,Enable:input.Enable,ActorPrincipalID:i.PrincipalID})
 respondDomain(w,out,err,http.StatusCreated)
}
func (s *Server) workspaceLinkAccess(w http.ResponseWriter,r *http.Request)(workspaceLinkReader,projectworkspace.WorkspaceLink,bool){
 i,ok:=s.authenticate(w,r);if !ok{return nil,projectworkspace.WorkspaceLink{},false}
 svc,ok:=s.workspaceLinksService(w);if !ok{return nil,projectworkspace.WorkspaceLink{},false}
 link,err:=svc.WorkspaceLink(r.Context(),r.PathValue("linkID"))
 if err!=nil{respondDomain(w,nil,err,0);return nil,projectworkspace.WorkspaceLink{},false}
 p,err:=s.projects.Project(r.Context(),link.ProjectID)
 if err!=nil{respondDomain(w,nil,err,0);return nil,projectworkspace.WorkspaceLink{},false}
 permission:="project.read";if r.Method!=http.MethodGet{permission="project.write"}
 if !s.authorize(w,r,i,p.WorkspaceID,permission){return nil,projectworkspace.WorkspaceLink{},false}
 return svc,link,true
}
func (s *Server) toggleWorkspaceLink(w http.ResponseWriter,r *http.Request){
 svc,link,ok:=s.workspaceLinkAccess(w,r);if !ok{return}
 i,ok:=s.authenticate(w,r);if !ok{return}
 var input struct {
  ExpectedRevision int64 `json:"expected_revision"`
  Enabled bool `json:"enabled"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.SetWorkspaceLinkEnabled(r.Context(),projectworkspace.ToggleWorkspaceLinkCommand{
  LinkID:link.ID,ActorPrincipalID:i.PrincipalID,ExpectedRevision:input.ExpectedRevision,Enabled:input.Enabled})
 respondDomain(w,out,err,http.StatusOK)
}
func (s *Server) publishWorkspaceAsset(w http.ResponseWriter,r *http.Request){
 svc,link,ok:=s.workspaceLinkAccess(w,r);if !ok{return}
 i,ok:=s.authenticate(w,r);if !ok{return}
 var input struct {
  AssetID string `json:"asset_id"`
  Version int64 `json:"version"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.PublishWorkspaceAsset(r.Context(),projectworkspace.PublishWorkspaceAssetCommand{
  LinkID:link.ID,AssetID:input.AssetID,Version:input.Version,ActorPrincipalID:i.PrincipalID})
 respondDomain(w,out,err,http.StatusCreated)
}
func (s *Server) listWorkspacePublications(w http.ResponseWriter,r *http.Request){
 svc,link,ok:=s.workspaceLinkAccess(w,r);if !ok{return}
 out,err:=svc.WorkspacePublications(r.Context(),link.ID)
 respondDomain(w,out,err,http.StatusOK)
}
