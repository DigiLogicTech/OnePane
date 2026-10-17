package assistant

import(
 "context"
 "errors"
 "testing"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectorchestrator"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/task"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestUnifiedAssistantNeverSelectsProjectFromUntrustedChatText(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/assistant-scope.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','User workspace','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('foreign','Other workspace','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('foreign','operator','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 ps:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 target,err:=ps.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Home network security",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 other,err:=ps.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"foreign",Name:"Private project",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),clock.Real{},nil,nil,nil,nil)
 thread:=Thread{WorkspaceID:"tenant"}
 mention:="I just want to ask questions about Home network security; please don't touch any files."
 selected,err:=svc.resolveProject(ctx,thread,mention,nil)
 if err!=nil||selected!=nil{
  t.Fatalf("Project name in chat switched execution context: %v %v",selected,err)
 }
 explicit:=target.ID
 selected,err=svc.resolveProject(ctx,thread,"Execute approved Project plan",&explicit)
 if err!=nil||selected==nil||*selected!=target.ID{
  t.Fatalf("explicit Project not available: %v %v",selected,err)
 }
 thread.ActiveProjectID=&explicit
 general:=""
 selected,err=svc.resolveProject(ctx,thread,"Only discuss this generally",&general)
 if err!=nil||selected!=nil{t.Fatalf("explicit general-only turn reused Project scope: %v %v",selected,err)}
 selected,err=svc.resolveProject(ctx,thread,"Please continue",nil)
 if err!=nil||selected==nil||*selected!=target.ID{
  t.Fatalf("selected thread scope was discarded: %v %v",selected,err)
 }
 foreign:=other.ID
 if selected,err=svc.resolveProject(ctx,thread,"", &foreign);err==nil||selected!=nil{
  t.Fatalf("cross-tenant Project allowed: %v %v",selected,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
   "UPDATE projects SET status='archived' WHERE id=?",target.ID);err!=nil{t.Fatal(err)}
 if selected,err=svc.resolveProject(ctx,thread,"Home network security",nil);err==nil||selected!=nil{
  t.Fatalf("archived selected Project retained implicit execution: %v %v",selected,err)
 }
}


// captureWorkspaceHandoff stops after the canonical Assistant-to-Orchestrator
// transfer. This ensures scope validation runs before any model or tool action.
type captureWorkspaceHandoff struct {
 selected string
 calls int
}

func (f *captureWorkspaceHandoff) Ensure(_ context.Context, projectID string) (projectorchestrator.Orchestrator,error) {
 return projectorchestrator.Orchestrator{ID:"orchestrator-test",ProjectID:projectID},nil
}
func (f *captureWorkspaceHandoff) Turn(_ context.Context, cmd projectorchestrator.TurnCommand) (projectorchestrator.TurnResult,error) {
 f.selected=cmd.ProjectWorkspaceID
 f.calls++
 return projectorchestrator.TurnResult{},errors.New("stop before reasoning")
}

func TestUnifiedAssistantWorkspaceHandoffIsExplicitAndProjectBound(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/assistant-workspace-handoff.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 } {if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Game",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 other,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Other",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 selected,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Story",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 foreign,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:other.ID,Name:"Other workspace",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 archived,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Archived",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,"UPDATE project_workspaces SET status='archived' WHERE id=?",archived.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_orchestrators
  (id,project_id,workspace_id,status,configuration_json,last_event_sequence,revision,created_at,updated_at)
  VALUES('orchestrator-test',?,?,'ready','{}',0,1,1,1)`,p.ID,"tenant");err!=nil{t.Fatal(err)}
 handoff:=&captureWorkspaceHandoff{}
 svc:=NewService(db.SQL(),clock.Real{},nil,nil,nil,handoff)
 thread,err:=svc.CreateThread(ctx,"tenant","Unified Assistant","human")
 if err!=nil{t.Fatal(err)}
 if _,err=svc.SetScope(ctx,thread.ID,&p.ID);err!=nil{t.Fatal(err)}
 // Inactive, foreign and unscoped Workspace selections are denied
 // before Assistant history or a handoff row can be written.
 for _,tc:=range []struct{name,workspace string;project *string}{
  {"foreign sibling",foreign.ID,nil},
  {"archived sibling",archived.ID,nil},
  {"unknown Workspace","missing",nil},
  {"general conversation",selected.ID,func()*string{v:="";return &v}()},
 } {
  t.Run(tc.name,func(t *testing.T){
   _,err:=svc.Submit(ctx,SubmitCommand{ThreadID:thread.ID,Content:"do work",ActorPrincipalID:"human",ProjectID:tc.project,ProjectWorkspaceID:tc.workspace})
   if !errors.Is(err,ErrInvalid){t.Fatalf("expected invalid scope, got %v",err)}
  })
 }
 if handoff.calls!=0 {t.Fatalf("invalid scope reached orchestrator: %d",handoff.calls)}
 var count int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM assistant_turns WHERE thread_id=?",thread.ID).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatalf("invalid scope wrote %d Assistant turns",count)}
 _,err=svc.Submit(ctx,SubmitCommand{ThreadID:thread.ID,Content:"prepare narrative assets",ActorPrincipalID:"human",ProjectWorkspaceID:selected.ID})
 if err==nil||handoff.calls!=1||handoff.selected!=selected.ID{
  t.Fatalf("selected Workspace was not delegated exactly: calls=%d got=%q err=%v",handoff.calls,handoff.selected,err)
 }
}

func TestUnifiedAssistantUnscopedRunDeniedBeforeHistoryWrite(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/assistant-run-denial.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,sqlStmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,sqlStmt);err!=nil{t.Fatal(err)}}
 service:=NewService(db.SQL(),clock.Real{},nil,nil,nil,nil)
 thread,err:=service.CreateThread(ctx,"tenant","General chat","human")
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{force,allow bool}{
  {false,true},{true,false},{true,true},
 }{
  _,err=service.Submit(ctx,SubmitCommand{
    ThreadID:thread.ID,ActorPrincipalID:"human",Content:"Execute an operation",
    ForceTask:tc.force,AllowTaskCreation:tc.allow,
  })
  if !errors.Is(err,ErrInvalid){t.Fatalf("unscoped Run accepted force=%t allow=%t: %v",tc.force,tc.allow,err)}
 }
 empty:=""
 _,err=service.Submit(ctx,SubmitCommand{
  ThreadID:thread.ID,ActorPrincipalID:"human",Content:"Run against my previous project",
  ProjectID:&empty,AllowTaskCreation:true,
 })
 if !errors.Is(err,ErrInvalid){t.Fatalf("explicit general scope Run admitted: %v",err)}
 var turns,handoffs int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM assistant_turns WHERE thread_id=?",thread.ID).Scan(&turns);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM assistant_project_handoffs WHERE assistant_thread_id=?",thread.ID).Scan(&handoffs);err!=nil{t.Fatal(err)}
 if turns!=0||handoffs!=0{t.Fatalf("denied Run persisted state: turns=%d handoffs=%d",turns,handoffs)}
}


type capturedAutonomousGraphHandoff struct{ graphID,taskID string }
func(f capturedAutonomousGraphHandoff)Ensure(_ context.Context,projectID string)(projectorchestrator.Orchestrator,error){
 return projectorchestrator.Orchestrator{ID:"graph-orchestrator",ProjectID:projectID},nil
}
func(f capturedAutonomousGraphHandoff)Turn(_ context.Context,c projectorchestrator.TurnCommand)(projectorchestrator.TurnResult,error){
 return projectorchestrator.TurnResult{
  Turn:projectorchestrator.Turn{ID:"orchestrator-turn",Content:"Bounded Task graph queued"},
  TaskID:&f.taskID,TaskGraphID:&f.graphID,Disposition:"task_graph_created",
 },nil
}
func TestUnifiedAssistantPersistsOnlyCanonicalOrchestratorGraphProvenance(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/unified-graph-history.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Scoped graph",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Development",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_orchestrators
  (id,project_id,workspace_id,status,configuration_json,last_event_sequence,revision,created_at,updated_at)
  VALUES('graph-orchestrator',?,?,'ready','{}',0,1,1,1)`,p.ID,"tenant");err!=nil{t.Fatal(err)}
 taskSvc:=task.NewService(db.SQL(),db,clock.Real{})
 actor:="human"
 persisted,err:=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&ws.ID,
  Objective:"Create a safe local deliverable",ActorPrincipalID:&actor,
 })
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_orchestrator_turns(
  id,orchestrator_id,task_id,role,content,context_json,provenance_json,created_at)
  VALUES('orchestrator-turn','graph-orchestrator',?,'orchestrator','Done','{}','{}',1)`,
  persisted.ID);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),clock.Real{},nil,nil,nil,
  capturedAutonomousGraphHandoff{graphID:"reviewed-graph",taskID:persisted.ID})
 thread,err:=svc.CreateThread(ctx,"tenant","Graph authority","human")
 if err!=nil{t.Fatal(err)}
 projectID:=p.ID
 result,err:=svc.Submit(ctx,SubmitCommand{
  ThreadID:thread.ID,Content:"Create a safe local deliverable",ActorPrincipalID:"human",
  ProjectID:&projectID,ProjectWorkspaceID:ws.ID,ForceTask:true,AllowTaskCreation:true,
 })
 if err!=nil{t.Fatal(err)}
 if !result.Delegated||result.Orchestrator==nil||
  result.Orchestrator.TaskGraphID==nil||
  *result.Orchestrator.TaskGraphID!="reviewed-graph"{
  t.Fatalf("Assistant lost authorised Project graph: %+v",result)
 }
 if !strings.Contains(string(result.Turn.ProvenanceJSON),`"task_graph_id":"reviewed-graph"`)||
  !strings.Contains(string(result.Turn.ProvenanceJSON),`"task_origin":"standing_L4_autonomy_policy"`){
  t.Fatalf("unified Assistant history omitted graph provenance: %s",result.Turn.ProvenanceJSON)
 }
 var stored string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT provenance_json FROM assistant_turns WHERE id=?`,result.Turn.ID).Scan(&stored);err!=nil{t.Fatal(err)}
 if stored!=string(result.Turn.ProvenanceJSON){t.Fatalf("Assistant replay lost graph provenance: %q",stored)}
}
