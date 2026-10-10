package api

import (
 "net/http"
 "strings"
 "time"
 "unicode"
 "unicode/utf8"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

const (
 verifiedEvidenceSearchMaxQueryRunes=64
 verifiedEvidenceSearchMaxHits=12
 verifiedEvidenceSearchHitsPerVersion=3
)
type workspaceEvidenceSearchHit struct{
 AssetID string `json:"asset_id"`
 Version int64 `json:"version"`
 ContentHash string `json:"content_hash"`
 RuneOffset int `json:"rune_offset"`
 Snippet string `json:"snippet"`
 Trust string `json:"trust"`
}
type workspaceEvidenceSearchResult struct{
 Schema string `json:"schema"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 VerifiedAtMS int64 `json:"verified_at_ms"`
 Query string `json:"query"`
 Hits []workspaceEvidenceSearchHit `json:"hits"`
 Truncated bool `json:"truncated"`
}

func validEvidenceQuery(value string)bool{
 n:=utf8.RuneCountInString(value)
 if n<2||n>verifiedEvidenceSearchMaxQueryRunes||
  strings.TrimSpace(value)!=value||!utf8.ValidString(value){return false}
 for _,ch:=range value{
  if unicode.IsControl(ch){return false}
 }
 return true
}
func lowerEvidenceRunes(value string)[]rune{
 lower:=[]rune(value)
 for i,ch:=range lower{lower[i]=unicode.ToLower(ch)}
 return lower
}
func matchEvidenceRunes(body,needle []rune,start int)int{
 for i:=start;i+len(needle)<=len(body);i++{
  matched:=true
  for j,ch:=range needle{
   if body[i+j]!=ch{matched=false;break}
  }
  if matched{return i}
 }
 return -1
}
// searchVerifiedEvidenceText is a bounded in-memory scan of text already
// verified and granted by collectVerifiedWorkspaceEvidence. There is no
// persistent plaintext index, browser execution, model prompt or cross-scope
// lookup; snippets remain explicitly untrusted source content.
func searchVerifiedEvidenceText(verified verifiedWorkspaceEvidence,query string)workspaceEvidenceSearchResult{
 output:=workspaceEvidenceSearchResult{
  Schema:"onepane.workspace-evidence-search/v1",
  ProjectID:verified.ProjectID,ProjectWorkspaceID:verified.ProjectWorkspaceID,
  ManifestSHA256:verified.ManifestSHA256,VerifiedAtMS:verified.VerifiedAtMS,
  Query:query,Hits:[]workspaceEvidenceSearchHit{},
 }
 needle:=lowerEvidenceRunes(query)
 for _,entry:=range verified.Entries{
  body:=[]rune(entry.Text)
  lower:=lowerEvidenceRunes(entry.Text)
  cursor:=0
  for count:=0;count<verifiedEvidenceSearchHitsPerVersion;count++{
   at:=matchEvidenceRunes(lower,needle,cursor)
   if at<0{break}
   if len(output.Hits)==verifiedEvidenceSearchMaxHits{
    output.Truncated=true
    return output
   }
   beginning:=at-60
   if beginning<0{beginning=0}
   ending:=at+len(needle)+100
   if ending>len(body){ending=len(body)}
   snippet:=string(body[beginning:ending])
   if beginning>0{snippet="…"+snippet}
   if ending<len(body){snippet+="…"}
   output.Hits=append(output.Hits,workspaceEvidenceSearchHit{
    AssetID:entry.AssetID,Version:entry.Version,ContentHash:entry.ContentHash,
    RuneOffset:at,Snippet:snippet,Trust:"untrusted_library_content",
   })
   cursor=at+len(needle)
  }
  if matchEvidenceRunes(lower,needle,cursor)>=0{output.Truncated=true}
 }
 return output
}

func(s *Server) searchVerifiedWorkspaceEvidence(w http.ResponseWriter,r *http.Request){
 p,workspaceID,_,ok:=s.workspaceRuntimeContext(w,r,false)
 if !ok{return}
 library,ok:=s.projects.(evidenceWorkspaceLibrary)
 if !ok||s.libraryArtifacts==nil{
  writeError(w,http.StatusServiceUnavailable,"Workspace text evidence search unavailable");return
 }
 var input struct{
  Selections []projectworkspace.WorkspaceEvidenceSelection `json:"selections"`
  Query string `json:"query"`
 }
 if !decodeJSON(w,r,&input){return}
 if !validEvidenceQuery(input.Query)||len(input.Selections)<1||
  len(input.Selections)>verifiedEvidenceMaxEntries{
  writeError(w,http.StatusBadRequest,"Search needs 2–64 non-control characters and 1–8 selected versions");return
 }
 verified,err:=collectVerifiedWorkspaceEvidence(r.Context(),library,s.libraryArtifacts,
  p.ID,workspaceID,p.WorkspaceID,input.Selections,time.Now().UnixMilli())
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,"Evidence selection denied or failed integrity checks");return
 }
 // On success, the manifest is revalidated against the current Workspace
 // read grants before any text snippet is returned.
 result:=searchVerifiedEvidenceText(verified,input.Query)
 w.Header().Set("Cache-Control","no-store, private")
 w.Header().Set("X-Content-Type-Options","nosniff")
 respondDomain(w,result,nil,http.StatusOK)
}
