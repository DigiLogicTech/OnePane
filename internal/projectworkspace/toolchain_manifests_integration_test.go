//go:build integration

package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func toolchainManifestTestService(t *testing.T)(*Service,*sqlitestore.DB,string,string,string,string){
 t.Helper()
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/manifest.db")
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){db.Close()})
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Worker','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','agent','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 s:=NewService(db.SQL(),db,clock.Real{})
 p,err:=s.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Software Studio",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 world,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Build",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 story,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"QA",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 for _,v:=range []WorkspaceView{world,story}{
  r,err:=s.CreateRuntime(ctx,CreateRuntimeCommand{ProjectID:p.ID,ProjectWorkspaceID:&v.ID,CreatedBy:"human"})
  if err!=nil{t.Fatal(err)}
  image:="registry.example/go@sha256:"+strings.Repeat("a",64)
  app,err:=s.DeclareApplication(ctx,DeclareApplicationCommand{RuntimeID:r.ID,Name:"Go Toolchain",
   SourceKind:AppOCIImage,SourceRef:image,CreatedBy:"human"})
  if err!=nil{t.Fatal(err)}
  if v.ID==world.ID{return s,db,p.ID,world.ID,story.ID,app.ID}
 }
 t.Fatal("missing owner")
 return nil,nil,"","","",""
}

func TestHumanApprovedWorkspaceToolchainManifestIsImmutableAndScoped(t *testing.T){
 ctx:=context.Background()
 s,db,projectID,worldID,storyID,appID:=toolchainManifestTestService(t)
 req:=[]ToolchainRequirement{{Executable:"python3",VersionConstraint:">=3.12"},{Executable:"go",VersionConstraint:"1.23.*"}}
 cmd:=ApproveToolchainManifestCommand{ProjectID:projectID,ProjectWorkspaceID:worldID,
  ApplicationID:appID,ExpectedRevision:0,Requirements:req,ActorPrincipalID:"human"}
 a,err:=s.ApproveWorkspaceToolchainManifest(ctx,cmd)
 if err!=nil{t.Fatalf("approved toolchain: %v",err)}
 if a.Revision!=1||a.Status!="approved_unverified"||!a.CurrentApplicationMatches||
  len(a.ManifestSHA256)!=64||a.ApprovedBy!="human"||
  a.Requirements[0].Executable!="go"||a.Requirements[1].Executable!="python3"{
  t.Fatalf("invalid approved baseline: %+v",a)
 }
 current,err:=s.WorkspaceToolchainManifest(ctx,projectID,worldID)
 if err!=nil||current.ManifestSHA256!=a.ManifestSHA256{t.Fatalf("approval was not durable: %+v %v",current,err)}
 if _,err=s.WorkspaceToolchainManifest(ctx,projectID,storyID);!errors.Is(err,sql.ErrNoRows){
  t.Fatalf("QA Workspace inherited ungranted software profile: %v",err)
 }
 if _,err=s.WorkspaceToolchainManifest(ctx,"forged-project",worldID);!errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("cross-project manifest read leaked: %v",err)
 }
 if _,err=s.ApproveWorkspaceToolchainManifest(ctx,ApproveToolchainManifestCommand{
  ProjectID:projectID,ProjectWorkspaceID:storyID,ApplicationID:appID,
  ActorPrincipalID:"human",Requirements:req});!errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("cross-Workspace executable approval accepted: %v",err)
 }
 cmd.ActorPrincipalID="agent";cmd.ExpectedRevision=1
 if _,err=s.ApproveWorkspaceToolchainManifest(ctx,cmd);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("autonomous Agent forged human toolchain approval: %v",err)
 }
 cmd.ActorPrincipalID="human";cmd.ExpectedRevision=0
 if _,err=s.ApproveWorkspaceToolchainManifest(ctx,cmd);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale approval revision succeeded: %v",err)
 }
 cmd.ExpectedRevision=1
 cmd.Requirements=[]ToolchainRequirement{{Executable:"rustc",VersionConstraint:"1.85.*"}}
 b,err:=s.ApproveWorkspaceToolchainManifest(ctx,cmd)
 if err!=nil||b.Revision!=2||b.ManifestSHA256==a.ManifestSHA256{
  t.Fatalf("second approved manifest revision failed: %+v %v",b,err)
 }
 // Both revisions remain immutable, historical and attached to this Workspace.
 var count int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspace_toolchain_manifests
 WHERE project_workspace_id=?`,worldID).Scan(&count);err!=nil||count!=2{
  t.Fatalf("approval history overwritten: %d %v",count,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspace_toolchain_manifests SET manifest_sha256=? WHERE project_workspace_id=? AND revision=1`,strings.Repeat("0",64),worldID);err==nil{
  t.Fatal("historical human approval illegally modified")
 }
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM project_workspace_toolchain_manifests WHERE project_workspace_id=?`,worldID);err==nil{
  t.Fatal("immutable approved manifest history deleted")
 }
 // A stale underlying application revision must invalidate the manifest,
 // not silently grant the newly installed image its predecessor's approval.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_applications SET revision=revision+1 WHERE id=?`,appID);err!=nil{t.Fatal(err)}
 stale,err:=s.WorkspaceToolchainManifest(ctx,projectID,worldID)
 if err!=nil||stale.CurrentApplicationMatches||stale.Status!="stale_application_changed"{
  t.Fatalf("application drift falsely accepted as approved: %+v %v",stale,err)
 }
 var payload string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT payload_json FROM events
 WHERE event_type='project_workspace.toolchain_manifest_approved'
 ORDER BY occurred_at DESC LIMIT 1`).Scan(&payload);err!=nil{
  t.Fatalf("approval provenance not recorded: %v",err)
 }
 if !json.Valid([]byte(payload))||strings.Contains(payload,"VersionConstraint"){
  t.Fatalf("approval event leaked unexpected raw data: %s",payload)
 }
}

func TestToolchainManifestValidatesVersionConstraintNamesAndImageBinding(t *testing.T){
 ctx:=context.Background()
 s,_,pid,wid,_,appID:=toolchainManifestTestService(t)
 cmd:=ApproveToolchainManifestCommand{ProjectID:pid,ProjectWorkspaceID:wid,
  ApplicationID:appID,ActorPrincipalID:"human",Requirements:[]ToolchainRequirement{{Executable:"made-up-tool",VersionConstraint:">=1.0"}}}
 if _,err:=s.ApproveWorkspaceToolchainManifest(ctx,cmd);err!=nil{t.Fatalf("arbitrary software names incorrectly forbidden: %v",err)}
 for _,invalid:=range [][]ToolchainRequirement{
  {},{{Executable:"python3",VersionConstraint:"1.2; echo secret"}},
  {{Executable:"../../host"}},
  {{Executable:"go"},{Executable:"go"}},
  {{Executable:"."}},{{Executable:"sh -c"}},
  {{Executable:"python3",VersionConstraint:strings.Repeat("A",65)}},
 }{
  cmd.ExpectedRevision=1;cmd.Requirements=invalid
  if _,err:=s.ApproveWorkspaceToolchainManifest(ctx,cmd);!errors.Is(err,ErrInvalidCommand){
   t.Fatalf("malformed requirements accepted: %+v %v",invalid,err)
  }
 }
}
