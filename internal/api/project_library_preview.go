package api

import (
 "errors"
 "io"
 "mime"
 "net/http"
 "strconv"
 "strings"
 "unicode/utf8"

)

const workspaceLibraryTextPreviewLimit int64=256<<10

// previewableWorkspaceLibraryMIME is only a rendering eligibility check.
// Untrusted bytes are always served as text/plain and inserted by the WebUI
// using textContent; a claimed text/html media type is never executed.
func previewableWorkspaceLibraryMIME(value string) bool {
 typ,_,err:=mime.ParseMediaType(strings.TrimSpace(value))
 if err!=nil{return false}
 typ=strings.ToLower(typ)
 if strings.HasPrefix(typ,"text/"){return true}
 switch typ {
 case "application/json","application/xml","application/javascript",
  "application/x-yaml","application/yaml","application/toml":
  return true
 default:
  return false
 }
}

var errInvalidWorkspaceLibraryPreview=errors.New("invalid bounded UTF-8 Library preview")

func readWorkspaceLibraryPreview(reader io.Reader,expected int64)([]byte,error) {
 if expected<0||expected>workspaceLibraryTextPreviewLimit {
  return nil,errInvalidWorkspaceLibraryPreview
 }
 body,err:=io.ReadAll(io.LimitReader(reader,workspaceLibraryTextPreviewLimit+1))
 if err!=nil||int64(len(body))!=expected||!utf8.Valid(body) {
  return nil,errInvalidWorkspaceLibraryPreview
 }
 return body,nil
}

// previewWorkspaceLibraryVersion is read-only and scoped to the same
// effective read grant as the immutable version download. It never provides
// an unrestricted Project-level text preview, filesystem path or blob URL.
func (s *Server) previewWorkspaceLibraryVersion(w http.ResponseWriter,r *http.Request) {
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false);if !ok{return}
 lib,ok:=s.projects.(projectLibraryService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Workspace Library unavailable");return}
 if s.libraryArtifacts==nil{
  writeError(w,http.StatusServiceUnavailable,"Managed Artifact store unavailable");return
 }
 version,err:=strconv.ParseInt(r.PathValue("version"),10,64)
 if err!=nil||version<1{writeError(w,http.StatusBadRequest,"Invalid Library version");return}
 selected,err:=lib.ResolveWorkspaceLibraryVersion(r.Context(),p.ID,workspaceID,
  r.PathValue("assetID"),version)
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !previewableWorkspaceLibraryMIME(selected.MIMEType){
  writeError(w,http.StatusUnsupportedMediaType,"Only textual Library versions can be previewed");return
 }
 if selected.SizeBytes<0||selected.SizeBytes>workspaceLibraryTextPreviewLimit{
  writeError(w,http.StatusRequestEntityTooLarge,"Preview limited to 256 KiB; download the authorised version instead");return
 }
 if !strings.HasPrefix(selected.StorageURI,"artifact:"){
  writeError(w,http.StatusServiceUnavailable,"Library version is not backed by a managed artifact");return
 }
 artifactID:=strings.TrimPrefix(selected.StorageURI,"artifact:")
 if artifactID==""{
  writeError(w,http.StatusServiceUnavailable,"Missing managed Library artifact");return
 }
 if err=s.libraryArtifacts.VerifyContent(r.Context(),artifactID);err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Library content failed integrity verification");return
 }
 reader,raw,err:=s.libraryArtifacts.Open(r.Context(),artifactID)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"Library content unavailable");return}
 defer reader.Close()
 if !libraryArtifactMatchesProject(selected,raw,p.ID,p.WorkspaceID){
  writeError(w,http.StatusForbidden,"Library provenance mismatch");return
 }
 bytes,err:=readWorkspaceLibraryPreview(reader,selected.SizeBytes)
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,"Library content is not valid bounded UTF-8 text");return
 }
 w.Header().Set("Content-Type","text/plain; charset=utf-8")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("X-OnePane-Content-Hash",selected.ContentHash)
 w.Header().Set("Content-Length",strconv.Itoa(len(bytes)))
 w.WriteHeader(http.StatusOK)
 _,_=w.Write(bytes)
}
