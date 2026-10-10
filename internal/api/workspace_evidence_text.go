package api

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "net/http"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

const (
 verifiedEvidenceMaxEntries=8
 verifiedEvidenceMaxFileBytes int64=64<<10
 verifiedEvidenceMaxTotalBytes int64=256<<10
)

// Verified Workspace evidence is a human-visible read receipt, NOT a Council
// attachment or an authorisation for another Workspace or model to read data.
type verifiedWorkspaceEvidenceEntry struct{
 projectworkspace.WorkspaceEvidenceEntry
 Text string `json:"text"`
 Trust string `json:"trust"`
}
type verifiedWorkspaceEvidence struct {
 Schema string `json:"schema"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 VerifiedAtMS int64 `json:"verified_at_ms"`
 Entries []verifiedWorkspaceEvidenceEntry `json:"entries"`
}

var errEvidenceUnsafe=errors.New("selected evidence is not authorised, not textual, too large or not integrity verified")

type evidenceWorkspaceLibrary interface{
 BuildWorkspaceEvidencePacket(context.Context,string,string,[]projectworkspace.WorkspaceEvidenceSelection)(projectworkspace.WorkspaceEvidencePacket,error)
 ResolveWorkspaceLibraryVersion(context.Context,string,string,string,int64)(projectworkspace.LibraryVersion,error)
}
type evidenceArtifactReader interface{
 VerifyContent(context.Context,string)error
 Open(context.Context,string)(io.ReadCloser,artifact.Artifact,error)
}

// Reads are bounded and assembled BEFORE a response is emitted. Resolve,
// VerifyContent and Open provenance are all required. A fresh permission
// snapshot at the end prevents a revoked/retargeted Library grant becoming a
// durable read result while the individual files are being processed.
func collectVerifiedWorkspaceEvidence(
 ctx context.Context,lib evidenceWorkspaceLibrary,artifacts evidenceArtifactReader,
 projectID,workspaceID,tenantWorkspaceID string,selections []projectworkspace.WorkspaceEvidenceSelection,
 nowMS int64,
)(verifiedWorkspaceEvidence,error){
 if tenantWorkspaceID==""||lib==nil||artifacts==nil||len(selections)<1||len(selections)>verifiedEvidenceMaxEntries{
  return verifiedWorkspaceEvidence{},errEvidenceUnsafe
 }
 manifest,err:=lib.BuildWorkspaceEvidencePacket(ctx,projectID,workspaceID,selections)
 if err!=nil{return verifiedWorkspaceEvidence{},err}
 if manifest.ProjectID!=projectID||manifest.ProjectWorkspaceID!=workspaceID||
  len(manifest.Entries)!=len(selections)||manifest.ManifestSHA256==""{
  return verifiedWorkspaceEvidence{},errEvidenceUnsafe
 }
 var budget int64
 for _,entry:=range manifest.Entries{
  if entry.SizeBytes<0||entry.SizeBytes>verifiedEvidenceMaxFileBytes||
   !previewableWorkspaceLibraryMIME(entry.MIMEType) {
   return verifiedWorkspaceEvidence{},errEvidenceUnsafe
  }
  budget+=entry.SizeBytes
  if budget>verifiedEvidenceMaxTotalBytes{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
 }
 output:=verifiedWorkspaceEvidence{
  Schema:"onepane.workspace-evidence-text/v1",
  ProjectID:projectID,ProjectWorkspaceID:workspaceID,
  ManifestSHA256:manifest.ManifestSHA256,VerifiedAtMS:nowMS,
  Entries:make([]verifiedWorkspaceEvidenceEntry,0,len(manifest.Entries)),
 }
 for _,entry:=range manifest.Entries{
  if err=ctx.Err();err!=nil{return verifiedWorkspaceEvidence{},err}
  selected,e:=lib.ResolveWorkspaceLibraryVersion(ctx,projectID,workspaceID,entry.AssetID,entry.Version)
  if e!=nil{return verifiedWorkspaceEvidence{},e}
  if selected.ContentHash!=entry.ContentHash||selected.SizeBytes!=entry.SizeBytes||
   selected.MIMEType!=entry.MIMEType||!strings.HasPrefix(selected.StorageURI,"artifact:"){
   return verifiedWorkspaceEvidence{},errEvidenceUnsafe
  }
  artifactID:=strings.TrimPrefix(selected.StorageURI,"artifact:")
  if artifactID==""{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
  if err=artifacts.VerifyContent(ctx,artifactID);err!=nil{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
  reader,raw,e:=artifacts.Open(ctx,artifactID)
  if e!=nil{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
  if raw.Status!=artifact.StatusActive||raw.ContentHash!=selected.ContentHash||
   raw.SizeBytes!=selected.SizeBytes||raw.ProjectID==nil||*raw.ProjectID!=projectID||
   raw.WorkspaceID!=tenantWorkspaceID{
   _=reader.Close()
   return verifiedWorkspaceEvidence{},errEvidenceUnsafe
  }
  bytes,readErr:=readWorkspaceLibraryPreview(reader,entry.SizeBytes)
  closeErr:=reader.Close()
  if readErr!=nil||closeErr!=nil{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
  digest:=sha256.Sum256(bytes)
  actual:="sha256:"+hex.EncodeToString(digest[:])
  if actual!=selected.ContentHash{return verifiedWorkspaceEvidence{},errEvidenceUnsafe}
  output.Entries=append(output.Entries,verifiedWorkspaceEvidenceEntry{
   WorkspaceEvidenceEntry:entry,Text:string(bytes),Trust:"untrusted_library_content",
  })
 }
 // Grants/publications may have been revoked or expired during the bounded
 // artifact reads; reselect and compare all canonical metadata before return.
 latest,err:=lib.BuildWorkspaceEvidencePacket(ctx,projectID,workspaceID,selections)
 if err!=nil{return verifiedWorkspaceEvidence{},err}
 if latest.ManifestSHA256!=manifest.ManifestSHA256||
  latest.ProjectID!=projectID||latest.ProjectWorkspaceID!=workspaceID||
  len(latest.Entries)!=len(manifest.Entries){
  return verifiedWorkspaceEvidence{},errEvidenceUnsafe
 }
 return output,nil
}

func(s *Server) readVerifiedWorkspaceEvidence(w http.ResponseWriter,r *http.Request){
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false)
 if !ok{return}
 library,ok:=s.projects.(evidenceWorkspaceLibrary)
 if !ok||s.libraryArtifacts==nil{
  writeError(w,http.StatusServiceUnavailable,"Verified Workspace evidence unavailable");return
 }
 var input struct{
  Selections []projectworkspace.WorkspaceEvidenceSelection `json:"selections"`
 }
 if !decodeJSON(w,r,&input){return}
 if len(input.Selections)<1||len(input.Selections)>verifiedEvidenceMaxEntries{
  writeError(w,http.StatusBadRequest,"Select one to eight bounded evidence versions");return
 }
 result,err:=collectVerifiedWorkspaceEvidence(r.Context(),library,s.libraryArtifacts,
  p.ID,workspaceID,p.WorkspaceID,input.Selections,time.Now().UnixMilli())
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,
   "Evidence denied, unavailable or failed content/provenance checks");return
 }
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("X-Content-Type-Options","nosniff")
 respondDomain(w,result,nil,http.StatusOK)
}
