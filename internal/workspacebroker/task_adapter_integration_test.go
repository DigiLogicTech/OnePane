//go:build integration

package workspacebroker

import (
 "context"
 "encoding/json"
 "errors"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/system"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

type taskProbeStub struct{
 calls int
 onProbe func(Request)
}
func(f *taskProbeStub) Probe(_ context.Context,req Request)(Receipt,error){
 f.calls++
 if f.onProbe!=nil{f.onProbe(req)}
 return Receipt{LinkID:req.LinkID,Healthy:true,Status:"healthy",StatusCode:200},nil
}
func pLink(v string)*string{return &v}

func TestBrokerToolGatewayTaskAttemptLeaseAndRevocation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"state.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=time.Now().UnixMilli()
 for _,st:=range []string{
  `INSERT INTO system_state(singleton_id,mode,revision,reason,updated_at)
   VALUES(1,'normal',1,'test',?)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Workspace','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('worker','agent','Worker','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','worker','active',?,?)`,
 }{
  var args []any
  if len(st)>0 && st[:6]=="INSERT" {
   if contains2(st,"created_at,updated_at"){args=[]any{now,now}}else{args=[]any{now}}
  }
  if _,err=db.SQL().ExecContext(ctx,st,args...);err!=nil{t.Fatalf("seed %s: %v",st,err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Task-broker review",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 source,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 target,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Story",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:project.ID,ProjectWorkspaceID:&source.ID,CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Scoped service",SourceKind:projectworkspace.AppOCIImage,
  SourceRef:"ghcr.io/example/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 endpoint,err:=projects.DeclareEndpoint(ctx,projectworkspace.DeclareEndpointCommand{
  RuntimeID:runtime.ID,ApplicationID:&app.ID,Name:"Health",
  Protocol:"http",InternalPort:8080,Exposure:projectworkspace.ExposurePreview,
  CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_workspace_service_links(
  id,project_id,source_workspace_id,target_workspace_id,endpoint_id,application_id,
  name,approved_path,enabled,expires_at_ms,revision,
  application_revision,endpoint_revision,container_spec_hash,verification_id,
  created_by,created_at,updated_at)
  VALUES('grant',?,?,?,?,?,'Health','/health',1,?,1,1,1,
  'spec','verification',?,?,?)`,
  project.ID,source.ID,target.ID,endpoint.ID,app.ID,now+60000,
  "operator",now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:pLink(project.ID),
  ProjectWorkspaceID:pLink(target.ID),Objective:"Check approved upstream status",
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 running,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:ready.ID,ExpectedRevision:ready.Revision,
  WorkerPrincipalID:pLink("worker"),ActorPrincipalID:pLink("operator"),
 })
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO agent_worker_runs(
  id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,
  capability_id,protocol_level,max_steps,step_count,max_replans,replan_count,
  max_escalations,escalation_count,route_policy_json,continuation_json,
  revision,started_at,updated_at)
  VALUES('run','tenant',?,?,'worker','running','assistant',?,'L1',
  16,0,3,0,2,0,'{}','{}',1,?,?)`,
  created.ID,attempt.ID,CapabilityID,now,now);err!=nil{t.Fatal(err)}
 auth:=authority.NewService(db.SQL(),db,clock.Real{})
 sys:=system.NewService(db.SQL(),db,clock.Real{})
 pol:=policy.NewEngine(auth,sys,clock.Real{})
 reg:=tool.NewRegistry()
 probe:=&taskProbeStub{}
 adapter:=NewTaskAdapter(db.SQL(),probe,true)
 if err=RegisterTaskTool(reg,adapter);err!=nil{t.Fatal(err)}
 gateway:=tool.NewGateway(db.SQL(),db,pol,auth,reg,clock.Real{})
 issue:=func(resource string)string{
  t.Helper()
  amount:=int64(2)
  lease,err:=auth.Issue(ctx,authority.IssueCommand{
   WorkspaceID:"tenant",PrincipalID:"worker",TaskID:pLink(created.ID),
   CapabilityID:CapabilityID,
   Scope:authority.Scope{
    ResourceRefs:[]string{resource},Actions:[]authority.ActionMode{authority.ActionRead},
   },IssuedBy:"operator",ExpiresAt:now+60000,UsageLimit:&amount,
  })
  if err!=nil{t.Fatal(err)}
  return lease.ID
 }
 ref:=ResourcePrefix+"grant"
 lease:=issue(ref)
 invoke:=func(resource,leaseID string,attemptID *string,input string)(tool.Invocation,error){
  t.Helper()
  return gateway.Invoke(ctx,tool.InvokeCommand{
   WorkspaceID:"tenant",TaskID:pLink(created.ID),AttemptID:attemptID,
   PrincipalID:"worker",LeaseID:leaseID,ToolID:ToolID,
   ToolVersion:ToolVersion,ResourceRef:resource,Input:json.RawMessage(input),
  })
 }
 input:=`{"link_id":"grant"}`
 good,err:=invoke(ref,lease,&attempt.ID,input)
 if err!=nil||good.Status!=tool.StatusSucceeded||probe.calls!=1{
  t.Fatalf("trusted Gateway health invocation failed: %+v %v calls=%d",good,err,probe.calls)
 }
 var receipt Receipt
 if err=json.Unmarshal(good.Result,&receipt);err!=nil||!receipt.Healthy||receipt.LinkID!="grant"{
  t.Fatalf("declassified receipt invalid: %+v %v",receipt,err)
 }
 updated,err:=auth.Get(ctx,lease)
 if err!=nil||updated.UsageCount!=1{t.Fatalf("lease not consumed exactly once: %+v %v",updated,err)}
 // A gateway invocation with an otherwise good lease but no Attempt must
 // fail at the broker boundary before any physical network action.
 missing,err:=invoke(ref,lease,nil,input)
 if err==nil||missing.Status!=tool.StatusFailed||probe.calls!=1{
  t.Fatalf("missing Attempt accessed source: %+v %v calls=%d",missing,err,probe.calls)
 }
 // A model that invents a new link cannot repurpose an exact resource lease.
 invented,err:=invoke(ResourcePrefix+"unapproved",lease,&attempt.ID,`{"link_id":"unapproved"}`)
 if err==nil||invented.Status!=tool.StatusFailed||probe.calls!=1{
  t.Fatalf("ungranted resource accessed source: %+v %v",invented,err)
 }
 // Exact invocation identity, input hash and running status are mandatory;
 // direct unregistered calls can never substitute for Gateway authorization.
 if _,err=adapter.Invoke(ctx,tool.AdapterRequest{
  InvocationID:"invented",WorkspaceID:"tenant",TaskID:&created.ID,
  AttemptID:&attempt.ID,ToolID:ToolID,ToolVersion:ToolVersion,
  ResourceRef:ref,Input:json.RawMessage(input),
 });!errors.Is(err,tool.ErrAdapterFailure)&&err==nil{
  t.Fatal("forged direct call was authorized")
 }
 if probe.calls!=1{t.Fatal("forged direct call dialled a service")}
 // A valid per-call lease cannot let an Agent move its persisted Task
 // into the source Workspace to read what the original target could access.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE tasks SET project_workspace_id=?
  WHERE id=?`,source.ID,created.ID);err!=nil{t.Fatal(err)}
 moved,err:=invoke(ref,issue(ref),&attempt.ID,input)
 if err==nil||moved.Status!=tool.StatusFailed||probe.calls!=1{
  t.Fatalf("source Workspace Task incorrectly admitted: %+v %v",moved,err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE tasks SET project_workspace_id=?
  WHERE id=?`,target.ID,created.ID);err!=nil{t.Fatal(err)}
 // Agent Worker state is not equivalent to a running Task Attempt. A
 // suspended Worker cannot initiate the same resource even with a fresh lease.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs SET status='waiting'
  WHERE id='run'`);err!=nil{t.Fatal(err)}
 waiting,err:=invoke(ref,issue(ref),&attempt.ID,input)
 if err==nil||waiting.Status!=tool.StatusFailed||probe.calls!=1{
  t.Fatalf("waiting Worker incorrectly admitted: %+v %v",waiting,err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs SET status='running'
  WHERE id='run'`);err!=nil{t.Fatal(err)}
 // Revoke during an otherwise successful broker read. Adapter checks
 // the persisted grant again before releasing even the sanitized result.
 probe.onProbe=func(_ Request){
  if _,e:=db.SQL().ExecContext(ctx,`UPDATE project_workspace_service_links SET enabled=0
   WHERE id='grant'`);e!=nil{t.Error(e)}
 }
 denied,err:=invoke(ref,issue(ref),&attempt.ID,input)
 if err==nil||denied.Status!=tool.StatusFailed||probe.calls!=2{
  t.Fatalf("in-flight revocation disclosed a result: %+v %v",denied,err)
 }
 // A disabled adapter is always inert, irrespective of caller metadata.
 inert:=NewTaskAdapter(db.SQL(),probe,false)
 if _,err=inert.Invoke(ctx,tool.AdapterRequest{
  InvocationID:"run",WorkspaceID:"tenant",TaskID:&running.ID,
  AttemptID:&attempt.ID,ToolID:ToolID,ToolVersion:ToolVersion,
  ResourceRef:ref,Input:json.RawMessage(input),
 });err==nil{t.Fatal("disabled adapter was callable")}
}

func contains2(s,sub string)bool{
 for i:=0;i+len(sub)<=len(s);i++{if s[i:i+len(sub)]==sub{return true}}
 return false
}
