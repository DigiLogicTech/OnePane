//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "io"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

type autonomousPlanArtifacts struct{}
func(autonomousPlanArtifacts)Open(_ context.Context,_ string)(io.ReadCloser,artifact.Artifact,error){
 proposal:=map[string]any{
  "answer":"Proposed a bounded local Project plan",
  "action":"task",
  "plan":map[string]any{"version":1,"steps":[]map[string]any{
   {"key":"design","objective":"Design the approved deliverable"},
   {"key":"implement","objective":"Implement the local deliverable",
    "depends_on":[]string{"design"}},
  }},
 }
 raw,_:=json.Marshal(proposal)
 envelope,_:=json.Marshal(map[string]any{"output_text":string(raw)})
 return io.NopCloser(strings.NewReader(string(envelope))),artifact.Artifact{},nil
}
func TestProjectOrchestratorCreatesAndRevokesStandingL4LocalTaskGraph(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/real-local-l4-graph.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Build offline",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"autonomous_project"},
   "feature_policy":{"AutonomousRoutines":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Development",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=NewService(db.SQL(),db,clock.Real{},handoffRoute{},&handoffInference{},
  autonomousPlanArtifacts{},tasks,nil)
 objective:="Produce a verified offline deliverable"
 result,err:=svc.Turn(ctx,TurnCommand{
  ProjectID:p.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"human",
  Objective:objective,ForceTask:true,AllowTaskCreation:true,
 })
 if err!=nil{t.Fatal(err)}
 if result.TaskID==nil||result.TaskGraphID==nil||result.Disposition!="task_graph_created"{
  t.Fatalf("approved local Project plan was not persisted as a graph: %+v",result)
 }
 graph,err:=svc.TaskGraph(ctx,p.ID,*result.TaskGraphID)
 if err!=nil{t.Fatal(err)}
 if len(graph.Nodes)!=2||graph.Nodes[0].ProjectWorkspaceID!=workspace.ID||
  graph.Nodes[1].ProjectWorkspaceID!=workspace.ID{
  t.Fatalf("autonomous planner crossed canonical Workspace: %+v",graph)
 }
 if !strings.Contains(string(result.Turn.ProvenanceJSON),"standing_L4_autonomy_policy")||
  !strings.Contains(result.Turn.Content,"Task dependencies are enforced"){
  t.Fatalf("standing admission/provenance was hidden: %+v",result.Turn)
 }
 // Turning off the Project's standing grant must reject future graphs.
 // Explicit user Run may still queue a single Task under other grants.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"onepane_operations":{"version":1,"mode":"autonomous_project"},
    "feature_policy":{"AutonomousRoutines":"disabled"}}'
  WHERE id=?`,p.ID);err!=nil{t.Fatal(err)}
 second,err:=svc.Turn(ctx,TurnCommand{
  ProjectID:p.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"human",
  Objective:"Create a second local deliverable",
  ForceTask:true,AllowTaskCreation:true,
 })
 if err!=nil{t.Fatal(err)}
 if second.TaskGraphID!=nil||second.TaskID==nil||
  second.Disposition!="task_created"{
  t.Fatalf("revoked autonomy still created executable graph: %+v",second)
 }
 persisted,err:=tasks.Get(ctx,*second.TaskID)
 if err!=nil{t.Fatal(err)}
 if persisted.Objective!="Create a second local deliverable"{
  t.Fatalf("revocation fallback rewrote operator Task objective: %+v",persisted)
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_orchestrator_task_graphs WHERE project_id=?",p.ID).
  Scan(&count);err!=nil||count!=1{
  t.Fatalf("rejected autonomy created additional Task graph: count=%d err=%v",count,err)
 }
}
