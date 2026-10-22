//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "sync"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)
func TestNewProjectStarterWorkspaceIsAtomicUnprivilegedAndHumanAuthorised(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/new-project-starter.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 policy:=json.RawMessage(`{"onepane_operations":{"version":1,"mode":"autonomous_project"},
 "feature_policy":{"AutonomousRoutines":"enabled"}}`)
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"A new development Project",
  CreatedBy:"operator",ProjectPolicyJSON:policy,CreateStarterWorkspace:true,
 })
 if err!=nil{t.Fatal(err)}
 views,err:=svc.WorkspaceViews(ctx,p.ID)
 if err!=nil{t.Fatal(err)}
 if len(views)!=1||views[0].ProjectID!=p.ID||
  views[0].Name!="Development"||views[0].Status!="active"{
  t.Fatalf("Project starter Workspace not persisted atomically: %+v",views)
 }
 if string(views[0].AISettingsJSON)==""||string(views[0].ResourceScopeJSON)!="{}"{
  t.Fatalf("starter Workspace requested hidden privileges: %+v",views[0])
 }
 var events,systemRuntimes int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE workspace_id='tenant' AND aggregate_type IN('project','project_workspace')
  AND aggregate_id IN(?,?)`,p.ID,views[0].ID).Scan(&events);err!=nil||events!=2{
  t.Fatalf("Project and Workspace creation not independently audited: %d %v",events,err)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_runtimes WHERE project_id=?`,p.ID).Scan(&systemRuntimes);err!=nil||systemRuntimes!=0{
  t.Fatalf("starter Workspace unexpectedly provisioned runtime: %d %v",systemRuntimes,err)
 }
 legacy,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"API without a starter",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 // Compatibility clients intentionally receive a synthetic "Main"
 // view even with no persisted canonical Project Workspace. Assert the
 // database record count rather than mistaking that virtual UI adapter for
 // a new user-authorised Workspace.
 // Legacy WorkspaceViews intentionally materialises "Main" during its
 // first read. The explicit starter flag must not have inserted that row
 // during Project creation; the pre-read DB check proves opt-in behaviour.
 var persistedLegacy int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_workspaces WHERE project_id=?",legacy.ID).Scan(&persistedLegacy);
  err!=nil||persistedLegacy!=0{
  t.Fatalf("legacy Project was changed at creation: %d %v",persistedLegacy,err)
 }
 legacyViews,err:=svc.WorkspaceViews(ctx,legacy.ID)
 if err!=nil||len(legacyViews)!=1||legacyViews[0].Name!="Main"{
  t.Fatalf("existing legacy lazy-Main behaviour changed: %+v %v",legacyViews,err)
 }
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_workspaces WHERE project_id=?",legacy.ID).Scan(&persistedLegacy);
  err!=nil||persistedLegacy!=1{
  t.Fatalf("legacy first-read migration was not preserved: %d %v",persistedLegacy,err)
 }
 if _,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Robot starter",CreatedBy:"robot",
  CreateStarterWorkspace:true,
 });err==nil{t.Fatal("Agent minted Project and Workspace without human approval")}
 // Concurrent old clients must not mint multiple compatibility Main
 // Workspaces for a Project without an explicit starter selection.
 db.SQL().SetMaxOpenConns(1)
 legacyConcurrent,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Concurrent legacy",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 const parallel=8
 var wg sync.WaitGroup
 resultErrors:=make(chan error,parallel)
 start:=make(chan struct{})
 for i:=0;i<parallel;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   <-start
   views,err:=svc.WorkspaceViews(ctx,legacyConcurrent.ID)
   if err==nil&&(len(views)!=1||views[0].Name!="Main"){
    err=ErrInvalidCommand
   }
   resultErrors<-err
  }()
 }
 close(start)
 wg.Wait();close(resultErrors)
 for err:=range resultErrors{if err!=nil{t.Fatalf("concurrent legacy migration failed: %v",err)}}
 var mains int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_workspaces WHERE project_id=?",legacyConcurrent.ID).Scan(&mains);err!=nil||mains!=1{
  t.Fatalf("concurrent legacy reads duplicated Main: count=%d err=%v",mains,err)
 }
 archived,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Archived unmigrated",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE projects SET status='archived' WHERE id=?",archived.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.WorkspaceViews(ctx,archived.ID);err==nil{
  t.Fatal("read of archived Project materialised new Workspace")
 }
 var archivedMains int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_workspaces WHERE project_id=?",archived.ID).Scan(&archivedMains);err!=nil||archivedMains!=0{
  t.Fatalf("archived Project mutated on read: count=%d err=%v",archivedMains,err)
 }
 var robotCount int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM projects
  WHERE name='Robot starter'`).Scan(&robotCount);err!=nil||robotCount!=0{
  t.Fatalf("failed Agent Project left partial persisted objects: %d %v",robotCount,err)
 }
}
