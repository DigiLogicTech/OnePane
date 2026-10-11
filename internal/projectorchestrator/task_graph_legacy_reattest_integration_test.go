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
)

func TestLegacyGraphReattestationRequiresOriginalPlanAndHumanUnexecutedTasks(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/reattest.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Operator','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','agent','Model','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','agent','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Preserve legacy approvals",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"World",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Story",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,tasks,nil)
 nodes:=[]TaskGraphNodeSpec{
  {Key:"world",ProjectWorkspaceID:world.ID,Objective:"Make world",ComputePreference:"cpu_only"},
  {Key:"story",ProjectWorkspaceID:story.ID,Objective:"Write story",DependsOn:[]string{"world"},ComputePreference:"auto"},
 }
 graph,err:=svc.CreateTaskGraph(ctx,CreateTaskGraphCommand{
  ProjectID:project.ID,Name:"Legacy story graph",IdempotencyKey:"same-review-v1",
  ActorPrincipalID:"human",Nodes:nodes,
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
  SET approved_dependencies_recorded=0,approved_dependencies_json='[]' WHERE graph_id=?`,
  graph.ID);err!=nil{t.Fatal(err)}
 legacy,err:=svc.TaskGraph(ctx,project.ID,graph.ID)
 if err!=nil||legacy.Progress.NeedsAttention!=2{t.Fatalf("legacy graph admitted without review: %+v %v",legacy,err)}
 cmd:=ReattestLegacyTaskGraphCommand{
  ProjectID:project.ID,GraphID:graph.ID,
  ExpectedManifestSHA256:graph.ManifestSHA256,Nodes:nodes,
  ActorPrincipalID:"human",ConfirmOperatorReview:true,
 }
 checkRejected:=func(name string,mutate func(*ReattestLegacyTaskGraphCommand)){
  t.Helper()
  t.Run(name,func(t *testing.T){
   c:=cmd;c.Nodes=append([]TaskGraphNodeSpec(nil),cmd.Nodes...)
   mutate(&c)
   if _,err:=svc.ReattestLegacyTaskGraph(ctx,c);err==nil{
    t.Fatal("unsafe legacy reattestation authorized")
   }
   var count int
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_graph_reattestations`).Scan(&count);err!=nil{t.Fatal(err)}
   if count!=0{t.Fatalf("failed review left an immutable receipt: %d",count)}
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_task_graph_nodes
    WHERE graph_id=? AND approved_dependencies_recorded=1`,graph.ID).Scan(&count);err!=nil{t.Fatal(err)}
   if count!=0{t.Fatalf("failed review attested Task predecessors: %d",count)}
  })
 }
 checkRejected("no_human_ack",func(c *ReattestLegacyTaskGraphCommand){c.ConfirmOperatorReview=false})
 checkRejected("model_cannot_approve",func(c *ReattestLegacyTaskGraphCommand){c.ActorPrincipalID="agent"})
 checkRejected("wrong_manifest",func(c *ReattestLegacyTaskGraphCommand){c.ExpectedManifestSHA256="sha256:"+string(make([]byte,64))})
 checkRejected("changed_objective",func(c *ReattestLegacyTaskGraphCommand){c.Nodes[1].Objective="Silently changed"})
 checkRejected("removed_dependency_in_proposal",func(c *ReattestLegacyTaskGraphCommand){c.Nodes[1].DependsOn=nil})
 checkRejected("reassigned_workspace",func(c *ReattestLegacyTaskGraphCommand){c.Nodes[0].ProjectWorkspaceID=story.ID})
 var taskIDs=map[string]string{}
 for _,n:=range graph.Nodes{taskIDs[n.Key]=n.TaskID}
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM task_dependencies WHERE
  task_id=? AND depends_on_task_id=?`,taskIDs["story"],taskIDs["world"]);err!=nil{t.Fatal(err)}
 checkRejected("missing_live_edge",func(c *ReattestLegacyTaskGraphCommand){})
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO task_dependencies(
  task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json)
  VALUES(?,?,'hard','all','{}')`,taskIDs["story"],taskIDs["world"]);err!=nil{t.Fatal(err)}
 originalTask,err:=tasks.Get(ctx,taskIDs["world"])
 if err!=nil{t.Fatal(err)}
 reviewed,err:=svc.ReattestLegacyTaskGraph(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 if reviewed.Progress.NeedsAttention!=0||reviewed.Progress.Admissible!=1||
  reviewed.Progress.Waiting!=1{t.Fatalf("verified original DAG did not recover: %+v",reviewed)}
 after,err:=tasks.Get(ctx,taskIDs["world"])
 if err!=nil{t.Fatal(err)}
 if originalTask.Revision!=after.Revision||originalTask.State!=after.State||
  originalTask.Objective!=after.Objective{
  t.Fatalf("metadata reattestation modified existing Task: before=%+v after=%+v",originalTask,after)
 }
 var receipts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_graph_reattestations
  WHERE graph_id=? AND reviewed_by='human'`,graph.ID).Scan(&receipts);err!=nil{t.Fatal(err)}
 if receipts!=1{t.Fatalf("immutable operator audit missing: %d",receipts)}
 if _,err:=svc.ReattestLegacyTaskGraph(ctx,cmd);!errors.Is(err,ErrGraphConflict){
  t.Fatalf("duplicate reattestation was permitted: %v",err)
 }
}
