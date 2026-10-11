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
 // Operator Project policy is not an evidence attachment. Raw private
 // policy and opaque credential references must never reach a reasoning
 // prompt, its persisted context manifest, or Project turn history.
 projectPolicyCanary:="PROJECT_PRIVATE_POLICY_NEVER_IN_MODEL_CONTEXT"
 policyRaw,_:=json.Marshal(map[string]any{
  "secret_connector_reference":projectPolicyCanary,
  "routing":map[string]any{"local_first":true,"private_notes":projectPolicyCanary},
 })
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET project_policy_json=?
  WHERE id=?`,string(policyRaw),p.ID);err!=nil{t.Fatal(err)}
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
  "orchestration":map[string]any{"mode":"team","team":map[string]any{"team_id":"story-team"}},
 })
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET ai_settings_json=? WHERE id=?`,string(settings),story.ID);err!=nil{t.Fatal(err)}
 worldSettings,_:=json.Marshal(map[string]any{
  "orchestration":map[string]any{"mode":"council","council":map[string]any{"team_id":"world-council"}},
 })
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET ai_settings_json=? WHERE id=?`,string(worldSettings),world.ID);err!=nil{t.Fatal(err)}
 // A Project-level orchestration model can coordinate known Workspace
 // names, but it cannot inspect another Workspace's unpublished objectives,
 // Task IDs or recent Task event identities without an explicit grant.
 taskSvc:=task.NewService(db.SQL(),db,clock.Real{})
 worldTask,err:=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&world.ID,
  Objective:"WORLD_PRIVATE_OBJECTIVE_NEVER_SHARED_WITH_STORY",
 })
 if err!=nil{t.Fatal(err)}
 longObjective:=strings.Repeat("🧪",257)+"PRIVATE_FULL_OBJECTIVE_TAIL"
 longTask,err:=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&world.ID,
  Objective:longObjective,
 })
 if err!=nil{t.Fatal(err)}
 fullStoredTask,err:=taskSvc.Get(ctx,longTask.ID)
 if err!=nil||fullStoredTask.Objective!=longObjective{
  t.Fatalf("Task persistence truncated the real objective: %v",err)
 }
 storyTask,err:=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&story.ID,
  Objective:"STORY_PRIVATE_OBJECTIVE_NEVER_SHARED_WITH_WORLD",
 })
 if err!=nil{t.Fatal(err)}
 legacyTask,err:=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,
  Objective:"LEGACY_SHARED_TASK_NOT_AUTOMATICALLY_WORKSPACE_EVIDENCE",
 })
 if err!=nil{t.Fatal(err)}
 orchestrator:=NewService(db.SQL(),db,clock.Real{},unavailableOrchestratorReasoning{},nil,nil,
  taskSvc,nil)
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
 worldSnapshot,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,world.ID)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(worldSnapshot),"\"objective_truncated\":true")||
  !strings.Contains(string(worldSnapshot),strings.Repeat("🧪",256))||
  strings.Contains(string(worldSnapshot),"PRIVATE_FULL_OBJECTIVE_TAIL"){
  t.Fatalf("Orchestrator context leaked full oversized Task objective or hid truncation status: %s",worldSnapshot)
 }
 storySnapshot,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,story.ID)
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name string;raw json.RawMessage;allowed,denied,deniedID string}{
  {"world",worldSnapshot,worldTask.Objective,storyTask.Objective,storyTask.ID},
  {"story",storySnapshot,storyTask.Objective,worldTask.Objective,worldTask.ID},
 }{
  if !strings.Contains(string(tc.raw),tc.allowed)||
   strings.Contains(string(tc.raw),tc.denied)||
   strings.Contains(string(tc.raw),tc.deniedID)||
   strings.Contains(string(tc.raw),legacyTask.Objective)||
   strings.Contains(string(tc.raw),legacyTask.ID){
   t.Fatalf("%s Workspace context disclosed private or unscoped Tasks: %s",tc.name,tc.raw)
  }
 }
 assetSnapshot,_,_,_,err:=orchestrator.contextSnapshot(ctx,p.ID,assets.ID)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(assetSnapshot),worldTask.Objective)||
  strings.Contains(string(assetSnapshot),storyTask.Objective)||
  strings.Contains(string(assetSnapshot),worldTask.ID)||
  strings.Contains(string(assetSnapshot),storyTask.ID){
  t.Fatalf("unprovisioned Assets Workspace received peer Task evidence: %s",assetSnapshot)
 }
 for _,selectedID:=range []string{world.ID,story.ID}{
  raw,_,mode,_,err:=orchestrator.contextSnapshot(ctx,p.ID,selectedID)
  if err!=nil{t.Fatal(err)}
  if strings.Contains(string(raw),projectPolicyCanary)||
   strings.Contains(string(raw),"secret_connector_reference")||
   strings.Contains(string(raw),"private_notes")||
   strings.Contains(string(raw),"\"policy\""){
   t.Fatalf("raw operator Project policy leaked to reasoning snapshot: %s",raw)
  }
  if strings.Contains(string(raw),secretMarker)||strings.Contains(string(raw),"private_model_setting")||
   strings.Contains(string(raw),"\"ai_settings\""){
   t.Fatalf("raw Workspace AI settings leaked to reasoning snapshot: %s",raw)
  }
  if selectedID==story.ID&&mode!="team"{
   t.Fatalf("removing raw settings broke the trusted mode derivation: %s",mode)
  }
 }
 // Final Team and mode must come from the chosen Workspace, never an
 // earlier sibling or a model-generated "mode" response.
 pinned,mode,teamID,err:=resolveOrchestratorWorkspaceProfile(ctx,db.SQL(),p.ID,world.ID,world.ID,story.ID)
 if err!=nil||pinned!=world.ID||mode!="council"||teamID!="world-council"{
  t.Fatalf("operator-pinned World Council profile changed: %s %s %s %v",pinned,mode,teamID,err)
 }
 suggested,mode,teamID,err:=resolveOrchestratorWorkspaceProfile(ctx,db.SQL(),p.ID,"",world.ID,story.ID)
 if err!=nil||suggested!=story.ID||mode!="team"||teamID!="story-team"{
  t.Fatalf("model-selected Story inherited World Council: %s %s %s %v",suggested,mode,teamID,err)
 }
 unchanged,mode,teamID,err:=resolveOrchestratorWorkspaceProfile(ctx,db.SQL(),p.ID,"",world.ID,"invalid")
 if err!=nil||unchanged!=world.ID||mode!="council"||teamID!="world-council"{
  t.Fatalf("invalid model suggestion changed selected execution profile: %s %s %s %v",unchanged,mode,teamID,err)
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
 if selected,_,_,err:=resolveOrchestratorWorkspaceProfile(ctx,db.SQL(),p.ID,foreign.ID,world.ID,world.ID);!errors.Is(err,ErrInvalid)||selected!=""{
  t.Fatalf("foreign Workspace inherited local Team configuration: %s %v",selected,err)
 }
 disabled,_:=json.Marshal(map[string]any{
  "orchestration":map[string]any{"mode":"team","team":map[string]any{"team_id":"story-team"}},
  "routing":map[string]any{"enabled":false},
 })
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=?
  WHERE id=?`,string(disabled),story.ID);err!=nil{t.Fatal(err)}
 selected,mode,teamID,err:=resolveOrchestratorWorkspaceProfile(ctx,db.SQL(),p.ID,"",world.ID,story.ID)
 if err!=nil||selected!=story.ID||mode!="direct"||teamID!=""{
  t.Fatalf("disabled Workspace routing started a Team anyway: %s %s %s %v",selected,mode,teamID,err)
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
