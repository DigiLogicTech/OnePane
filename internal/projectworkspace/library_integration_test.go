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
 disabled,err:=svc.SetWorkspaceLinkEnabled(ctx,ToggleWorkspaceLinkCommand{LinkID:link.ID,ExpectedRevision:link.Revision,ActorPrincipalID:"operator",Enabled:false})
 if err!=nil||disabled.Enabled{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("link revocation did not revoke publication-based access")}
 if err=svc.GrantLibraryAsset(ctx,GrantLibraryAssetCommand{ProjectID:project.ID,AssetID:lib.ID,WorkspaceID:target.ID,VersionPolicy:"pinned",PinnedVersion:1,ActorPrincipalID:"operator"});err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err!=nil{t.Fatalf("direct pinned grant not honoured: %v",err)}
 if err=svc.RevokeLibraryAsset(ctx,project.ID,lib.ID,target.ID,"operator");err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceLibraryVersion(ctx,project.ID,target.ID,lib.ID,1);err==nil{t.Fatal("direct grant revocation did not remove access")}
 // Retention: revoking access never deletes the Project artifact itself.
 if err=artifactSvc.VerifyContent(ctx,raw.ID);err!=nil{t.Fatalf("revoking a link damaged immutable content: %v",err)}
}
