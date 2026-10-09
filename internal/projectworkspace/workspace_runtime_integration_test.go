//go:build integration

package projectworkspace

import (
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Retain old Project runtime relationships and verify per-Workspace runtime
// identities, lifecycle and tool declarations are independent.
func TestLegacyAndWorkspaceRuntimesCoexistAfterMigration(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/runtime-owners.db");if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 seed:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }
 for _,q:=range seed{if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Game Studio",CreatedBy:"operator"});if err!=nil{t.Fatal(err)}
 old,err:=svc.CreateRuntime(ctx,CreateRuntimeCommand{ProjectID:p.ID,CreatedBy:"operator",DesiredState:RuntimeDesiredStopped});if err!=nil{t.Fatal(err)}
 if old.ProjectWorkspaceID!=nil{t.Fatal("legacy runtime unexpectedly assigned to Workspace")}
 legacyApp,err:=svc.DeclareApplication(ctx,DeclareApplicationCommand{RuntimeID:old.ID,Name:"old-tool",SourceKind:AppOCIImage,SourceRef:"ghcr.io/example/legacy@sha256:abc",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 a,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator"});if err!=nil{t.Fatal(err)}
 b,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator"});if err!=nil{t.Fatal(err)}
 c,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Art",ActorPrincipalID:"operator"});if err!=nil{t.Fatal(err)}
 runtimes:=[]ProjectRuntime{}
 for _,w:=range []WorkspaceView{a,b,c}{
  r,err:=svc.CreateRuntime(ctx,CreateRuntimeCommand{ProjectID:p.ID,ProjectWorkspaceID:&w.ID,CreatedBy:"operator",DesiredState:RuntimeDesiredStopped})
  if err!=nil{t.Fatalf("create runtime for %s: %v",w.Name,err)}
  if r.ProjectWorkspaceID==nil||*r.ProjectWorkspaceID!=w.ID||r.ID==old.ID{t.Fatalf("bad ownership: %+v",r)}
  runtimes=append(runtimes,r)
 }
 gotOld,err:=svc.RuntimeByProject(ctx,p.ID)
 if err!=nil||gotOld.ID!=old.ID{t.Fatalf("legacy runtime altered: %+v %v",gotOld,err)}
 check,err:=svc.RuntimeByProjectWorkspace(ctx,p.ID,b.ID)
 if err!=nil||check.ID!=runtimes[1].ID{t.Fatalf("story runtime mismatch: %+v %v",check,err)}
 if _,err=svc.CreateRuntime(ctx,CreateRuntimeCommand{ProjectID:p.ID,ProjectWorkspaceID:&a.ID,CreatedBy:"operator"});err==nil{t.Fatal("allowed duplicate runtime for same Workspace")}
 foreign:="not-a-workspace"
 if _,err=svc.CreateRuntime(ctx,CreateRuntimeCommand{ProjectID:p.ID,ProjectWorkspaceID:&foreign,CreatedBy:"operator"});err==nil{t.Fatal("allowed unowned Workspace")}
 changed,err:=svc.SetRuntimeDesiredState(ctx,SetRuntimeDesiredStateCommand{
  RuntimeID:runtimes[0].ID,ExpectedRevision:runtimes[0].Revision,
  DesiredState:RuntimeDesiredRunning,ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 if changed.DesiredState!=RuntimeDesiredRunning{t.Fatal("World state did not change")}
 untouched,err:=svc.Runtime(ctx,runtimes[1].ID)
 if err!=nil||untouched.DesiredState!=RuntimeDesiredStopped{t.Fatalf("World changed Story lifecycle: %v %+v",err,untouched)}
 oldAgain,err:=svc.Runtime(ctx,old.ID)
 if err!=nil||oldAgain.DesiredState!=RuntimeDesiredStopped{t.Fatalf("legacy runtime changed: %v %+v",err,oldAgain)}
 apps,err:=svc.ListApplications(ctx,old.ID)
 if err!=nil||len(apps)!=1||apps[0].ID!=legacyApp.ID{t.Fatalf("legacy app linkage lost: %v %+v",err,apps)}
 var fkCheck string
 if err=db.SQL().QueryRowContext(ctx,"PRAGMA quick_check").Scan(&fkCheck);err!=nil||fkCheck!="ok"{t.Fatalf("post migration check: %v %s",err,fkCheck)}
 rows,err:=db.SQL().QueryContext(ctx,"PRAGMA foreign_key_check")
 if err!=nil{t.Fatal(err)}
 defer rows.Close()
 if rows.Next(){t.Fatal("migration introduced foreign key violation")}
 if err=rows.Err();err!=nil{t.Fatal(err)}
}
