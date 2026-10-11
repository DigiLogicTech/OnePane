//go:build integration

package projectorchestrator

import (
 "context"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

func TestTaskStartFailsClosedOnDeletedOrUnattestedApprovedGraphDependencies(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/task-graph-admission.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Actual admission fence",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 var workspaces []string
 for _,name:=range []string{"World","Story","Assets"}{
  ws,e:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
   ProjectID:p.ID,Name:name,ActorPrincipalID:"operator",
  })
  if e!=nil{t.Fatal(e)}
  workspaces=append(workspaces,ws.ID)
 }
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,tasks,nil)
 graph,err:=svc.CreateTaskGraph(ctx,CreateTaskGraphCommand{
  ProjectID:p.ID,Name:"Actual Worker admission",IdempotencyKey:"worker-start-integrity-v1",
  ActorPrincipalID:"operator",Nodes:[]TaskGraphNodeSpec{
   {Key:"story",ProjectWorkspaceID:workspaces[1],Objective:"Write story",DependsOn:[]string{"world"}},
   {Key:"world",ProjectWorkspaceID:workspaces[0],Objective:"Create world"},
   {Key:"assets",ProjectWorkspaceID:workspaces[2],Objective:"Independent assets"},
   {Key:"orphan",ProjectWorkspaceID:workspaces[0],Objective:"Preserve canonical graph membership"},
  },
 })
 if err!=nil{t.Fatal(err)}
 keys:=map[string]string{}
 for _,n:=range graph.Nodes{keys[n.Key]=n.TaskID}
 // A deliberately synthetic fixture marks World complete, isolating
 // dependency admission from independent completion verification.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE tasks SET state='complete' WHERE id=?`,keys["world"]);err!=nil{t.Fatal(err)}
 story,err:=tasks.Get(ctx,keys["story"])
 if err!=nil{t.Fatal(err)}
 story,err=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:story.ID,ExpectedRevision:story.Revision})
 if err!=nil{t.Fatal(err)}
 asset,err:=tasks.Get(ctx,keys["assets"])
 if err!=nil{t.Fatal(err)}
 asset,err=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:asset.ID,ExpectedRevision:asset.Revision})
 if err!=nil{t.Fatal(err)}
 checkStartBlocked:=func(label string){
  t.Helper()
  t.Run(label,func(t *testing.T){
   if _,_,err:=tasks.Start(ctx,task.StartCommand{
    TaskID:story.ID,ExpectedRevision:story.Revision,
   });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
    t.Fatalf("Task started without approved predecessor integrity: %v",err)
   }
   var count int
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,story.ID).Scan(&count);err!=nil{t.Fatal(err)}
   if count!=0{t.Fatalf("blocked Task opened %d Attempts",count)}
  })
 }
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM task_dependencies
  WHERE task_id=? AND depends_on_task_id=?`,story.ID,keys["world"]);err!=nil{t.Fatal(err)}
 checkStartBlocked("deleted_hard_dependency")
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO task_dependencies(
  task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json)
  VALUES(?,?,'hard','all','{}')`,story.ID,keys["world"]);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_recorded=0 WHERE graph_id=? AND node_key='story'`,graph.ID);err!=nil{t.Fatal(err)}
 checkStartBlocked("unattested_legacy_graph")
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_recorded=1,approved_dependencies_json='[]'
  WHERE graph_id=? AND node_key='story'`,graph.ID);err!=nil{t.Fatal(err)}
 checkStartBlocked("approved_manifest_does_not_match_edge")
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_json='["world"]'
  WHERE graph_id=? AND node_key='story'`,graph.ID);err!=nil{t.Fatal(err)}
 started,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:story.ID,ExpectedRevision:story.Revision,
 })
 if err!=nil||started.State!=task.StateRunning||attempt.ID==""{
  t.Fatalf("exactly attested predecessor was not admissible: %+v %+v %v",started,attempt,err)
 }
 // A live original Attempt can enter an automatic dependency wait.
 // Removing an approved predecessor must prohibit BOTH operator Resume and
 // the trusted agent-worker's same-Attempt automatic wake.
 waiting,err:=tasks.WaitDependency(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:started.Revision,
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM task_dependencies
  WHERE task_id=? AND depends_on_task_id=?`,story.ID,keys["world"]);err!=nil{t.Fatal(err)}
 if _,err:=tasks.Resume(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:waiting.Revision,
 });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("operator Resume bypassed deleted approved edge: %v",err)
 }
 err=db.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  return tasks.ResumeWaitingAttemptInTransaction(ctx,tx,task.TransitionCommand{
   TaskID:story.ID,ExpectedRevision:waiting.Revision,
  },attempt.ID)
 })
 if !errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("autonomous same-Attempt wake bypassed deleted edge: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO task_dependencies(
  task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json)
  VALUES(?,?,'hard','all','{}')`,story.ID,keys["world"]);err!=nil{t.Fatal(err)}
 resumed,err:=tasks.Resume(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:waiting.Revision,
 })
 if err!=nil||resumed.State!=task.StateRunning{
  t.Fatalf("restored approved edge prevented legitimate Attempt resume: %+v %v",resumed,err)
 }
 // Verification retry is a third legal RUNNING transition. Guard it too.
 verifyingRequested,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:resumed.Revision,
 })
 if err!=nil{t.Fatal(err)}
 verifying,err:=tasks.BeginVerification(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:verifyingRequested.Revision,
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_json='[]'
  WHERE graph_id=? AND node_key='story'`,graph.ID);err!=nil{t.Fatal(err)}
 if _,err:=tasks.ContinueAfterVerification(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:verifying.Revision,
 });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("verification retry bypassed approved graph integrity: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_json='["world"]'
  WHERE graph_id=? AND node_key='story'`,graph.ID);err!=nil{t.Fatal(err)}
 continued,err:=tasks.ContinueAfterVerification(ctx,task.TransitionCommand{
  TaskID:story.ID,ExpectedRevision:verifying.Revision,
 })
 if err!=nil||continued.State!=task.StateRunning{
  t.Fatalf("verified retry failed after restoring original graph: %+v %v",continued,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET status='archived' WHERE id=?`,workspaces[2]);err!=nil{t.Fatal(err)}
 if _,_,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:asset.ID,ExpectedRevision:asset.Revision,
 });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("archived Workspace allowed graph Task execution: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET status='active' WHERE id=?`,workspaces[2]);err!=nil{t.Fatal(err)}
 if _,_,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:asset.ID,ExpectedRevision:asset.Revision,
 });err!=nil{t.Fatalf("restored independent Workspace could not execute: %v",err)}
 orphan,err:=tasks.Get(ctx,keys["orphan"])
 if err!=nil{t.Fatal(err)}
 orphan,err=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:orphan.ID,ExpectedRevision:orphan.Revision})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM project_orchestrator_task_graph_nodes
  WHERE graph_id=? AND node_key='orphan'`,graph.ID);err!=nil{t.Fatal(err)}
 if _,_,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:orphan.ID,ExpectedRevision:orphan.Revision,
 });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("orphaned graph Task bypassed Task start admission: %v",err)
 }
 standalone,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,
  Objective:"Non-graph Task must keep its established admission path",
 })
 if err!=nil{t.Fatal(err)}
 standalone,err=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:standalone.ID,ExpectedRevision:standalone.Revision})
 if err!=nil{t.Fatal(err)}
 if _,_,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:standalone.ID,ExpectedRevision:standalone.Revision,
 });err!=nil{t.Fatalf("ordinary Task unexpectedly blocked by graph fence: %v",err)}
}
