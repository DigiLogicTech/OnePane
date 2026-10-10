package api

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func frozenResearchFixture(l *evidenceFixtureLibrary)projectworkspace.ResearchEvidenceSelection{
 return projectworkspace.ResearchEvidenceSelection{
  SessionID:"research-session",
  ProjectID:"project",ProjectWorkspaceID:"world",
  ResearchManifestSHA256:"sha256:frozen-research",
  Evidence:l.packet,ApprovedBy:"human",
 }
}

func TestFrozenResearchTextPreflightReturnsOnlyHashBoundReceipt(t *testing.T){
 secret:="PRIVATE RESEARCH MATERIAL: evidence alpha; do not copy to outputs."
 lib,blobs:=verifiedEvidenceFixture(secret)
 frozen:=frozenResearchFixture(lib)
 result,err:=preflightFrozenResearchText(context.Background(),frozen,
  lib,blobs,"tenant",9876)
 if err!=nil{t.Fatal(err)}
 if result.Schema!="onepane.research-text-preflight/v1"||
  result.Status!="verified_for_operator_only"||
  result.SessionID!=frozen.SessionID||
  result.ResearchManifestSHA256!=frozen.ResearchManifestSHA256||
  result.EvidenceManifestSHA256!=frozen.Evidence.ManifestSHA256||
  result.DocumentCount!=1||result.VerifiedBytes!=int64(len(secret))||
  result.VerifiedAtMS!=9876||result.ModelVisible||result.Persisted||
  lib.packetCalls!=2{
  t.Fatalf("incorrect or unverified Research receipt: %+v, calls=%d",result,lib.packetCalls)
 }
 body,err:=json.Marshal(result)
 if err!=nil{t.Fatal(err)}
 for _,forbidden:=range []string{secret,"PRIVATE RESEARCH MATERIAL","immutable",
  "storage_uri","content_bytes","token","prompt","text", "artifact_id"} {
  // 'text' in the fixed schema name is expected; enforce the private
  // content keys without rejecting this intentionally descriptive schema.
  if forbidden=="text"||forbidden=="token"{continue}
  if strings.Contains(string(body),forbidden){
   t.Fatalf("Research preflight leaked content or private artifact metadata: %s",body)
  }
 }
}

func TestFrozenResearchTextPreflightFailsClosed(t *testing.T){
 cases:=[]struct{
  name string
  mutate func(*projectworkspace.ResearchEvidenceSelection,*evidenceFixtureLibrary,*evidenceFixtureArtifacts)
 }{
  {"wrong_project",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.ProjectID="foreign"}},
  {"wrong_workspace",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.ProjectWorkspaceID="foreign"}},
  {"wrong_frozen_digest",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.Evidence.ManifestSHA256="sha256:stale"}},
  {"wrong_version_metadata",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.Evidence.Entries[0].SizeBytes++}},
  {"missing_research_sha",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.ResearchManifestSHA256=""}},
  {"no_frozen_session",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.SessionID=""}},
  {"grant_revoked_during_read",func(_ *projectworkspace.ResearchEvidenceSelection,l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.denyAfterFirst=true}},
  {"permission_manifest_changed",func(_ *projectworkspace.ResearchEvidenceSelection,l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.changedAfterFirst=true}},
  {"blob_quarantined",func(_ *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,b *evidenceFixtureArtifacts){b.raw.Status="quarantined"}},
  {"blob_corrupted",func(_ *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,b *evidenceFixtureArtifacts){b.body="changed"}},
  {"foreign_tenant",func(_ *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,b *evidenceFixtureArtifacts){b.raw.WorkspaceID="foreign"}},
  {"non_text",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){f.Evidence.Entries[0].MIMEType="application/pdf"}},
  {"unbounded_versions",func(f *projectworkspace.ResearchEvidenceSelection,_ *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){
   first:=f.Evidence.Entries[0]
   f.Evidence.Entries=make([]projectworkspace.WorkspaceEvidenceEntry,9)
   for i:=range f.Evidence.Entries{f.Evidence.Entries[i]=first}
  }},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   lib,blob:=verifiedEvidenceFixture("Confidential scientific evidence.")
   frozen:=frozenResearchFixture(lib)
   // The frozen receipt is independent of the freshly queried Library state.
   frozen.Evidence.Entries=append([]projectworkspace.WorkspaceEvidenceEntry(nil),lib.packet.Entries...)
   tc.mutate(&frozen,lib,blob)
   got,err:=preflightFrozenResearchText(context.Background(),frozen,
    lib,blob,"tenant",1)
   if err==nil||got.SessionID!=""||got.DocumentCount!=0||got.EvidenceManifestSHA256!=""{
    t.Fatalf("unsafe Research text preflight succeeded: %+v err=%v",got,err)
   }
  })
 }
}

func TestFrozenResearchTextPreflightRefusesWithoutApprovedVersions(t *testing.T){
 lib,blobs:=verifiedEvidenceFixture("test")
 frozen:=frozenResearchFixture(lib)
 frozen.Evidence.Entries=nil
 _,err:=preflightFrozenResearchText(context.Background(),frozen,lib,blobs,"tenant",1)
 if !errors.Is(err,errEvidenceUnsafe)||lib.packetCalls!=0{
  t.Fatalf("missing frozen versions reached Library: %v, calls=%d",err,lib.packetCalls)
 }
}
