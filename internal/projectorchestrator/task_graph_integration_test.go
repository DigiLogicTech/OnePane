//go:build integration

package projectorchestrator

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestProjectTaskGraphAtomicDependencyAndIdempotency(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/orchestrator-graph.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Human','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','agent','Agent','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','agent','active',?,?)`,
 }{if _,err=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Local-first creation",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 source,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Assets",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 integration,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Integration",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,
  task.NewService(db.SQL(),db,clock.Real{}),nil)
 plan:=CreateTaskGraphCommand{
  ProjectID:p.ID,Name:"Create a local project",
  ActorPrincipalID:"operator",IdempotencyKey:"user-approved-plan-v1",
  // Deliberately unordered: validate independent DAG topology, not the
  // caller's choice of array order.
  Nodes:[]TaskGraphNodeSpec{
   {Key:"integration",ProjectWorkspaceID:integration.ID,
    Objective:"Validate assembled local artefacts",DependsOn:[]string{"story","assets"}},
   {Key:"story",ProjectWorkspaceID:story.ID,
    Objective:"Write the local script",DependsOn:[]string{"assets"}},
   {Key:"assets",ProjectWorkspaceID:source.ID,
    Objective:"Build an isolated local asset"},
  },
 }
 graph,err:=svc.CreateTaskGraph(ctx,plan)
 if err!=nil{t.Fatal(err)}
 if graph.ID==""||len(graph.Nodes)!=3||graph.ManifestSHA256==""{
  t.Fatalf("Project graph not persisted: %+v",graph)
 }
 lookup:=map[string]TaskGraphNode{}
 for _,n:=range graph.Nodes{lookup[n.Key]=n}
 if lookup["assets"].ProjectWorkspaceID!=source.ID||
  lookup["story"].ProjectWorkspaceID!=story.ID||
  lookup["integration"].ProjectWorkspaceID!=integration.ID||
  len(lookup["integration"].DependsOn)!=2{
  t.Fatalf("Graph allowed Workspace/task dependency drift: %+v",lookup)
 }
 for _,key:=range []string{"assets","story","integration"}{
  persisted,err:=task.NewService(db.SQL(),db,clock.Real{}).Get(ctx,lookup[key].TaskID)
  if err!=nil||persisted.State!=task.StateCreated||persisted.ProjectID==nil||
   *persisted.ProjectID!=p.ID||persisted.ProjectWorkspaceID==nil||
   *persisted.ProjectWorkspaceID!=lookup[key].ProjectWorkspaceID{
   t.Fatalf("Task scope drift: %+v %v",persisted,err)
  }
  var completion struct{Routing struct{
   Access struct{RemoteModels bool `json:"remote_models"`;Internet bool `json:"internet"`;Secrets string `json:"secrets"`} `json:"workspace_access"`
  } `json:"onepane_routing"`}
  if json.Unmarshal(persisted.Completion,&completion)!=nil||
   completion.Routing.Access.RemoteModels||completion.Routing.Access.Internet||
   completion.Routing.Access.Secrets!="none"{
   t.Fatalf("Graph unexpectedly enabled remote models, Internet or Vault: %s",persisted.Completion)
  }
 }
 retried,err:=svc.CreateTaskGraph(ctx,plan)
 if err!=nil||retried.ID!=graph.ID{
  t.Fatalf("retry duplicated Task graph: %+v %v",retried,err)
 }
 var taskCount,edgeCount int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_task_graph_nodes
  WHERE graph_id=?`,graph.ID).Scan(&taskCount);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_dependencies
  WHERE task_id IN (SELECT task_id FROM project_orchestrator_task_graph_nodes
   WHERE graph_id=?)`,graph.ID).Scan(&edgeCount);err!=nil{t.Fatal(err)}
 if taskCount!=3||edgeCount!=3{
  t.Fatalf("graph retry duplicated nodes/edges: %d %d",taskCount,edgeCount)
 }
 changed:=plan
 changed.Nodes=append([]TaskGraphNodeSpec(nil),plan.Nodes...)
 changed.Nodes[0].Objective="Different user-approved objective"
 if _,err=svc.CreateTaskGraph(ctx,changed);!errors.Is(err,ErrGraphConflict){
  t.Fatalf("idempotency key reused with changed scope: %v",err)
 }
 taskSvc:=task.NewService(db.SQL(),db,clock.Real{})
 // Even when an independent caller marks a successor READY, Task.Start
 // rechecks its hard DAG dependency in the same write transaction.
 successor,err:=taskSvc.Get(ctx,lookup["story"].TaskID)
 if err!=nil{t.Fatal(err)}
 successor,err=taskSvc.MarkReady(ctx,task.TransitionCommand{
  TaskID:successor.ID,ExpectedRevision:successor.Revision,
 })
 if err!=nil{t.Fatal(err)}
 if _,_,err=taskSvc.Start(ctx,task.StartCommand{
  TaskID:successor.ID,ExpectedRevision:successor.Revision,
 });!errors.Is(err,task.ErrHardDependencyUnsatisfied){
  t.Fatalf("successor Task launched before prerequisite: %v",err)
 }
 var attempts int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,successor.ID).Scan(&attempts);
  err!=nil||attempts!=0{t.Fatalf("unsatisfied DAG created Attempt: %d %v",attempts,err)}
 // Simulate a completed, independently verified predecessor in the state
 // fixture. The remaining Task lifecycle can then admit this successor.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE tasks SET state='complete'
  WHERE id=?`,lookup["assets"].TaskID);err!=nil{t.Fatal(err)}
 successor,attempt,err:=taskSvc.Start(ctx,task.StartCommand{
  TaskID:successor.ID,ExpectedRevision:successor.Revision,
 })
 if err!=nil||successor.State!=task.StateRunning||attempt.ID==""{
  t.Fatalf("satisfied successor did not resume: %+v %+v %v",successor,attempt,err)
 }
 // A new graph is scoped to another Project only by a fresh operator action.
 if _,err=svc.TaskGraph(ctx,"other-project",graph.ID);!errors.Is(err,sql.ErrNoRows){
  t.Fatalf("graph exposed across Project boundaries: %v",err)
 }
}

func TestProjectTaskGraphRejectsUnsafeOrNonHumanPlansAtomically(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/bad-plan.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Human','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('model','agent','Model','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','model','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Graph security",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 source,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Source",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 orch:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,
  task.NewService(db.SQL(),db,clock.Real{}),nil)
 makePlan:=func()CreateTaskGraphCommand{return CreateTaskGraphCommand{
  ProjectID:p.ID,Name:"No unsafe graph",IdempotencyKey:"test-key",
  ActorPrincipalID:"human",
  Nodes:[]TaskGraphNodeSpec{{Key:"a",ProjectWorkspaceID:source.ID,Objective:"First"},
   {Key:"b",ProjectWorkspaceID:source.ID,Objective:"Second",DependsOn:[]string{"a"}}},
 }}
 cases:=[]struct{
  label string
  mutate func(*CreateTaskGraphCommand)
 }{
  {"agent_cannot_approve",func(c *CreateTaskGraphCommand){c.ActorPrincipalID="model"}},
  {"self_cycle",func(c *CreateTaskGraphCommand){c.Nodes[0].DependsOn=[]string{"a"}}},
  {"multi_cycle",func(c *CreateTaskGraphCommand){c.Nodes[0].DependsOn=[]string{"b"}}},
  {"unknown_dependency",func(c *CreateTaskGraphCommand){c.Nodes[1].DependsOn=[]string{"missing"}}},
  {"duplicate_key",func(c *CreateTaskGraphCommand){c.Nodes[1].Key="a"}},
  {"duplicate_edge",func(c *CreateTaskGraphCommand){c.Nodes[1].DependsOn=[]string{"a","a"}}},
  {"wrong_workspace",func(c *CreateTaskGraphCommand){c.Nodes[1].ProjectWorkspaceID="foreign"}},
  {"empty_objective",func(c *CreateTaskGraphCommand){c.Nodes[1].Objective=" "}},
  {"unbounded_objective",func(c *CreateTaskGraphCommand){c.Nodes[1].Objective=strings.Repeat("X",5000)}},
  {"invalid_idempotency",func(c *CreateTaskGraphCommand){c.IdempotencyKey="../escape"}},
 }
 for _,tc:=range cases{
  t.Run(tc.label,func(t *testing.T){
   plan:=makePlan()
   tc.mutate(&plan)
   if _,err:=orch.CreateTaskGraph(ctx,plan);err==nil{
    t.Fatal("malformed or unauthorized DAG committed")
   }
   var graphs,graphTasks int
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_task_graphs`).Scan(&graphs);err!=nil{t.Fatal(err)}
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks`).Scan(&graphTasks);err!=nil{t.Fatal(err)}
   if graphs!=0||graphTasks!=0{
    t.Fatalf("malformed graph partially committed: graphs=%d tasks=%d",graphs,graphTasks)
   }
  })
 }
}
