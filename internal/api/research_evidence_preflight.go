package api

import (
 "context"
 "net/http"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// A Research evidence preflight is a single, non-persistent operator request.
// It reads and verifies the exact already-frozen text versions, then returns
// only a small receipt. No evidence contents, snippets, blob paths, lease or
// new model authority may cross the API response boundary.
type researchTextPreflightReceipt struct {
 Schema string `json:"schema"`
 Status string `json:"status"`
 SessionID string `json:"session_id"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ResearchManifestSHA256 string `json:"research_manifest_sha256"`
 EvidenceManifestSHA256 string `json:"evidence_manifest_sha256"`
 DocumentCount int `json:"document_count"`
 VerifiedBytes int64 `json:"verified_bytes"`
 VerifiedAtMS int64 `json:"verified_at_ms"`
 ModelVisible bool `json:"model_visible"`
 Persisted bool `json:"persisted"`
}

// The frozen approval is the ONLY source of selections; caller-supplied asset
// IDs are forbidden. Reuse the same byte/ownership checks as operator text
// review, including post-read manifest and permission revalidation.
func preflightFrozenResearchText(
 ctx context.Context,frozen projectworkspace.ResearchEvidenceSelection,
 lib evidenceWorkspaceLibrary,blobs evidenceArtifactReader,
 tenantWorkspaceID string,nowMS int64,
)(researchTextPreflightReceipt,error){
 empty:=researchTextPreflightReceipt{}
 packet:=frozen.Evidence
 if frozen.SessionID==""||frozen.ProjectID==""||frozen.ProjectWorkspaceID==""||
  frozen.ResearchManifestSHA256==""||packet.ManifestSHA256==""||
  packet.ProjectID!=frozen.ProjectID||packet.ProjectWorkspaceID!=frozen.ProjectWorkspaceID||
  packet.Schema!="onepane.workspace-evidence-metadata/v1"||
  len(packet.Entries)<1||len(packet.Entries)>verifiedEvidenceMaxEntries {
  return empty,errEvidenceUnsafe
 }
 selections:=make([]projectworkspace.WorkspaceEvidenceSelection,0,len(packet.Entries))
 for _,entry:=range packet.Entries{
  selections=append(selections,projectworkspace.WorkspaceEvidenceSelection{
   AssetID:entry.AssetID,Version:entry.Version,
  })
 }
 verified,err:=collectVerifiedWorkspaceEvidence(ctx,lib,blobs,
  frozen.ProjectID,frozen.ProjectWorkspaceID,tenantWorkspaceID,selections,nowMS)
 if err!=nil{return empty,err}
 if verified.ManifestSHA256!=packet.ManifestSHA256||
  len(verified.Entries)!=len(packet.Entries){
  return empty,errEvidenceUnsafe
 }
 total:=int64(0)
 for i,entry:=range verified.Entries {
  if entry.WorkspaceEvidenceEntry!=packet.Entries[i]||
   entry.Trust!="untrusted_library_content" {
   return empty,errEvidenceUnsafe
  }
  total+=entry.SizeBytes
 }
 return researchTextPreflightReceipt{
  Schema:"onepane.research-text-preflight/v1",
  Status:"verified_for_operator_only",
  SessionID:frozen.SessionID,
  ProjectID:frozen.ProjectID,ProjectWorkspaceID:frozen.ProjectWorkspaceID,
  ResearchManifestSHA256:frozen.ResearchManifestSHA256,
  EvidenceManifestSHA256:packet.ManifestSHA256,
  DocumentCount:len(verified.Entries),VerifiedBytes:total,
  VerifiedAtMS:verified.VerifiedAtMS,
  ModelVisible:false,Persisted:false,
 },nil
}

func(s *Server) preflightResearchEvidenceText(w http.ResponseWriter,r *http.Request){
 identity,session,ok:=s.sessionFor(w,r,"team.read")
 if !ok{return}
 if !s.authorize(w,r,identity,session.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(researchEvidenceService)
 if !ok||s.libraryArtifacts==nil{
  writeError(w,http.StatusServiceUnavailable,"Research text preflight unavailable");return
 }
 library,ok:=s.projects.(evidenceWorkspaceLibrary)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"Research text preflight unavailable");return
 }
 frozen,err:=svc.ResearchEvidence(r.Context(),session.ID,identity.PrincipalID)
 if err!=nil{
  // Never disclose evidence based on a stale human approval or revoked grant.
  writeError(w,http.StatusUnprocessableEntity,"Research approval unavailable or no longer authorised");return
 }
 receipt,err:=preflightFrozenResearchText(r.Context(),frozen,library,
  s.libraryArtifacts,session.WorkspaceID,time.Now().UnixMilli())
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,
   "Frozen Research text is unavailable, non-text, oversized or failed verification");return
 }
 // Permission/revocation can change while artifact bytes are read. Require
 // the still-authorised exact immutable receipt again immediately before reply.
 fresh,err:=svc.ResearchEvidence(r.Context(),session.ID,identity.PrincipalID)
 if err!=nil||fresh.ProjectID!=frozen.ProjectID||
  fresh.ProjectWorkspaceID!=frozen.ProjectWorkspaceID||
  fresh.ResearchManifestSHA256!=frozen.ResearchManifestSHA256||
  fresh.Evidence.ManifestSHA256!=frozen.Evidence.ManifestSHA256{
  writeError(w,http.StatusUnprocessableEntity,
   "Research approval changed or was revoked during verification");return
 }
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("X-Content-Type-Options","nosniff")
 writeJSON(w,http.StatusOK,receipt)
}
