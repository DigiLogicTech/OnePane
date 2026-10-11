//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestStandingLevel4AutonomyAdmitsBoundedGraphAndRevokesLive(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/consented-local-graph.db")
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
  WorkspaceID:"tenant",Name:"Local-first development",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"autonomous_project"},
   "feature_policy":{"AutonomousRoutines":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Development",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 service:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,tasks,nil)
 spec:=[]TaskGraphNodeSpec{
  {Key:"plan",ProjectWorkspaceID:ws.ID,Objective:"Plan an approved deliverable",ComputePreference:"auto",Priority:10},
  {Key:"build",ProjectWorkspaceID:ws.ID,Objective:"Build a local artifact",DependsOn:[]string{"plan"},ComputePreference:"auto",Priority:10},
 }
 request:=CreateTaskGraphCommand{
  ProjectID:p.ID,Name:"Autonomous Project plan",IdempotencyKey:"l4-consented-graph",
  ActorPrincipalID:"human",AuthorizedWorkspaceID:ws.ID,
  UserObjective:"Create a verified local deliverable",
  AutonomousModelDraft:true,Nodes:spec,
 }
 graph,err:=service.CreateTaskGraph(ctx,request)
 if err!=nil{t.Fatal(err)}
 if len(graph.Nodes)!=2||graph.Nodes[0].ProjectWorkspaceID!=ws.ID||
  graph.Nodes[1].ProjectWorkspaceID!=ws.ID{
  t.Fatalf("autonomous graph crossed scoped Workspace: %+v",graph)
 }
 for _,node:=range graph.Nodes{
  current,err:=tasks.Get(ctx,node.TaskID)
  if err!=nil{t.Fatal(err)}
  var metadata struct{
   Source string `json:"source"`
   OperatorApproved bool `json:"operator_approved"`
   StandingAutonomyPolicy bool `json:"standing_autonomy_policy"`
   UserObjective string `json:"user_objective"`
  }
  if err:=json.Unmarshal(current.Completion,&metadata);err!=nil{t.Fatal(err)}
  if metadata.Source!="project-orchestrator-autonomous-graph"||
   metadata.OperatorApproved||!metadata.StandingAutonomyPolicy||
   metadata.UserObjective!=request.UserObjective{
   t.Fatalf("model graph falsely claimed human review or changed original objective: %+v",metadata)
  }
  allowed,err:=(featurepolicy.StoredScopes{DB:db.SQL()}).AuthorizeTask(
   ctx,"tenant",node.TaskID,"inference.local")
  if err!=nil||!allowed{t.Fatalf("approved local graph Task cannot run: %t %v",allowed,err)}
 }
 retry,err:=service.CreateTaskGraph(ctx,request)
 if err!=nil||retry.ID!=graph.ID{t.Fatalf("idempotent local graph retry mutated plan: %+v %v",retry,err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"onepane_operations":{"version":1,"mode":"autonomous_project"},
    "feature_policy":{"AutonomousRoutines":"disabled"}}' WHERE id=?`,p.ID);err!=nil{t.Fatal(err)}
 for _,node:=range graph.Nodes{
  allowed,err:=(featurepolicy.StoredScopes{DB:db.SQL()}).AuthorizeTask(
   ctx,"tenant",node.TaskID,"inference.local")
  if err!=nil||allowed{t.Fatalf("revoked Project permitted autonomous Task: %t %v",allowed,err)}
 }
 denied:=request;denied.IdempotencyKey="revoked-model-plan"
 if _,err:=service.CreateTaskGraph(ctx,denied);err==nil{
  t.Fatal("revoked standing autonomy admitted a new model graph")
 }
}

func TestAutonomousGraphNeverSpillsWorkspacesOrClaimsLevel5Authority(t *testing.T){
 // An API caller can neither set the internal admission flag nor mint
 // unrelated Workspace targets via the bounded model plan request.
 var cmd CreateTaskGraphCommand
 if err:=json.Unmarshal([]byte(`{"project_id":"p","autonomous_model_draft":true,
   "authorized_workspace_id":"foreign","user_objective":"bypass"}`),&cmd);err!=nil{t.Fatal(err)}
 if cmd.AutonomousModelDraft||cmd.AuthorizedWorkspaceID!=""||cmd.UserObjective!=""{
  t.Fatal("REST input minted an internal-only autonomous graph authority")
 }
}
