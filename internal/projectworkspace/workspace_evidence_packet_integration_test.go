//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestWorkspaceEvidencePacketRequiresExactVersionGrantAndFailsClosed(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/evidence.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Human operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Research project",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 views,err:=svc.WorkspaceViews(ctx,project.ID)
 if err!=nil||len(views)!=1{t.Fatalf("expected default active Workspace: %v",err)}
 world:=views[0]
 research,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Independent Research",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 first,err:=svc.ImportLibraryAsset(ctx,ImportLibraryAssetCommand{
  ProjectID:project.ID,SourceWorkspaceID:world.ID,
  Name:"world.txt",MIMEType:"text/plain",ArtifactID:"secret-artifact-source",
  ContentHash:strings.Repeat("a",64),SizeBytes:1024,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 second,err:=svc.ImportLibraryAsset(ctx,ImportLibraryAssetCommand{
  ProjectID:project.ID,SourceWorkspaceID:world.ID,
  Name:"story.json",MIMEType:"application/json",ArtifactID:"secret-artifact-two",
  ContentHash:strings.Repeat("b",64),SizeBytes:4096,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 refs:=[]WorkspaceEvidenceSelection{{AssetID:second.ID,Version:1},{AssetID:first.ID,Version:1}}
 source,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,world.ID,refs)
 if err!=nil{t.Fatal(err)}
 if source.Schema!="onepane.workspace-evidence-metadata/v1"||
  source.ProjectWorkspaceID!=world.ID||len(source.Entries)!=2||
  !strings.HasPrefix(source.ManifestSHA256,"sha256:")||
  source.ManifestSHA256=="sha256:"||source.SnapshotAtMS<1{
  t.Fatalf("invalid evidence manifest: %+v",source)
 }
 reversed,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,world.ID,
  []WorkspaceEvidenceSelection{refs[1],refs[0]})
 if err!=nil||source.ManifestSHA256!=reversed.ManifestSHA256{
  t.Fatalf("selection order changed canonical evidence digest: %+v %v",reversed,err)
 }
 encoded,_:=json.Marshal(source)
 for _,secret:=range []string{"secret-artifact","artifact:","storage_uri","session_cookie","actual_document_content"}{
  if strings.Contains(string(encoded),secret){t.Fatalf("blob/storage reference disclosed: %s",encoded)}
 }
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,research.ID,refs);
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("ungranted Research Workspace saw private World evidence: %v",err)
 }
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,"other-project",world.ID,refs);
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("cross-Project request was not rejected: %v",err)
 }
 link,err:=svc.CreateWorkspaceLink(ctx,CreateWorkspaceLinkCommand{
  ProjectID:project.ID,SourceWorkspaceID:world.ID,TargetWorkspaceID:research.ID,
  Name:"Review only",ActorPrincipalID:"human",Enable:true,
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,research.ID,
  []WorkspaceEvidenceSelection{{AssetID:first.ID,Version:1}});
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("link without version publication disclosed evidence: %v",err)
 }
 if _,err:=svc.PublishWorkspaceAsset(ctx,PublishWorkspaceAssetCommand{
  LinkID:link.ID,AssetID:first.ID,Version:1,ActorPrincipalID:"human",
 });err!=nil{t.Fatal(err)}
 grant,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,research.ID,
  []WorkspaceEvidenceSelection{{AssetID:first.ID,Version:1}})
 if err!=nil||len(grant.Entries)!=1||grant.Entries[0].ContentHash!=strings.Repeat("a",64){
  t.Fatalf("published immutable version was not selectable: %+v %v",grant,err)
 }
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,research.ID,refs);
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("publication implicitly included ungranted second asset: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_workspace_links
  SET enabled=0 WHERE id=?`,link.ID);err!=nil{t.Fatal(err)}
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,research.ID,
  []WorkspaceEvidenceSelection{{AssetID:first.ID,Version:1}});
  !errors.Is(err,ErrCrossWorkspace){t.Fatalf("revoked link still provided new evidence: %v",err)}
 // Old manifest is merely a receipt, NOT a fresh bearer capability.
 if grant.ManifestSHA256==""{t.Fatal("prior grant should retain its historical hash")}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_library_assets
  SET archived=1 WHERE id=?`,first.ID);err!=nil{t.Fatal(err)}
 if _,err:=svc.BuildWorkspaceEvidencePacket(ctx,project.ID,world.ID,
  []WorkspaceEvidenceSelection{{AssetID:first.ID,Version:1}});
  !errors.Is(err,ErrCrossWorkspace){t.Fatalf("archived asset still selectable: %v",err)}
}

func TestWorkspaceEvidencePacketRejectsMaliciousSelectionsBeforeDatabase(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/evidence-invalid.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{})
 inputs:=[][]WorkspaceEvidenceSelection{
  nil,
  {{AssetID:"x",Version:0}},
  {{AssetID:"x",Version:-1}},
  {{AssetID:" x ",Version:1}},
  {{AssetID:"x",Version:1},{AssetID:"x",Version:1}},
 }
 for i,items:=range inputs{
  if _,err:=svc.BuildWorkspaceEvidencePacket(context.Background(),"project","world",items);
   !errors.Is(err,ErrInvalidCommand){
   t.Fatalf("unsafe evidence selection %d was not rejected: %v",i,err)
  }
 }
 tooMany:=make([]WorkspaceEvidenceSelection,17)
 for i:=range tooMany{tooMany[i]=WorkspaceEvidenceSelection{AssetID:"x",Version:int64(i+1)}}
 if _,err:=svc.BuildWorkspaceEvidencePacket(context.Background(),"project","world",tooMany);
  !errors.Is(err,ErrInvalidCommand){t.Fatalf("unbounded evidence manifest accepted: %v",err)}
}
