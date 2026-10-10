package api

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "path/filepath"
 "strconv"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type projectLibraryService interface {
 ImportLibraryAsset(context.Context,projectworkspace.ImportLibraryAssetCommand)(projectworkspace.LibraryAsset,error)
 LibraryAssets(context.Context,string)([]projectworkspace.LibraryAsset,error)
 WorkspaceLibraryAssets(context.Context,string,string,string)([]projectworkspace.LibraryAsset,error)
 WorkspaceLibraryVersions(context.Context,string,string,string)([]projectworkspace.LibraryVersion,error)
 WorkspacePublishedOutputs(context.Context,string,string)([]projectworkspace.WorkspacePublishedOutput,error)
 WorkspacePublicationReviews(context.Context,string,string)([]projectworkspace.WorkspacePublicationReview,error)
 LibraryVersions(context.Context,string,string)([]projectworkspace.LibraryVersion,error)
 GrantLibraryAsset(context.Context,projectworkspace.GrantLibraryAssetCommand)error
 RevokeLibraryAsset(context.Context,string,string,string,string)error
 ResolveWorkspaceLibraryVersion(context.Context,string,string,string,int64)(projectworkspace.LibraryVersion,error)
}
func (s *Server) projectLibraryAccess(w http.ResponseWriter,r *http.Request,write bool)(projectLibraryService,projectworkspace.Project,string,bool) {
 i,ok:=s.authenticate(w,r)
 if !ok{return nil,projectworkspace.Project{},"",false}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return nil,projectworkspace.Project{},"",false}
 permission:="project.read";if write{permission="project.write"}
 if !s.authorize(w,r,i,p.WorkspaceID,permission){return nil,projectworkspace.Project{},"",false}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Project Library unavailable");return nil,projectworkspace.Project{},"",false}
 return lib,p,i.PrincipalID,true
}
func (s *Server) listWorkspaceLibrary(w http.ResponseWriter,r *http.Request){
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace Library unavailable");return}
 items,err:=lib.WorkspaceLibraryAssets(r.Context(),p.ID,workspaceID,r.URL.Query().Get("q"))
 respondDomain(w,items,err,http.StatusOK)
}
// Packet creation is a read-only permission-filtered selection. It does
// not attach evidence to a Council/Task or provide a transferable read token.
func(s *Server) buildWorkspaceEvidencePacket(w http.ResponseWriter,r *http.Request){
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false)
 if !ok{return}
 builder,ok:=s.projects.(interface{
  BuildWorkspaceEvidencePacket(context.Context,string,string,[]projectworkspace.WorkspaceEvidenceSelection)(projectworkspace.WorkspaceEvidencePacket,error)
 })
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace evidence selection unavailable");return}
 var input struct{
  Selections []projectworkspace.WorkspaceEvidenceSelection `json:"selections"`
 }
 if !decodeJSON(w,r,&input){return}
 receipt,err:=builder.BuildWorkspaceEvidencePacket(r.Context(),p.ID,workspaceID,input.Selections)
 respondDomain(w,receipt,err,http.StatusOK)
}

// A Workspace never receives the unrestricted Project-wide asset history.
// Each version is independently filtered by the currently effective read grant
// or enabled directional publication, including pinned-versus-latest policy.
func (s *Server) listWorkspaceLibraryVersions(w http.ResponseWriter,r *http.Request) {
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace Library unavailable");return}
 versions,err:=lib.WorkspaceLibraryVersions(r.Context(),p.ID,workspaceID,r.PathValue("assetID"))
 respondDomain(w,versions,err,http.StatusOK)
}

// Completed, source-verified Task outputs can be listed only through the
// caller's active canonical Workspace; content still requires a fresh
// per-version download authorisation.
func (s *Server) listWorkspacePublishedOutputs(w http.ResponseWriter,r *http.Request) {
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace Task outputs unavailable");return}
 outputs,err:=lib.WorkspacePublishedOutputs(r.Context(),p.ID,workspaceID)
 respondDomain(w,outputs,err,http.StatusOK)
}

// Read-only operator visibility for aged unresolved Workspace publications.
// This endpoint never starts a Task, reuses credentials or retries a blob write.
func (s *Server) listWorkspacePublicationReviews(w http.ResponseWriter,r *http.Request) {
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace publication reviews unavailable");return}
 reviews,err:=lib.WorkspacePublicationReviews(r.Context(),p.ID,workspaceID)
 respondDomain(w,reviews,err,http.StatusOK)
}

func (s *Server) listProjectLibrary(w http.ResponseWriter,r *http.Request) {
 lib,p,_,ok:=s.projectLibraryAccess(w,r,false);if !ok{return}
 query:=strings.TrimSpace(r.URL.Query().Get("q"))
 if query!="" {
  search,ok:=s.projects.(interface{
   SearchLibraryAssets(context.Context,string,string)([]projectworkspace.LibraryAsset,error)
  })
  if !ok{writeError(w,http.StatusServiceUnavailable,"Project Library search unavailable");return}
  assets,err:=search.SearchLibraryAssets(r.Context(),p.ID,query)
  respondDomain(w,assets,err,http.StatusOK)
  return
 }
 assets,err:=lib.LibraryAssets(r.Context(),p.ID)
 respondDomain(w,assets,err,http.StatusOK)
}
func (s *Server) listProjectLibraryVersions(w http.ResponseWriter,r *http.Request) {
 lib,p,_,ok:=s.projectLibraryAccess(w,r,false);if !ok{return}
 versions,err:=lib.LibraryVersions(r.Context(),p.ID,r.PathValue("assetID"))
 respondDomain(w,versions,err,http.StatusOK)
}
func (s *Server) uploadProjectLibrary(w http.ResponseWriter,r *http.Request) {
 lib,p,actor,ok:=s.projectLibraryAccess(w,r,true);if !ok{return}
 if s.libraryArtifacts==nil{writeError(w,http.StatusServiceUnavailable,"Artifact blob store unavailable");return}
 // Streaming multipart upload. Never allow ParseMultipartForm to spool large
 // content to a global Windows/system temporary directory.
 const limit int64=32<<20
 r.Body=http.MaxBytesReader(w,r.Body,limit+16384)
 multipart,err:=r.MultipartReader()
 if err!=nil{writeError(w,http.StatusBadRequest,"expected multipart file upload");return}
 sourceWorkspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 var uploaded artifact.Artifact
 var name string
 var gotFile bool
 for {
  part,e:=multipart.NextPart()
  if e==io.EOF{break}
  if e!=nil{writeError(w,http.StatusBadRequest,"invalid or oversized multipart upload");return}
  if part.FormName()!="file"||part.FileName()==""{
   _=part.Close();continue
  }
  if gotFile{_ = part.Close();writeError(w,http.StatusBadRequest,"only one file per upload");return}
  gotFile=true
  name=filepath.Base(strings.ReplaceAll(part.FileName(),"\\","/"))
  if strings.TrimSpace(name)==""||len(name)>240||name=="."{
   _=part.Close();writeError(w,http.StatusBadRequest,"invalid filename");return
  }
  mime:=strings.TrimSpace(part.Header.Get("Content-Type"))
  if mime==""||len(mime)>120{mime="application/octet-stream"}
  projectID:=p.ID
  label:=policy.DataLabel{WorkspaceID:p.WorkspaceID,Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,Trust:policy.TrustUntrustedContent}
  // Store is content-addressed and verified when accessed. Source content is
  // untrusted even if the uploader or filename claims it is executable.
  uploaded,err=s.libraryArtifacts.Create(r.Context(),artifact.CreateCommand{
   WorkspaceID:p.WorkspaceID,ProjectID:&projectID,MediaType:mime,Label:label,CreatedBy:&actor,ActorPrincipalID:&actor,
   Metadata:json.RawMessage(`{"source":"project_library_upload"}`)},part)
  _=part.Close()
  if err!=nil{writeError(w,http.StatusBadRequest,"artifact upload failed: "+err.Error());return}
  imported,err:=lib.ImportLibraryAsset(r.Context(),projectworkspace.ImportLibraryAssetCommand{
   ProjectID:p.ID,Name:name,MIMEType:mime,ArtifactID:uploaded.ID,ContentHash:uploaded.ContentHash,
   SizeBytes:uploaded.SizeBytes,ActorPrincipalID:actor,SourceWorkspaceID:sourceWorkspace})
  respondDomain(w,imported,err,http.StatusCreated)
  return
 }
 writeError(w,http.StatusBadRequest,"multipart file missing")
}
// Adopt an already-managed artifact from a previous OnePane build without
// duplicating or moving its bytes. Never accept arbitrary filesystem paths.
func (s *Server) adoptManagedProjectArtifact(w http.ResponseWriter,r *http.Request){
 lib,p,actor,ok:=s.projectLibraryAccess(w,r,true);if !ok{return}
 if s.libraryArtifacts==nil{writeError(w,http.StatusServiceUnavailable,"Artifact store unavailable");return}
 var in struct{
  ArtifactID string `json:"artifact_id"`
  Name string `json:"name"`
  SourceWorkspaceID string `json:"source_workspace_id"`
 }
 if !decodeJSON(w,r,&in){return}
 in.ArtifactID=strings.TrimSpace(in.ArtifactID)
 if in.ArtifactID==""||strings.ContainsAny(in.ArtifactID,"/\\\\") {
  writeError(w,http.StatusBadRequest,"managed artifact ID required");return
 }
 a,err:=s.libraryArtifacts.Get(r.Context(),in.ArtifactID)
 if err!=nil{writeError(w,http.StatusNotFound,"managed artifact not found");return}
 if a.WorkspaceID!=p.WorkspaceID||(a.ProjectID!=nil&&*a.ProjectID!=p.ID){
  writeError(w,http.StatusForbidden,"artifact belongs to a different Project or tenancy");return
 }
 if a.Status!=artifact.StatusActive{
  writeError(w,http.StatusConflict,"artifact is not active; review its integrity before adoption");return
 }
 if err=s.libraryArtifacts.VerifyContent(r.Context(),a.ID);err!=nil{
  writeError(w,http.StatusConflict,"stored artifact failed integrity verification");return
 }
 if strings.TrimSpace(in.Name)==""{in.Name="Recovered artifact "+a.ID}
 item,err:=lib.ImportLibraryAsset(r.Context(),projectworkspace.ImportLibraryAssetCommand{
  ProjectID:p.ID,Name:in.Name,MIMEType:a.MediaType,ArtifactID:a.ID,
  ContentHash:a.ContentHash,SizeBytes:a.SizeBytes,
  SourceWorkspaceID:in.SourceWorkspaceID,ActorPrincipalID:actor})
 respondDomain(w,item,err,http.StatusCreated)
}
func (s *Server) grantProjectLibrary(w http.ResponseWriter,r *http.Request) {
 lib,p,actor,ok:=s.projectLibraryAccess(w,r,true);if !ok{return}
 var in struct{
  WorkspaceID string `json:"workspace_id"`
  VersionPolicy string `json:"version_policy"`
  PinnedVersion int64 `json:"pinned_version"`
 }
 if !decodeJSON(w,r,&in){return}
 if in.VersionPolicy==""{in.VersionPolicy="latest"}
 err:=lib.GrantLibraryAsset(r.Context(),projectworkspace.GrantLibraryAssetCommand{
  ProjectID:p.ID,AssetID:r.PathValue("assetID"),WorkspaceID:in.WorkspaceID,
  ActorPrincipalID:actor,VersionPolicy:in.VersionPolicy,PinnedVersion:in.PinnedVersion})
 respondDomain(w,map[string]any{"granted":err==nil},err,http.StatusOK)
}
func (s *Server) revokeProjectLibraryGrant(w http.ResponseWriter,r *http.Request){
 lib,p,actor,ok:=s.projectLibraryAccess(w,r,true);if !ok{return}
 err:=lib.RevokeLibraryAsset(r.Context(),p.ID,r.PathValue("assetID"),r.PathValue("workspaceID"),actor)
 respondDomain(w,map[string]any{"revoked":err==nil},err,http.StatusOK)
}
func (s *Server) downloadWorkspaceLibraryVersion(w http.ResponseWriter,r *http.Request){
 lib,p,_,ok:=s.projectLibraryAccess(w,r,false);if !ok{return}
 if s.libraryArtifacts==nil{writeError(w,http.StatusServiceUnavailable,"Artifact blob store unavailable");return}
 workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 version,err:=strconv.ParseInt(r.PathValue("version"),10,64)
 if err!=nil||version<1{writeError(w,http.StatusBadRequest,"invalid asset version");return}
 selected,err:=lib.ResolveWorkspaceLibraryVersion(r.Context(),p.ID,workspaceID,r.PathValue("assetID"),version)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !strings.HasPrefix(selected.StorageURI,"artifact:"){
  writeError(w,http.StatusServiceUnavailable,"Library version cannot be downloaded from a non-artifact source");return
 }
 artifactID:=strings.TrimPrefix(selected.StorageURI,"artifact:")
 if err=s.libraryArtifacts.VerifyContent(r.Context(),artifactID);err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Library content verification failed");return
 }
 reader,raw,err:=s.libraryArtifacts.Open(r.Context(),artifactID)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"Library blob unavailable");return}
 defer reader.Close()
 if !libraryArtifactMatchesProject(selected,raw,p.ID,p.WorkspaceID){
  writeError(w,http.StatusForbidden,"Library provenance mismatch");return
 }
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("Content-Type","application/octet-stream")
 w.Header().Set("Content-Disposition",fmt.Sprintf("attachment; filename=\"onepane-%s-v%d\"",r.PathValue("assetID"),version))
 w.Header().Set("X-OnePane-Content-Hash",selected.ContentHash)
 w.Header().Set("Content-Length",strconv.FormatInt(selected.SizeBytes,10))
 w.WriteHeader(http.StatusOK)
 _,_=io.Copy(w,reader)
}
