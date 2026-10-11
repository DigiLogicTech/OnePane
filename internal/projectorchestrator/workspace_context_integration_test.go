//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestOrchestratorRuntimeContextIsBoundToSelectedWorkspace(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/workspace-context.db")
 if err!=nil {t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil {t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('operator','human','Human','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','operator','active',?,?)`,
 } {
  if _,err:=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil {t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Separated Worlds",CreatedBy:"operator",
 })
 if err!=nil {t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil {t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator",
 })
 if err!=nil {t.Fatal(err)}
 assets,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Assets",ActorPrincipalID:"operator",
 })
 if err!=nil {t.Fatal(err)}
 worldRuntime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&world.ID,CreatedBy:"operator",
 })
 if err!=nil {t.Fatal(err)}
 storyRuntime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&story.ID,CreatedBy:"operator",
 })
 if err!=nil {t.Fatal(err)}
 // Make the wrong sibling runtime more recent than the selected one.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_runtimes SET updated_at=?
   WHERE id=?`,now+60000,storyRuntime.ID);err!=nil {t.Fatal(err)}
 // Workspace AI settings remain private service configuration. The
 // Orchestrator derives execution mode but must not disclose sibling values
 // or even its selected raw settings to a language model.
 secretMarker:="STORY_PRIVATE_CONFIG_MARKER_NEVER_IN_MODEL_CONTEXT"
 settings,_:=json.Marshal(map[string]any{
  "private_model_setting":secretMarker,
  "orchestration":map[string]any{"mode":"team"},
 })
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET ai_settings_json=? WHERE id=?`,string(settings),story.ID);err!=nil{t.Fatal(err)}
 orchestrator:=NewService(db.SQL(),db,clock.Real{},unavailableOrchestratorReasoning{},nil,nil,
  task.NewService(db.SQL(),db,clock.Real{}),nil)
 type snapshot struct {
  Runtime struct {
   ID string `json:"id"`
   Scope string `json:"scope"`
   ProjectWorkspaceID string `json:"project_workspace_id"`
   Status string `json:"status"`
  } `json:"runtime"`
 }
 check:=func(wantID,wantRuntime,wantStatus string){
  t.Helper()
  raw,chosen,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,wantID)
  if err!=nil||chosen!=wantID {t.Fatalf("selection %q: chosen=%q err=%v",wantID,chosen,err)}
  var doc snapshot
  if err:=json.Unmarshal(raw,&doc);err!=nil {t.Fatal(err)}
  if doc.Runtime.ID!=wantRuntime||doc.Runtime.Status!=wantStatus||
   doc.Runtime.Scope!="project_workspace"||doc.Runtime.ProjectWorkspaceID!=wantID{
   t.Fatalf("Workspace %q received another Workspace's runtime: %s",wantID,raw)
  }
 }
 check(world.ID,worldRuntime.ID,"defined")
 check(story.ID,storyRuntime.ID,"defined")
 check(assets.ID,"","not_provisioned")
 for _,selectedID:=range []string{world.ID,story.ID}{
  raw,_,mode,_,err:=orchestrator.contextSnapshot(ctx,p.ID,selectedID)
  if err!=nil{t.Fatal(err)}
  if strings.Contains(string(raw),secretMarker)||strings.Contains(string(raw),"private_model_setting")||
   strings.Contains(string(raw),"\"ai_settings\""){
   t.Fatalf("raw Workspace AI settings leaked to reasoning snapshot: %s",raw)
  }
  if selectedID==story.ID&&mode!="team"{
   t.Fatalf("removing raw settings broke the trusted mode derivation: %s",mode)
  }
 }
 if selected:=selectModelProposedWorkspace(ctx,db.SQL(),p.ID,world.ID,world.ID,story.ID);selected!=world.ID{
  t.Fatalf("model overrode explicitly selected World Workspace with %s",selected)
 }
 if selected:=selectModelProposedWorkspace(ctx,db.SQL(),p.ID,"",world.ID,story.ID);selected!=story.ID{
  t.Fatalf("model could not select valid sibling for unscoped operator request: %s",selected)
 }
 if selected:=selectModelProposedWorkspace(ctx,db.SQL(),p.ID,"",world.ID,"does-not-exist");selected!=world.ID{
  t.Fatalf("model redirected to nonexistent Workspace: %s",selected)
 }
 foreignProject,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Foreign Project",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 foreign,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:foreignProject.ID,Name:"Foreign Workspace",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 if selected:=selectModelProposedWorkspace(ctx,db.SQL(),p.ID,"",world.ID,foreign.ID);selected!=world.ID{
  t.Fatalf("model selected another Project's Workspace: %s",selected)
 }
 if _,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,foreign.ID);!errors.Is(err,ErrInvalid){
  t.Fatalf("foreign Workspace explicit selector incorrectly accepted: %v",err)
 }

 // Invalid, stale, and cross-Project selectors fail closed instead of
 // silently choosing whichever sibling was most recently updated.
 for _,requested:=range []string{"does-not-exist","  does-not-exist  "} {
  if _,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,requested);!errors.Is(err,ErrInvalid){
   t.Fatalf("invalid Workspace %q silently substituted: %v",requested,err)
  }
  if _,err:=orchestrator.Turn(ctx,TurnCommand{
   ProjectID:p.ID,ProjectWorkspaceID:requested,ActorPrincipalID:"operator",
   Objective:"Build in the explicitly selected Workspace",ForceTask:true,
  });!errors.Is(err,ErrInvalid){
   t.Fatalf("Task silently reassigned from invalid Workspace %q: %v",requested,err)
  }
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET status='archived'
  WHERE id=?`,assets.ID);err!=nil {t.Fatal(err)}
 if _,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,assets.ID);!errors.Is(err,ErrInvalid){
  t.Fatalf("archived Workspace silently rerouted: %v",err)
 }
 if selected:=selectModelProposedWorkspace(ctx,db.SQL(),p.ID,"",world.ID,assets.ID);selected!=world.ID{
  t.Fatalf("model redirected to archived Workspace: %s",selected)
 }
 var count int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_turns`).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatalf("rejected Workspace caused side effects: %d recorded turns",count)}
}
