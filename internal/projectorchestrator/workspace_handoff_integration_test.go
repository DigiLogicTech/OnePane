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
 "github.com/DigiLogicTech/OnePane/internal/inference"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

type handoffRoute struct{}
func (handoffRoute) Route(_ context.Context, _ scheduler.RouteRequest)(scheduler.Decision,error){
 return scheduler.Decision{Selected:&scheduler.RankedCandidate{
  Candidate:scheduler.Candidate{
   ID:"test-qualified-local-model",Kind:scheduler.CandidateModel,
   Local:true,HardZeroIncrementalCost:true,CostClass:scheduler.CostLocal,
  },
 }},nil
}
type handoffInference struct{contexts []string}
func (f *handoffInference) Execute(_ context.Context,c inference.ExecuteCommand)(inference.InferenceRequest,error){
 f.contexts=append(f.contexts,string(c.ContextManifestJSON))
 id:="handoff-first"
 if len(f.contexts)==2 {id="handoff-second"}
 return inference.InferenceRequest{ResponseArtifactID:&id},nil
}
type handoffArtifacts struct{destination,third string}
func (a handoffArtifacts) Open(_ context.Context,id string)(io.ReadCloser,artifact.Artifact,error){
 plan:=map[string]any{
  "answer":"SOURCE_PRIVATE_RESPONSE_NOT_FOR_DESTINATION",
  "action":"task","objective":"SOURCE_SECRET_INCLUDED_IN_UNSAFE_PLAN",
  "project_workspace_id":a.destination,
 }
 if id=="handoff-second"{
  plan=map[string]any{
   "answer":"Destination-scoped plan",
   "action":"task","objective":"DESTINATION_SAFE_OBJECTIVE",
   "plan":map[string]any{"version":1,"steps":[]map[string]any{
     {"key":"plan_step","objective":"Draft a local-only deliverable"},
   }},
   // A second model suggestion must never loop or override chosen target.
   "project_workspace_id":a.third,
  }
 }
 raw,_:=json.Marshal(plan)
 wrapped,_:=json.Marshal(map[string]any{"output_text":string(raw)})
 return io.NopCloser(strings.NewReader(string(wrapped))),artifact.Artifact{},nil
}

func TestOrchestratorHandoffRebindsReasoningToDestinationWorkspace(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/handoff.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('human','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','human','active',?,?)`,
 } {
  if _,err:=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Bounded handoff",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 var ids []string
 for _,name:=range []string{"World","Story","Assets"}{
  ws,e:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
   ProjectID:p.ID,Name:name,ActorPrincipalID:"human",
  })
  if e!=nil{t.Fatal(e)}
  ids=append(ids,ws.ID)
 }
 taskSvc:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,taskSvc,nil)
 _,sourceID,_,_,err:=svc.contextSnapshot(ctx,p.ID,"")
 if err!=nil{t.Fatal(err)}
 destID,thirdID:="",""
 for _,id:=range ids{
  if id==sourceID{continue}
  if destID==""{destID=id}else{thirdID=id}
 }
 if destID==""||thirdID==""{t.Fatal("missing distinct destination and forbidden third Workspace")}
 _,err=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&sourceID,
  Objective:"SOURCE_PRIVATE_TASK_CONTEXT_CANARY",
 })
 if err!=nil{t.Fatal(err)}
 _,err=taskSvc.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&destID,
  Objective:"DESTINATION_PRIVATE_TASK_CONTEXT_CANARY",
 })
 if err!=nil{t.Fatal(err)}
 infer:=&handoffInference{}
 svc=NewService(db.SQL(),db,clock.Real{},handoffRoute{},infer,
  handoffArtifacts{destination:destID,third:thirdID},taskSvc,nil)
 result,err:=svc.Turn(ctx,TurnCommand{
  ProjectID:p.ID,ActorPrincipalID:"human",
  Objective:"Create a deliverable in the appropriate Workspace",
  AllowTaskCreation:true,
 })
 if err!=nil{t.Fatal(err)}
 if result.TaskID==nil||result.ProjectWorkspaceID==nil||
  *result.ProjectWorkspaceID!=destID||result.Disposition!="task_created"{
  t.Fatalf("wrong final Workspace Task handoff: %+v",result)
 }
 persisted,err:=taskSvc.Get(ctx,*result.TaskID)
 if err!=nil{t.Fatal(err)}
 if persisted.ProjectWorkspaceID==nil||*persisted.ProjectWorkspaceID!=destID||
  persisted.Objective!="Create a deliverable in the appropriate Workspace"{
  t.Fatalf("model rewrote the user-approved Task objective across Workspace boundary: %+v",persisted)
 }
 if !strings.Contains(string(result.Turn.ProvenanceJSON),"draft_only_no_task_authority")||
  !strings.Contains(string(result.Turn.ProvenanceJSON),destID)||
  !strings.Contains(result.Turn.Content,"Draft a local-only deliverable"){
  t.Fatalf("safe model plan was not retained as nonexecuting scoped draft: %+v",result.Turn)
 }
 var autoGraphs int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_orchestrator_task_graphs WHERE project_id=?",p.ID).
  Scan(&autoGraphs);err!=nil||autoGraphs!=0{
  t.Fatalf("model draft was executed as an unapproved Task graph: count=%d err=%v",autoGraphs,err)
 }
  if len(infer.contexts)!=2||
  !strings.Contains(infer.contexts[0],"SOURCE_PRIVATE_TASK_CONTEXT_CANARY")||
  strings.Contains(infer.contexts[0],"DESTINATION_PRIVATE_TASK_CONTEXT_CANARY")||
  !strings.Contains(infer.contexts[1],"DESTINATION_PRIVATE_TASK_CONTEXT_CANARY")||
  strings.Contains(infer.contexts[1],"SOURCE_PRIVATE_TASK_CONTEXT_CANARY"){
  t.Fatalf("reasoning did not rebind to selected destination evidence: %+v",infer.contexts)
 }
 if strings.Contains(string(result.Turn.ContextJSON),"SOURCE_PRIVATE_TASK_CONTEXT_CANARY")||
  !strings.Contains(string(result.Turn.ContextJSON),"DESTINATION_PRIVATE_TASK_CONTEXT_CANARY")||
  strings.Contains(result.Turn.Content,"SOURCE_PRIVATE_RESPONSE_NOT_FOR_DESTINATION")||
  !strings.HasPrefix(result.Turn.Content,"Destination-scoped plan"){
  t.Fatalf("persisted destination Turn contains source-only model context or response: %+v",result.Turn)
 }
}


func TestOrchestratorRejectsModelSelectedWorkspaceWithRevokedReasoningPolicy(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/denied-handoff.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',?,?)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}}
 projectSvc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projectSvc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Denied destination",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 ids:=[]string{}
 for _,name:=range []string{"Private files","Orchestration"}{
  ws,err:=projectSvc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
   ProjectID:p.ID,Name:name,ActorPrincipalID:"human",
  })
  if err!=nil{t.Fatal(err)}
  ids=append(ids,ws.ID)
 }
 taskSvc:=task.NewService(db.SQL(),db,clock.Real{})
 preflight:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,taskSvc,nil)
 _,source,_,_,err:=preflight.contextSnapshot(ctx,p.ID,"")
 if err!=nil{t.Fatal(err)}
 dest:=ids[0]
 if dest==source{dest=ids[1]}
 if dest==source{t.Fatal("destination equals source")}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
 SET ai_settings_json='{"feature_policy":{"ProjectAI":"disabled"}}' WHERE id=?`,dest);err!=nil{t.Fatal(err)}
 infer:=&handoffInference{}
 svc:=NewService(db.SQL(),db,clock.Real{},handoffRoute{},infer,
  handoffArtifacts{destination:dest,third:source},taskSvc,nil)
 if _,err:=svc.Turn(ctx,TurnCommand{
  ProjectID:p.ID,ActorPrincipalID:"human",Objective:"Run Project operation",
  AllowTaskCreation:true,ForceTask:true,
 });err==nil{
  t.Fatal("model redirected Task into Workspace with revoked Project AI")
 }
 if len(infer.contexts)!=1{
  t.Fatalf("blocked destination entered inference: %d calls",len(infer.contexts))
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM tasks WHERE project_id=?",p.ID).Scan(&count);err!=nil{
  t.Fatal(err)
 }
 if count!=0{t.Fatalf("model redirected Task despite policy denial: count=%d",count)}
}
