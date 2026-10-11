package api

import (
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestEvidenceTextSearchLimitsQueryAndRejectsControls(t *testing.T){
 for _,q:=range []string{"", "a", " words", "words ", "a\nsecret",
  strings.Repeat("x",65),string([]byte{0xff,0xfe})}{
  if validEvidenceQuery(q){t.Fatalf("invalid evidence query permitted: %q",q)}
 }
 for _,q:=range []string{"ok","Árbol","🦊 fox","source code"}{
  if !validEvidenceQuery(q){t.Fatalf("valid evidence query denied: %q",q)}
 }
}

func TestEvidenceTextSearchProducesOnlyBoundedSourceSnippets(t *testing.T){
 text:=strings.Repeat("prelude ",14)+"RARE-TERM"+
  strings.Repeat("private unrelated material ",40)
 in:=verifiedWorkspaceEvidence{
  ProjectID:"p",ProjectWorkspaceID:"research",ManifestSHA256:"sha256:approved",
  Entries:[]verifiedWorkspaceEvidenceEntry{{
   WorkspaceEvidenceEntry:projectworkspace.WorkspaceEvidenceEntry{
    AssetID:"asset",Version:2,ContentHash:"sha256:immutable",
   },Text:text,Trust:"untrusted_library_content",
  }},
 }
 results:=searchVerifiedEvidenceText(in,"rare-term")
 if results.ManifestSHA256!="sha256:approved"||
  results.ProjectWorkspaceID!="research"||len(results.Hits)!=1||
  results.Hits[0].AssetID!="asset"||results.Hits[0].Version!=2||
  results.Hits[0].Trust!="untrusted_library_content"||
  !strings.Contains(results.Hits[0].Snippet,"RARE-TERM")||
  strings.Contains(results.Hits[0].Snippet,text)||
  len([]rune(results.Hits[0].Snippet))>185{
  t.Fatalf("unbounded or misattributed search result: %+v",results)
 }
}
func TestEvidenceTextSearchMatchesUnicodeAndKeepsRuneOffsets(t *testing.T){
 in:=verifiedWorkspaceEvidence{Entries:[]verifiedWorkspaceEvidenceEntry{{
  WorkspaceEvidenceEntry:projectworkspace.WorkspaceEvidenceEntry{
   AssetID:"unicode",Version:1,ContentHash:"sha256:literal",
  },Text:"🦊 CAFE acá Café CAFE",Trust:"untrusted_library_content",
 }}}
 results:=searchVerifiedEvidenceText(in,"café")
 if len(results.Hits)!=1||results.Hits[0].RuneOffset!=11{
  t.Fatalf("case-folded Unicode search offset drift: %+v",results.Hits)
 }
}
func TestEvidenceTextSearchCapsHitsAcrossMultipleVersions(t *testing.T){
 entries:=make([]verifiedWorkspaceEvidenceEntry,8)
 for i:=range entries{
  entries[i]=verifiedWorkspaceEvidenceEntry{
   WorkspaceEvidenceEntry:projectworkspace.WorkspaceEvidenceEntry{
    AssetID:"selected",Version:int64(i+1),
   },Text:strings.Repeat("match ",5),Trust:"untrusted_library_content",
  }
 }
 results:=searchVerifiedEvidenceText(verifiedWorkspaceEvidence{Entries:entries},"match")
 if len(results.Hits)!=verifiedEvidenceSearchMaxHits||!results.Truncated{
  t.Fatalf("search did not cap hits: %+v",results)
 }
 perVersion:=map[int64]int{}
 for _,hit:=range results.Hits{
  perVersion[hit.Version]++
  if perVersion[hit.Version]>verifiedEvidenceSearchHitsPerVersion{
   t.Fatalf("result version exceeded per-document match cap: %+v",hit)
  }
 }
}
