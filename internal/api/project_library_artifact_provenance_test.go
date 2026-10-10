package api

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestLibraryArtifactProvenanceRequiresActiveExactProjectTenantAndHash(t *testing.T){
 project:="project-one"
 version:=projectworkspace.LibraryVersion{
  AssetID:"asset-one",Version:1,ContentHash:"sha256:abc",SizeBytes:42,
 }
 live:=artifact.Artifact{
  ID:"art-one",ProjectID:&project,WorkspaceID:"tenant",
  ContentHash:"sha256:abc",SizeBytes:42,Status:artifact.StatusActive,
 }
 if !libraryArtifactMatchesProject(version,live,project,"tenant"){
  t.Fatal("known valid Project Library provenance rejected")
 }
 cases:=[]struct{
  name string
  mutate func(*projectworkspace.LibraryVersion,*artifact.Artifact)
 }{
  {"quarantined",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.Status=artifact.StatusQuarantined}},
  {"archived",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.Status=artifact.StatusArchived}},
  {"corrupted",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.Status=artifact.StatusCorrupted}},
  {"missing_project",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.ProjectID=nil}},
  {"foreign_project",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){v:="other";a.ProjectID=&v}},
  {"foreign_tenant",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.WorkspaceID="other-tenant"}},
  {"wrong_content_hash",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.ContentHash="sha256:tampered"}},
  {"wrong_size",func(_ *projectworkspace.LibraryVersion,a *artifact.Artifact){a.SizeBytes=999}},
  {"empty_recorded_hash",func(v *projectworkspace.LibraryVersion,_ *artifact.Artifact){v.ContentHash=""}},
  {"negative_version_size",func(v *projectworkspace.LibraryVersion,_ *artifact.Artifact){v.SizeBytes=-1}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   v,a:=version,live
   tc.mutate(&v,&a)
   if libraryArtifactMatchesProject(v,a,project,"tenant"){
    t.Fatal("unsafe managed artifact provenance accepted")
   }
  })
 }
 if libraryArtifactMatchesProject(version,live,"","tenant")||
  libraryArtifactMatchesProject(version,live,project,""){
  t.Fatal("empty scope must not authorise Library download")
 }
}
