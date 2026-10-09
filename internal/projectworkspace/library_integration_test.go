//go:build integration

package projectworkspace

import (
 "context"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)
func TestProjectLibraryImmutableArtifactScopedExchange(t *testing.T){
 ctx:=context.Background()
 store,err:=sqlitestore.Open(t.TempDir()+"/library.db");if err!=nil{t.Fatal(err)}
 defer store.Close()
 if err=store.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 queries:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }
 for _,q:=range queries{if _,err=store.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}}
 svc:=NewService(store.SQL(),store,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Game Theory",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 root,err:=svc.WorkspaceViews(ctx,project.ID);if err!=nil||len(root)!=1{t.Fatalf("default Workspace: %v",err)}
 source:=root[0]
 target,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:project.ID,Name:"Story",ActorPrincipalID:"operator"});if err!=nil{t.Fatal(err)}
 blobs,err:=artifact.NewLocalStore(t.TempDir());if err!=nil{t.Fatal(err)}
 artifactSvc:=artifact.NewService(store.SQL(),store,blobs,clock.Real{})
 pID:=project.ID
 actor:="operator"
 raw,err:=artifactSvc.Create(ctx,artifact.CreateCommand{WorkspaceID:"tenant",ProjectID:&pID,MediaType:"text/plain",CreatedBy:&actor,Label:policy.DataLabel{
  WorkspaceID:"tenant",Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,Trust:policy.TrustUntrustedContent,
 }},strings.NewReader("This castle needs a hidden library"))
 if err!=nil{t.Fatal(err)}
 lib,err:=svc.ImportLibraryAsset(ctx,ImportLibraryAssetCommand{ProjectID:project.ID,Name:"Castle lore.txt",MIMEType:"text/plain",ArtifactID:raw.ID,ContentHash:raw.ContentHash,SizeBytes:raw.SizeBytes,ActorPrincipalID:"operator",SourceWorkspaceID:source.ID})
 if err!=nil{t.Fatal(err)}
 if lib.CurrentVersion!=1{t.Fatal("expected first immutable version")}
 if err=artifactSvc.VerifyContent(ctx,raw.ID);err!=nil{t.Fatal(err)}
 direct,err:=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,source.ID,lib.ID,1)
 if err!=nil||direct.ContentHash!=raw.ContentHash{t.Fatalf("uploader grant: %v %+v",err,direct)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("target accessed ungranted document")}
 link,err:=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{ProjectID:project.ID,SourceWorkspaceID:source.ID,TargetWorkspaceID:target.ID,Name:"Story sources",ActorPrincipalID:"operator",Enable:true})
 if err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("link alone must not imply broad document grant")}
 pub,err:=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:link.ID,AssetID:lib.ID,Version:1,ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 granted,err:=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1)
 if err!=nil||granted.ContentHash!=pub.ContentHash{t.Fatalf("published exact version not readable: %v %+v",err,granted)}
 // Version enumeration follows the same direct-grant and directional-link
 // permissions as the content download endpoint, not Project-wide history.
 publishedVersions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,target.ID,lib.ID)
 if err!=nil||len(publishedVersions)!=1||publishedVersions[0].Version!=1{
  t.Fatalf("enabled publication must expose exact pinned version: %+v %v",publishedVersions,err)
 }
 disabled,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{LinkID:link.ID,ExpectedRevision:link.Revision,ActorPrincipalID:"operator",Enabled:false})
 if err!=nil||disabled.Enabled{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("link revocation did not revoke publication-based access")}
 if versions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,target.ID,lib.ID);err!=ErrCrossWorkspace||len(versions)!=0{
  t.Fatalf("disabled link still exposed immutable history: %+v %v",versions,err)
 }
 if err=svc.GrantLibraryAsset(ctx,GrantLibraryAssetCommand{ProjectID:project.ID,AssetID:lib.ID,WorkspaceID:target.ID,VersionPolicy:"pinned",PinnedVersion:1,ActorPrincipalID:"operator"});err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err!=nil{t.Fatalf("direct pinned grant not honoured: %v",err)}
 if err=svc.RevokeLibraryAsset(ctx,project.ID,lib.ID,target.ID,"operator");err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("direct grant revocation did not remove access")}
 // A Workspace with a pinned older version must not be offered the newer
 // Project head version it has no permission to download.
 if err=svc.GrantLibraryAsset(ctx,GrantLibraryAssetCommand{
  ProjectID:project.ID,AssetID:lib.ID,WorkspaceID:target.ID,
  VersionPolicy:"pinned",PinnedVersion:1,ActorPrincipalID:"operator"});err!=nil{t.Fatal(err)}
 _,err=store.SQL().ExecContext(ctx,`INSERT INTO project_library_asset_versions
   (asset_id,version,content_hash,size_bytes,mime_type,storage_uri,provenance_json,created_at)
   VALUES(?,?,?,?,?,?,?,?)`,
   lib.ID,2,raw.ContentHash,raw.SizeBytes,"text/plain","artifact:"+raw.ID,`{"source":"versioned-test"}`,now+1)
 if err!=nil{t.Fatal(err)}
 _,err=store.SQL().ExecContext(ctx,`UPDATE project_library_assets
   SET current_version=2,updated_at=? WHERE id=?`,now+1,lib.ID)
 if err!=nil{t.Fatal(err)}
 pinned,err:=svc.WorkspaceLibraryAssets(ctx,project.ID,target.ID,"castle")
 if err!=nil||len(pinned)!=1||pinned[0].CurrentVersion!=2||pinned[0].AccessibleVersion!=1{
  t.Fatalf("pinned Workspace must see download-eligible v1, not Project v2: %+v, %v",pinned,err)
 }
 latest,err:=svc.WorkspaceLibraryAssets(ctx,project.ID,source.ID,"castle")
 if err!=nil||len(latest)!=1||latest[0].AccessibleVersion!=2{
  t.Fatalf("uploader Workspace latest grant must follow v2: %+v, %v",latest,err)
 }
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,2);err==nil{
  t.Fatal("pinned Workspace was allowed to fetch ungranted newer version")
 }
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err!=nil{
  t.Fatalf("pinned version v1 cannot be downloaded: %v",err)
 }
 // An older pinned grant cannot enumerate (or fetch) a newer build, while
 // the source Workspace's latest grant follows the new head only.
 targetVersions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,target.ID,lib.ID)
 if err!=nil||len(targetVersions)!=1||targetVersions[0].Version!=1{
  t.Fatalf("target pinned history leaked v2: %+v %v",targetVersions,err)
 }
 sourceVersions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,source.ID,lib.ID)
 if err!=nil||len(sourceVersions)!=1||sourceVersions[0].Version!=2{
  t.Fatalf("source latest history must contain only v2: %+v %v",sourceVersions,err)
 }
 if versions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,"foreign-workspace",lib.ID);err!=ErrCrossWorkspace||len(versions)!=0{
  t.Fatalf("foreign Workspace enumerated Library metadata: %+v %v",versions,err)
 }
 if versions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,target.ID,"foreign-asset");err!=ErrCrossWorkspace||len(versions)!=0{
  t.Fatalf("unknown/foreign asset disclosed history: %+v %v",versions,err)
 }
 // Search uses bounded server-side Project metadata and enforces the same
 // grant-filtered Workspace inventory, never scanning unapproved blob data.
 byFilename,err:=svc.SearchLibraryAssets(ctx,project.ID,"CASTLE")
 if err!=nil||len(byFilename)!=1||byFilename[0].ID!=lib.ID{
  t.Fatalf("case-insensitive Project asset search failed: %+v %v",byFilename,err)
 }
 byType,err:=svc.SearchLibraryAssets(ctx,project.ID,"TEXT/PLAIN")
 if err!=nil||len(byType)!=1||byType[0].ID!=lib.ID{
  t.Fatalf("MIME metadata search failed: %+v %v",byType,err)
 }
 permitted,err:=svc.WorkspaceLibraryAssets(ctx,project.ID,target.ID,"TEXT/PLAIN")
 if err!=nil||len(permitted)!=1||permitted[0].AccessibleVersion!=1{
  t.Fatalf("Workspace MIME search must honour its pinned grant: %+v %v",permitted,err)
 }
 missing,err:=svc.SearchLibraryAssets(ctx,project.ID,"never-present")
 if err!=nil||len(missing)!=0{t.Fatalf("search must not invent matches: %+v %v",missing,err)}
 if _,err=svc.SearchLibraryAssets(ctx,project.ID,strings.Repeat("x",257));err!=ErrInvalidCommand{
  t.Fatalf("oversized search must be rejected: %v",err)
 }
 if err=svc.RevokeLibraryAsset(ctx,project.ID,lib.ID,target.ID,"operator");err!=nil{t.Fatal(err)}
 if versions,err:=svc.WorkspaceLibraryVersions(ctx,project.ID,target.ID,lib.ID);err!=ErrCrossWorkspace||len(versions)!=0{
  t.Fatalf("revoked pinned grant still exposes version list: %+v %v",versions,err)
 }
 // Retention: revoking access never deletes the Project artifact itself.
 if err=artifactSvc.VerifyContent(ctx,raw.ID);err!=nil{t.Fatalf("revoking a link damaged immutable content: %v",err)}
}
