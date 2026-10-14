//go:build integration

package goalobserveradapter

import (
 "context"
 "encoding/json"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

// These test rows simulate a Tool Gateway record *after* the coordinator's
// separate mutation permit and authority checks. Gateway tests independently
// verify that agents cannot create this record without real admission.
func TestHumanBoundGoalTCPObserverAdapterPersistsAndRequiresActualInvocationPrincipal(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-observer-adapter.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Observer effect",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Local",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Control only the existing TCP observer",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=time.Now().UnixMilli()
 taskID:="observer-task"
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO tasks(
  id,workspace_id,project_id,project_workspace_id,objective,state,
  scheduling_class,priority,completion_json,revision,created_at,updated_at)
  VALUES(?,?,?,?,?,'running','normal_task',0,'{}',1,?,?)`,
  taskID,"tenant",p.ID,ws.ID,"Human-reviewed exact observer control",now,now)
 if err!=nil{t.Fatal(err)}
 a:=New(db.SQL(),svc)
 mk:=func(id,actor,toolID string,rev int64)tool.AdapterRequest{
  t.Helper()
  now:=time.Now().UnixMilli()
  resource:=ResourceRef(p.ID,g.ID)
  _,err:=db.SQL().ExecContext(ctx,`INSERT INTO operations(
   id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,
   resource_ref,tool_id,tool_version,adapter_id,adapter_version,
   desired_state_json,precondition_json,reconciliation_json,
   policy_revision,input_hash,revision,created_at,updated_at)
   VALUES(?,? ,? ,? ,? ,'executing',?, ?, ?, '1',?, '1',
    '{}','{}','{}',1,'testhash',1,?,?)`,
   "op-"+id,"tenant",taskID,actor,"idem-"+id,CapabilityID,
   resource,toolID,AdapterID,now,now)
  if err!=nil{t.Fatal(err)}
  _,err=db.SQL().ExecContext(ctx,`INSERT INTO tool_invocations(
    id,workspace_id,task_id,principal_id,operation_id,tool_id,tool_version,
    adapter_id,adapter_version,mode,resource_ref,input_hash,status,
    created_at,updated_at)
   VALUES(?,'tenant',?,?,?,?,'1',?,'1','mutate',?,'testhash','running',?,?)`,
   "inv-"+id,taskID,actor,"op-"+id,toolID,AdapterID,resource,now,now)
  if err!=nil{t.Fatal(err)}
  revInput,_:=json.Marshal(map[string]any{
   "project_id":p.ID,"goal_id":g.ID,
   "expected_goal_revision":g.Revision,
   "expected_fence_revision":rev,
  })
  return tool.AdapterRequest{
   InvocationID:"inv-"+id,WorkspaceID:"tenant",TaskID:&taskID,ToolID:toolID,
   ToolVersion:"1",ResourceRef:resource,Input:revInput,
  }
 }
 robot:=mk("robot","robot",PauseToolID,0)
 if _,err:=a.Invoke(ctx,robot);err==nil{
  t.Fatal("Agent-created executing rows impersonated human")
 }
 initial,err:=svc.GetGoalTCPObservationFence(ctx,p.ID,g.ID)
 if err!=nil||initial.Status!="running"||initial.Revision!=0{
  t.Fatalf("robot changed observer state: %+v %v",initial,err)
 }
 human:=mk("human","owner",PauseToolID,0)
 outcome,err:=a.Invoke(ctx,human)
 if err!=nil||len(outcome.Result)==0{t.Fatalf("approved human pause failed: %+v %v",outcome,err)}
 persisted,err:=svc.GetGoalTCPObservationFence(ctx,p.ID,g.ID)
 if err!=nil||persisted.Status!="paused"||persisted.Revision!=1||
  persisted.ChangedBy!="owner"||persisted.ObservationAuthorized{
  t.Fatalf("persisted pause was not verified: %+v %v",persisted,err)
 }
 if _,err:=a.Invoke(ctx,human);err==nil{
  t.Fatal("one-shot effect replay unexpectedly mutated twice")
 }
 // A valid human and tenant Workspace are insufficient: a Task
 // owned by a different Project must not inherit this Goal's authority.
 foreignProject,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Other isolated Project",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 foreignWS,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:foreignProject.ID,Name:"Different sandbox",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 otherTask:="foreign-task"
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO tasks(
  id,workspace_id,project_id,project_workspace_id,objective,state,
  scheduling_class,priority,completion_json,revision,created_at,updated_at)
  VALUES(?,?,?,?,?,'running','normal_task',0,'{}',1,?,?)`,
  otherTask,"tenant",foreignProject.ID,foreignWS.ID,"Unrelated project",now,now)
 if err!=nil{t.Fatal(err)}
 taskID=otherTask
 crossProject:=mk("cross-project","owner",ResumeToolID,1)
 if _,err:=a.Invoke(ctx,crossProject);err==nil{
  t.Fatal("foreign Project Task permitted observer resume")
 }
 crossProject.TaskID=nil
 if _,err:=a.Invoke(ctx,crossProject);err==nil{
  t.Fatal("Taskless invocation permitted observer mutation")
 }
 // A direct writer invocation carrying a foreign Task is also denied,
 // even when the actor is otherwise a valid human Project member.
 _,err=svc.SetGoalTCPObservationFence(ctx,
  projectworkspace.SetGoalTCPObservationFenceCommand{
   ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
   AttachedTaskID:otherTask,TargetStatus:"running",
   ExpectedGoalRevision:g.Revision,ExpectedFenceRevision:1,
  })
 if err==nil{t.Fatal("writer transaction admitted cross-Project Task")}
 taskID="observer-task"
 unchanged,err:=svc.GetGoalTCPObservationFence(ctx,p.ID,g.ID)
 if err!=nil||unchanged.Status!="paused"||unchanged.Revision!=1{
  t.Fatalf("cross-Project attempt mutated observer: %+v %v",unchanged,err)
 }
 restored:=mk("restore","owner",ResumeToolID,1)
 if _,err:=a.Invoke(ctx,restored);err!=nil{t.Fatalf("compensation-style new human operation denied: %v",err)}
 again:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 current,err:=again.GetGoalTCPObservationFence(ctx,p.ID,g.ID)
 if err!=nil||current.Status!="running"||current.Revision!=2||
  current.ObservationAuthorized{
  t.Fatalf("resume did not persist across new service instance: %+v %v",current,err)
 }
 var tasks,probes int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&probes);err!=nil{t.Fatal(err)}
 if tasks!=2||probes!=0{t.Fatalf("adapter created Tasks or TCP probes: %d Tasks, %d probes",tasks,probes)}
}
