package api

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type evidenceFixtureLibrary struct {
 packet projectworkspace.WorkspaceEvidencePacket
 version projectworkspace.LibraryVersion
 packetCalls int
 denyAfterFirst bool
 changedAfterFirst bool
}
func(f *evidenceFixtureLibrary) BuildWorkspaceEvidencePacket(_ context.Context,project,workspace string,s []projectworkspace.WorkspaceEvidenceSelection)(projectworkspace.WorkspaceEvidencePacket,error){
 f.packetCalls++
 if f.denyAfterFirst&&f.packetCalls>1{return projectworkspace.WorkspaceEvidencePacket{},projectworkspace.ErrCrossWorkspace}
 if len(s)!=len(f.packet.Entries)||f.packet.ProjectID!=project||f.packet.ProjectWorkspaceID!=workspace{
  return projectworkspace.WorkspaceEvidencePacket{},projectworkspace.ErrCrossWorkspace
 }
 packet:=f.packet
 if f.changedAfterFirst&&f.packetCalls>1{packet.ManifestSHA256="sha256:changed"}
 return packet,nil
}
func(f *evidenceFixtureLibrary) ResolveWorkspaceLibraryVersion(_ context.Context,project,workspace,asset string,version int64)(projectworkspace.LibraryVersion,error){
 if project!=f.packet.ProjectID||workspace!=f.packet.ProjectWorkspaceID||
  asset!=f.version.AssetID||version!=f.version.Version{
  return projectworkspace.LibraryVersion{},projectworkspace.ErrCrossWorkspace
 }
 return f.version,nil
}
type evidenceFixtureArtifacts struct{
 body string
 raw artifact.Artifact
 rejectVerify bool
 failOpen bool
}
func(f *evidenceFixtureArtifacts) VerifyContent(context.Context,string)error{
 if f.rejectVerify{return errors.New("corrupt or unavailable artifact")}
 return nil
}
func(f *evidenceFixtureArtifacts) Open(context.Context,string)(io.ReadCloser,artifact.Artifact,error){
 if f.failOpen{return nil,artifact.Artifact{},errors.New("unavailable")}
 return io.NopCloser(strings.NewReader(f.body)),f.raw,nil
}
func verifiedEvidenceFixture(contents string)(*evidenceFixtureLibrary,*evidenceFixtureArtifacts){
 digest:=sha256.Sum256([]byte(contents))
 hash:="sha256:"+hex.EncodeToString(digest[:])
 project:="project"
 packet:=projectworkspace.WorkspaceEvidencePacket{
  Schema:"onepane.workspace-evidence-metadata/v1",
  ProjectID:project,ProjectWorkspaceID:"world",ManifestSHA256:"sha256:manifest",
  Entries:[]projectworkspace.WorkspaceEvidenceEntry{{
   AssetID:"a",Version:1,Name:"hello.md",ContentHash:hash,
   SizeBytes:int64(len(contents)),MIMEType:"text/markdown",CreatedAt:1,
  }},
 }
 lib:=&evidenceFixtureLibrary{
  packet:packet,
  version:projectworkspace.LibraryVersion{
   AssetID:"a",Version:1,ContentHash:hash,SizeBytes:int64(len(contents)),
   MIMEType:"text/markdown",StorageURI:"artifact:immutable",CreatedAt:1,
  },
 }
 artifacts:=&evidenceFixtureArtifacts{body:contents,raw:artifact.Artifact{
  ProjectID:&project,WorkspaceID:"tenant",ContentHash:hash,
  SizeBytes:int64(len(contents)),Status:artifact.StatusActive,
 }}
 return lib,artifacts
}
func readFixture(lib *evidenceFixtureLibrary,blob *evidenceFixtureArtifacts)(verifiedWorkspaceEvidence,error){
 return collectVerifiedWorkspaceEvidence(context.Background(),lib,blob,"project","world",
  []projectworkspace.WorkspaceEvidenceSelection{{AssetID:"a",Version:1}},12345)
}
func TestVerifiedWorkspaceEvidenceReadsHashBoundedUntrustedText(t *testing.T){
 text:="# Notes\n<script>this is untrusted text, not UI code</script>\n"
 lib,blob:=verifiedEvidenceFixture(text)
 received,err:=readFixture(lib,blob)
 if err!=nil{t.Fatal(err)}
 if received.ManifestSHA256!="sha256:manifest"||received.ProjectWorkspaceID!="world"||
  len(received.Entries)!=1||received.Entries[0].Text!=text||
  received.Entries[0].Trust!="untrusted_library_content"||
  lib.packetCalls!=2{
  t.Fatalf("verified evidence was not constructed from two access checks: %+v",received)
 }
}
func TestVerifiedWorkspaceEvidenceFailsClosedOnAllUnsafeSources(t *testing.T){
 cases:=[]struct{
  name string
  mutate func(*evidenceFixtureLibrary,*evidenceFixtureArtifacts)
 }{
  {"revoked_between_checks",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.denyAfterFirst=true}},
  {"manifest_changed_between_checks",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.changedAfterFirst=true}},
  {"bytes_corrupted_after_verification",func(_ *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){a.body="tampered"}},
  {"blob_verification_failed",func(_ *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){a.rejectVerify=true}},
  {"blob_open_failed",func(_ *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){a.failOpen=true}},
  {"blob_quarantined",func(_ *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){a.raw.Status=artifact.StatusQuarantined}},
  {"wrong_tenant_project",func(_ *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){v:="other";a.raw.ProjectID=&v}},
  {"wrong_content_hash",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.version.ContentHash="sha256:wrong"}},
  {"wrong_mime",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.version.MIMEType="application/octet-stream"}},
  {"not_managed_blob",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.version.StorageURI="file:/host/secrets"}},
  {"binary_mime",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.packet.Entries[0].MIMEType="application/pdf"}},
  {"invalid_utf8",func(l *evidenceFixtureLibrary,a *evidenceFixtureArtifacts){
   a.body=string([]byte{0xff,0xfe});l.version.ContentHash=l.packet.Entries[0].ContentHash
  }},
  {"size_overrun",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.packet.Entries[0].SizeBytes=verifiedEvidenceMaxFileBytes+1}},
  {"size_mismatch",func(l *evidenceFixtureLibrary,_ *evidenceFixtureArtifacts){l.packet.Entries[0].SizeBytes++}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   lib,blob:=verifiedEvidenceFixture("Hello evidence")
   tc.mutate(lib,blob)
   value,err:=readFixture(lib,blob)
   if err==nil||len(value.Entries)>0||value.ManifestSHA256!=""{
    t.Fatalf("unsafe evidence was partially released: %+v error=%v",value,err)
   }
  })
 }
}
func TestVerifiedWorkspaceEvidenceRejectsUnboundedRequest(t *testing.T){
 lib,blob:=verifiedEvidenceFixture("safe")
 for _,n:=range []int{0,9,16}{
  req:=make([]projectworkspace.WorkspaceEvidenceSelection,n)
  if _,err:=collectVerifiedWorkspaceEvidence(context.Background(),lib,blob,
   "project","world",req,0);!errors.Is(err,errEvidenceUnsafe){
   t.Fatalf("unbounded size %d accepted: %v",n,err)
  }
 }
 if lib.packetCalls!=0{t.Fatal("invalid selections reached Library service")}
}
