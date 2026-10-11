//go:build integration

package projectworkspace

import (
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestWorkspaceArtifactChannelsEnforceProjectAndVersionBoundaries(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/links.db");if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 seeds:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant-a','Tenant A','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant-a','owner','active',?,?)`,
 }
 for i,q:=range seeds{args:=[]any{now,now};if i==2{args=[]any{now,now}}
  if _,err=db.SQL().ExecContext(ctx,q,args...);err!=nil{t.Fatalf("seed %d: %v",i,err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant-a",Name:"Game Development",CreatedBy:"owner"})
 if err!=nil{t.Fatal(err)}
 initial,err:=svc.WorkspaceViews(ctx,p.ID);if err!=nil||len(initial)!=1{t.Fatalf("source workspaces: %v %#v",err,initial)}
 world,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"World Builder",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 story,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Storyline",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 source:=initial[0]
 link,err:=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{ProjectID:p.ID,SourceWorkspaceID:source.ID,TargetWorkspaceID:world.ID,Name:"Research -> World",ActorPrincipalID:"owner",Enable:true})
 if err!=nil{t.Fatal(err)}
 if !link.Enabled{t.Fatal("operator explicitly enabled link")}
 if _,err=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{ProjectID:p.ID,SourceWorkspaceID:source.ID,TargetWorkspaceID:source.ID,Name:"Unsafe",ActorPrincipalID:"owner",Enable:true});err==nil{t.Fatal("self-links must be denied")}
 if _,err=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{ProjectID:p.ID,SourceWorkspaceID:source.ID,TargetWorkspaceID:"does-not-exist",Name:"Unsafe",ActorPrincipalID:"owner",Enable:true});err==nil{t.Fatal("unknown targets must be denied")}
 assetID:="asset-forest"
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,?,?,?,2,?,?)`,assetID,p.ID,"Forest map","application/json",now,now);err!=nil{t.Fatal(err)}
 for version,hash:=range map[int]string{1:"sha256-old",2:"sha256-new"}{
  if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,mime_type,storage_uri,created_at) VALUES(?,?,?,'application/json',?,?)`,assetID,version,hash,"artifact:"+hash,now);err!=nil{t.Fatal(err)}
 }
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:link.ID,AssetID:assetID,Version:2,ActorPrincipalID:"owner"});err==nil{t.Fatal("publication without source grant must fail")}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,version_policy,permissions_json,created_at,updated_at) VALUES(?,?,?,'latest','{"read":true,"create_derivative":true}',?,?)`,
  "grant-source",source.ID,assetID,now,now);err!=nil{t.Fatal(err)}
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:link.ID,AssetID:assetID,Version:1,ActorPrincipalID:"owner"});err==nil{t.Fatal("latest grant must not publish obsolete version")}
 published,err:=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:link.ID,AssetID:assetID,Version:2,ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 if published.ContentHash!="sha256-new"||published.AssetVersion!=2{t.Fatalf("source hash/version mismatch: %+v",published)}
 visible,err:=svc.WorkspacePublications(ctx,link.ID)
 if err!=nil||len(visible)!=1{t.Fatalf("channel read: %v %+v",err,visible)}
 worldLink,err:=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{ProjectID:p.ID,SourceWorkspaceID:world.ID,TargetWorkspaceID:story.ID,Name:"World -> Story",ActorPrincipalID:"owner",Enable:true})
 if err!=nil{t.Fatal(err)}
 // Seeing an upstream publication does NOT let the World Workspace forward
 // someone else's work to Storyline without its own source derivative grant.
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:worldLink.ID,AssetID:assetID,Version:2,ActorPrincipalID:"owner"});err==nil{
  t.Fatal("ungranted re-publication to downstream Workspace succeeded")
 }
 worldAsset:="asset-world-build"
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,worldAsset,p.ID,"World scene","application/json",now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,mime_type,storage_uri,created_at) VALUES(?,1,'sha256-world','application/json','artifact:world',?)`,worldAsset,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,permissions_json,created_at,updated_at) VALUES(?,?,?,'{"read":true,"create_derivative":true}',?,?)`,
  "grant-world",world.ID,worldAsset,now,now);err!=nil{t.Fatal(err)}
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:worldLink.ID,AssetID:worldAsset,Version:1,ActorPrincipalID:"owner"});err!=nil{t.Fatal(err)}
 visibleStory,err:=svc.WorkspacePublications(ctx,worldLink.ID)
 if err!=nil||len(visibleStory)!=1||visibleStory[0].AssetID!=worldAsset{t.Fatalf("World -> Storyline publication: %v %+v",err,visibleStory)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,worldAsset,1);err!=nil{t.Fatalf("Storyline should receive explicitly published World artifact: %v",err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,assetID,2);err==nil{t.Fatal("upstream research asset leaked transitively into Storyline")}


 // A single explicit World -> Story artifact grant may have an expiry. A
 // link that has expired must not be usable through *any* Library read path,
 // regardless of immutable publication history or desired enabled state.
 expires:=clock.Real{}.UnixMilli()+60000
 timed,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{
  LinkID:worldLink.ID,ActorPrincipalID:"owner",ExpectedRevision:worldLink.Revision,
  Enabled:true,ExpiresAtMS:&expires,
 })
 if err!=nil||timed.ExpiresAtMS==nil||*timed.ExpiresAtMS!=expires||timed.Expired{
  t.Fatalf("operator expiry not persisted: %+v %v",timed,err)
 }
 versions,err:=svc.WorkspaceLibraryVersions(ctx,p.ID,story.ID,worldAsset)
 if err!=nil||len(versions)!=1||versions[0].Version!=1{
  t.Fatalf("valid expiring link did not expose exact approved version: %+v %v",versions,err)
 }
 // The database does not delete rows or mutate any Library blob on expiry.
 expiredAt:=clock.Real{}.UnixMilli()-1000
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE project_workspace_links SET expires_at_ms=? WHERE id=?`,
  expiredAt,worldLink.ID);err!=nil{t.Fatal(err)}
 expired,err:=svc.WorkspaceLink(ctx,worldLink.ID)
 if err!=nil||!expired.Expired||!expired.Enabled{
  t.Fatalf("expired link was not exposed as an expired policy: %+v %v",expired,err)
 }
 if _,err=svc.WorkspacePublications(ctx,worldLink.ID);err==nil{
  t.Fatal("expired channel still exposed metadata")
 }
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{
  LinkID:worldLink.ID,AssetID:worldAsset,Version:1,ActorPrincipalID:"owner",
 });err==nil{t.Fatal("expired channel allowed a new publication")}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,worldAsset,1);err==nil{
  t.Fatal("expired channel still allowed version read")
 }
 if _,err=svc.WorkspaceLibraryVersions(ctx,p.ID,story.ID,worldAsset);err==nil{
  t.Fatal("expired channel exposed version enumeration")
 }
 assets,err:=svc.WorkspaceLibraryAssets(ctx,p.ID,story.ID,"")
 if err!=nil||len(assets)!=0{
  t.Fatalf("expired grant appeared in scoped Library inventory: %+v %v",assets,err)
 }
 if _,err=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{
  LinkID:worldLink.ID,ActorPrincipalID:"owner",ExpectedRevision:timed.Revision,
  Enabled:true,
 });err==nil{t.Fatal("expired link reactivated without explicit renewal")}
 renewal:=clock.Real{}.UnixMilli()+120000
 restored,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{
  LinkID:worldLink.ID,ActorPrincipalID:"owner",ExpectedRevision:timed.Revision,
  Enabled:true,ExpiresAtMS:&renewal,
 })
 if err!=nil||restored.Expired||restored.ExpiresAtMS==nil||
  *restored.ExpiresAtMS!=renewal{
  t.Fatalf("CAS renewal failed: %+v %v",restored,err)
 }
 if _,err=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{
  LinkID:worldLink.ID,ActorPrincipalID:"owner",ExpectedRevision:timed.Revision,
  Enabled:true,ClearExpiry:true,
 });err==nil{t.Fatal("stale expiry clearance succeeded")}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,worldAsset,1);err!=nil{
  t.Fatalf("renewed channel did not restore the exact original version: %v",err)
 }
 // A live historical link cannot keep serving a source that has been
 // archived. The same boundary also applies if the target is archived.
 for _,archivedWorkspace:=range []string{world.ID,story.ID}{
  if _,err=db.SQL().ExecContext(ctx,
   `UPDATE project_workspaces SET status='archived' WHERE id=?`,
   archivedWorkspace);err!=nil{t.Fatal(err)}
  if _,err=svc.WorkspacePublications(ctx,worldLink.ID);err==nil{
   t.Fatalf("archived link endpoint %s exposed metadata",archivedWorkspace)
  }
  if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,worldAsset,1);err==nil{
   t.Fatalf("archived link endpoint %s still authorised version",archivedWorkspace)
  }
  if _,err=db.SQL().ExecContext(ctx,
   `UPDATE project_workspaces SET status='active' WHERE id=?`,
   archivedWorkspace);err!=nil{t.Fatal(err)}
 }
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,p.ID,story.ID,worldAsset,1);err!=nil{
  t.Fatalf("active endpoints did not restore approved grant: %v",err)
 }

 disabled,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{LinkID:link.ID,ActorPrincipalID:"owner",ExpectedRevision:link.Revision,Enabled:false})
 if err!=nil||disabled.Enabled{t.Fatalf("revoke: %v %+v",err,disabled)}
 if _,err=svc.WorkspacePublications(ctx,link.ID);err==nil{t.Fatal("revoked link exposed publications")}
 if _,err=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{LinkID:link.ID,AssetID:assetID,Version:2,ActorPrincipalID:"owner"});err==nil{t.Fatal("revoked link accepted publication")}
 if _,err=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{LinkID:link.ID,ActorPrincipalID:"owner",ExpectedRevision:link.Revision,Enabled:true});err==nil{t.Fatal("stale revision toggle must be rejected")}
 reenabled,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{LinkID:link.ID,ActorPrincipalID:"owner",ExpectedRevision:disabled.Revision,Enabled:true})
 if err!=nil||!reenabled.Enabled{t.Fatalf("re-enable: %v %+v",err,reenabled)}
}
