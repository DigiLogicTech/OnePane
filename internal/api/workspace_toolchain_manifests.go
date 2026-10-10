package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type workspaceToolchainManifestService interface {
 WorkspaceToolchainManifest(context.Context,string,string)(projectworkspace.ApprovedToolchainManifest,error)
 ApproveWorkspaceToolchainManifest(context.Context,projectworkspace.ApproveToolchainManifestCommand)(projectworkspace.ApprovedToolchainManifest,error)
}

// Both routes recheck the same canonical Project/Workspace membership used by
// the sandbox runtime routes; a manifest is a durable human approval, not an
// untrusted AI prompt or a proxy for tool execution permission.
func (s *Server) getWorkspaceToolchainManifest(w http.ResponseWriter,r *http.Request){
 p,workspace,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 svc,ok:=s.projects.(workspaceToolchainManifestService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace toolchain manifests unavailable");return}
 out,err:=svc.WorkspaceToolchainManifest(r.Context(),p.ID,workspace)
 respondDomain(w,out,err,http.StatusOK)
}

func (s *Server) approveWorkspaceToolchainManifest(w http.ResponseWriter,r *http.Request){
 p,workspace,principal,ok:=s.workspaceRuntimeContext(w,r,true);if !ok{return}
 svc,ok:=s.projects.(workspaceToolchainManifestService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace toolchain manifests unavailable");return}
 var input struct{
  ApplicationID string `json:"application_id"`
  ExpectedRevision int64 `json:"expected_revision"`
  Requirements []projectworkspace.ToolchainRequirement `json:"requirements"`
 }
 if !decodeJSON(w,r,&input){return}
 out,err:=svc.ApproveWorkspaceToolchainManifest(r.Context(),projectworkspace.ApproveToolchainManifestCommand{
  ProjectID:p.ID,ProjectWorkspaceID:workspace,
  ApplicationID:input.ApplicationID,ExpectedRevision:input.ExpectedRevision,
  Requirements:input.Requirements,ActorPrincipalID:principal,
  RequestID:headerPtr(r,"X-Request-ID"),TraceID:headerPtr(r,"X-Trace-ID"),
 })
 respondDomain(w,out,err,http.StatusOK)
}
