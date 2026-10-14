//go:build integration

package goalobserveradapter

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/approval"
 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/operation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/system"
 "github.com/DigiLogicTech/OnePane/internal/tool"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

type healthyObserverOperationWatchdog struct{}
func(healthyObserverOperationWatchdog)Healthy(context.Context)bool{return true}

// Actual ToolGateway -> one-use Operation permit -> persisted adapter ->
// operator approval -> independent local-state verification -> committed
// Operation -> separately approved inverse Operation. No mock ToolInvocations.
func TestObserverPauseResumeRequiresRealCoordinatorApprovalAndCompensation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/full-observer-operation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=time.Now().UnixMilli()
 seeds:=[]string{
  `INSERT INTO system_state(singleton_id,mode,revision,reason,updated_at)
   VALUES(1,'normal',1,'healthy',?)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Owner','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('approver','human','Independent approver','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('verifier','human','Independent verifier','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','approver','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','verifier','active',?,?)`,
 }
 for _,q:=range seeds{
  var args []any
  if q==seeds[0]{args=[]any{now}}else{args=[]any{now,now}}
  if _,err:=db.SQL().ExecContext(ctx,q,args...);err!=nil{t.Fatal(err)}
 }
 _,err=db.SQL().ExecContext(ctx,`INSERT OR IGNORE INTO roles(
  id,role_class,name,definition_json,revision,created_at,updated_at)
  VALUES('test-observer-approver','human','approver','{}',1,?,?)`,now,now)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO principal_roles(
  principal_id,role_id,workspace_id,created_at)
  SELECT 'approver',id,'tenant',? FROM roles
  WHERE name='approver' AND role_class='human'`,now)
 if err!=nil{t.Fatal(err)}
 goalService:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=goalService.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",CreatedBy:"owner",Name:"Controlled local observer",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=goalService.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Exact Project sandbox",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=goalService.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Observe only when specifically permitted",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 taskID:="human-scoped-observer"
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO tasks(
  id,workspace_id,project_id,project_workspace_id,objective,state,
  scheduling_class,priority,completion_json,revision,created_at,updated_at)
  VALUES(?,?,?,?,?,'running','normal_task',0,'{}',1,?,?)`,
  taskID,"tenant",project.ID,ws.ID,"Operator-directed pause or resume",now,now)
 if err!=nil{t.Fatal(err)}
 resource:=ResourceRef(project.ID,goal.ID)
 auth:=authority.NewService(db.SQL(),db,clock.Real{})
 sys:=system.NewService(db.SQL(),db,clock.Real{})
 pol:=policy.NewEngine(auth,sys,clock.Real{})
 pol.SetWatchdog(healthyObserverOperationWatchdog{})
 reg:=tool.NewRegistry()
 if err:=Register(reg,New(db.SQL(),goalService));err!=nil{t.Fatal(err)}
 gateway:=tool.NewGateway(db.SQL(),db,pol,auth,reg,clock.Real{})
 gate:=operation.NewMutationGate(db.SQL())
 gateway.SetMutationPermitValidator(gate)
 coord:=operation.NewCoordinator(db.SQL(),db,pol,gateway,gate,clock.Real{})
 approvals:=approval.NewService(db.SQL(),db,clock.Real{})
 coord.SetApprovalConsumer(approvals)
 verifyService:=verification.NewService(db.SQL(),db,clock.Real{})
 issue:=func()string{
  t.Helper()
  limit:=int64(1)
  lease,e:=auth.Issue(ctx,authority.IssueCommand{
   WorkspaceID:"tenant",TaskID:&taskID,PrincipalID:"owner",
   CapabilityID:CapabilityID,
   Scope:authority.Scope{
    ResourceRefs:[]string{resource},
    Actions:[]authority.ActionMode{authority.ActionMutate},
   },
   IssuedBy:"owner",ExpiresAt:time.Now().Add(10*time.Minute).UnixMilli(),
   UsageLimit:&limit,
  })
  if e!=nil{t.Fatal(e)}
  return lease.ID
 }
 packInput:=func(rev int64)json.RawMessage{
  raw,e:=json.Marshal(map[string]any{
   "project_id":project.ID,"goal_id":goal.ID,
   "expected_goal_revision":goal.Revision,"expected_fence_revision":rev,
  })
  if e!=nil{t.Fatal(e)}
  return raw
 }
 makeApproved:=func(op operation.Operation)operation.Operation{
  t.Helper()
  request,e:=approvals.Request(ctx,approval.RequestCommand{
   OperationID:op.ID,RequestedBy:"owner",TTLMillis:120000,
  })
  if e!=nil{t.Fatal(e)}
  if _,e=approvals.Resolve(ctx,approval.ResolveCommand{
   ApprovalID:request.ID,ExpectedRevision:request.Revision,
   PrincipalID:"owner",Approve:true,
  });!errors.Is(e,approval.ErrSelfApproval){
   t.Fatalf("self approval accepted: %v",e)
  }
  if _,e=coord.ContinueApproved(ctx,operation.ContinueApprovedCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,ApprovalID:request.ID,
  });e==nil{t.Fatal("unapproved observer effect acquired a resource lease")}
  approved,e:=approvals.Resolve(ctx,approval.ResolveCommand{
   ApprovalID:request.ID,ExpectedRevision:request.Revision,
   PrincipalID:"approver",Approve:true,
  })
  if e!=nil||approved.Status!=approval.Approved{
   t.Fatalf("independent approval failed: %+v %v",approved,e)
  }
  op,e=coord.ContinueApproved(ctx,operation.ContinueApprovedCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,ApprovalID:request.ID,
  })
  if e!=nil||op.State!=operation.StatePrepared||op.ResourceLeaseID==nil{
   t.Fatalf("approved observer operation not prepared: %+v %v",op,e)
  }
  return op
 }
 executeAndVerify:=func(op operation.Operation,lease string,input json.RawMessage,expected string)operation.Operation{
  t.Helper()
  var e error
  op,e=coord.Execute(ctx,operation.ExecuteCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,LeaseID:lease,Input:input,
  })
  if e!=nil||op.State!=operation.StateObserving{
   t.Fatalf("operation not admitted to observing: %+v %v",op,e)
  }
  fence,e:=goalService.GetGoalTCPObservationFence(ctx,project.ID,goal.ID)
  if e!=nil||fence.Status!=expected||fence.ObservationAuthorized{
   t.Fatalf("unexpected persisted local postcondition: %+v %v",fence,e)
  }
  if _,e=coord.Execute(ctx,operation.ExecuteCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,LeaseID:lease,Input:input,
  });e==nil{t.Fatal("Operation Coordinator replayed already-executed effect")}
  v,e:=verifyService.Create(ctx,verification.CreateCommand{
   WorkspaceID:"tenant",TaskID:&taskID,OperationID:&op.ID,
   SubjectRef:resource,RequiredLevel:policy.VerificationV2,
   Spec:json.RawMessage(`{"scope":"local_observer_fence_only"}`),
  })
  if e!=nil{t.Fatal(e)}
  if _,e=coord.CommitVerified(ctx,operation.CommitCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,VerificationID:v.ID,
  });e==nil{t.Fatal("unverified effect committed")}
  level:=policy.VerificationV2
  v,e=verifyService.Resolve(ctx,verification.ResolveCommand{
   VerificationID:v.ID,ExpectedRevision:v.Revision,
   Status:verification.StatusPass,AchievedLevel:&level,
   Result:json.RawMessage(`{"observed":"local_sqlite_fence","independent_device_health":false}`),
   VerifiedBy:"verifier",
  })
  if e!=nil||v.Status!=verification.StatusPass{t.Fatalf("verification failed: %+v %v",v,e)}
  op,e=coord.CommitVerified(ctx,operation.CommitCommand{
   OperationID:op.ID,ExpectedRevision:op.Revision,VerificationID:v.ID,
  })
  if e!=nil||op.State!=operation.StateCommitted{t.Fatalf("verified effect not committed: %+v %v",op,e)}
  return op
 }
 pauseInput:=packInput(0)
 resumeInput:=packInput(1)
 compensation,_:=json.Marshal(map[string]any{
  "tool_id":ResumeToolID,"tool_version":ToolVersion,"resource_ref":resource,
  "input":resumeInput,
  "desired_state":map[string]any{"dispatch":"running"},
  "precondition":map[string]any{"dispatch":"paused"},
  "reconciliation":map[string]any{"type":"local_fence_revision","revision":2},
 })
 pauseLease:=issue()
 op,err:=coord.Prepare(ctx,operation.PrepareCommand{
  WorkspaceID:"tenant",TaskID:&taskID,PrincipalID:"owner",CapabilityLeaseID:pauseLease,
  IdempotencyKey:"operator-pause-1",ToolID:PauseToolID,ToolVersion:ToolVersion,
  ResourceRef:resource,Input:pauseInput,
  DesiredState:json.RawMessage(`{"dispatch":"paused"}`),
  Precondition:json.RawMessage(`{"dispatch":"running"}`),
  Reconciliation:json.RawMessage(`{"type":"local_fence_revision","revision":1}`),
  Compensation:compensation,
 })
 if !errors.Is(err,operation.ErrApprovalRequired)||op.State!=operation.StateAuthorized{
  t.Fatalf("pause failed to require approval: %+v %v",op,err)
 }
 _,err=gateway.Invoke(ctx,tool.InvokeCommand{
  WorkspaceID:"tenant",TaskID:&taskID,PrincipalID:"owner",LeaseID:pauseLease,
  OperationID:&op.ID,ToolID:PauseToolID,ToolVersion:ToolVersion,
  ResourceRef:resource,Input:pauseInput,
 })
 if err==nil{t.Fatal("direct gateway invoked a privileged local observer without coordinator permit")}
 initial,err:=goalService.GetGoalTCPObservationFence(ctx,project.ID,goal.ID)
 if err!=nil||initial.Status!="running"||initial.Revision!=0{
  t.Fatalf("unauthorised attempt changed local observer: %+v %v",initial,err)
 }
 op=makeApproved(op)
 op=executeAndVerify(op,pauseLease,pauseInput,"paused")
 // Rollback is a distinct, separately approved inverse. It cannot use
 // the now-consumed one-shot lease or the first approval.
 resumeLease:=issue()
 undo,err:=coord.Compensate(ctx,operation.CompensateCommand{
  OriginalOperationID:op.ID,PrincipalID:"owner",
  CapabilityLeaseID:resumeLease,IdempotencyKey:"approved-inverse-1",
 })
 if !errors.Is(err,operation.ErrApprovalRequired)||undo.State!=operation.StateAuthorized||
  undo.CompensatesOperationID==nil||*undo.CompensatesOperationID!=op.ID{
  t.Fatalf("rollback bypassed separate approval: %+v %v",undo,err)
 }
 undo=makeApproved(undo)
 undo=executeAndVerify(undo,resumeLease,resumeInput,"running")
 state,err:=goalService.GetGoalTCPObservationFence(ctx,project.ID,goal.ID)
 if err!=nil||state.Revision!=2||state.Status!="running"||state.ObservationAuthorized{
  t.Fatalf("inverse Operation failed to restore dispatch state: %+v %v",state,err)
 }
 var probes int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&probes);err!=nil{t.Fatal(err)}
 if probes!=0{t.Fatalf("observation control dispatched network I/O: %d",probes)}
 // An approval is not standing privilege: removing the approver's tenant
 // membership AFTER approval consumption but BEFORE the Tool call must
 // deny the one-shot permit, without touching the local dispatch fence.
 revokedLease:=issue()
 revokedInput:=packInput(2)
 revokedOp,err:=coord.Prepare(ctx,operation.PrepareCommand{
  WorkspaceID:"tenant",TaskID:&taskID,PrincipalID:"owner",
  CapabilityLeaseID:revokedLease,IdempotencyKey:"approval-revoked-pre-dispatch",
  ToolID:PauseToolID,ToolVersion:ToolVersion,ResourceRef:resource,
  Input:revokedInput,
  DesiredState:json.RawMessage(`{"dispatch":"paused"}`),
  Precondition:json.RawMessage(`{"dispatch":"running"}`),
  Reconciliation:json.RawMessage(`{"type":"local_fence_revision","revision":3}`),
 })
 if !errors.Is(err,operation.ErrApprovalRequired)||revokedOp.State!=operation.StateAuthorized{
  t.Fatalf("revocation test did not require fresh approval: %+v %v",revokedOp,err)
 }
 revokedOp=makeApproved(revokedOp)
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='revoked' WHERE workspace_id='tenant' AND principal_id='approver'`)
 if err!=nil{t.Fatal(err)}
 denied,denial:=coord.Execute(ctx,operation.ExecuteCommand{
  OperationID:revokedOp.ID,ExpectedRevision:revokedOp.Revision,
  LeaseID:revokedLease,Input:revokedInput,
 })
 if denial==nil||denied.State!=operation.StateFailed{
  t.Fatalf("inactive approver's consumed approval still executed: %+v %v",denied,denial)
 }
 unchanged,err:=goalService.GetGoalTCPObservationFence(ctx,project.ID,goal.ID)
 if err!=nil||unchanged.Status!="running"||unchanged.Revision!=2{
  t.Fatalf("denied effect changed local dispatch: %+v %v",unchanged,err)
 }
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='active' WHERE workspace_id='tenant' AND principal_id='approver'`)
 if err!=nil{t.Fatal(err)}
 // A crash after an effect but before independent verification must not
 // replay the original Tool. Bootstrap blocks the uncertain Operation;
 // reconciliation belongs to an authorised operator, not an Agent retry.
 strandedLease:=issue()
 strandedInput:=packInput(2)
 stranded,err:=coord.Prepare(ctx,operation.PrepareCommand{
  WorkspaceID:"tenant",TaskID:&taskID,PrincipalID:"owner",
  CapabilityLeaseID:strandedLease,IdempotencyKey:"crash-before-local-verification",
  ToolID:PauseToolID,ToolVersion:ToolVersion,ResourceRef:resource,
  Input:strandedInput,
  DesiredState:json.RawMessage(`{"dispatch":"paused"}`),
  Precondition:json.RawMessage(`{"dispatch":"running"}`),
  Reconciliation:json.RawMessage(`{"type":"manual_state_check"}`),
 })
 if !errors.Is(err,operation.ErrApprovalRequired)||stranded.State!=operation.StateAuthorized{
  t.Fatalf("crash test preauthorization failed: %+v %v",stranded,err)
 }
 stranded=makeApproved(stranded)
 stranded,err=coord.Execute(ctx,operation.ExecuteCommand{
  OperationID:stranded.ID,ExpectedRevision:stranded.Revision,
  LeaseID:strandedLease,Input:strandedInput,
 })
 if err!=nil||stranded.State!=operation.StateObserving{
  t.Fatalf("crash test effect failed: %+v %v",stranded,err)
 }
 recovered:=operation.NewCoordinator(db.SQL(),db,nil,nil,nil,clock.Real{})
 sweep,err:=recovered.RecoverInterrupted(ctx,nil,nil,nil)
 if err!=nil||len(sweep.Interrupted)!=1||len(sweep.Blocked)!=1||
  sweep.Interrupted[0]!=stranded.ID||sweep.Blocked[0]!=stranded.ID{
  t.Fatalf("restart silently trusted uncertain effect: %+v %v",sweep,err)
 }
 unknown,err:=recovered.Get(ctx,stranded.ID)
 if err!=nil||unknown.State!=operation.StateBlockedUnknownOutcome{
  t.Fatalf("outcome not blocked after restart: %+v %v",unknown,err)
 }
 sweep,err=recovered.RecoverInterrupted(ctx,nil,nil,nil)
 if err!=nil||len(sweep.Interrupted)!=0||len(sweep.Blocked)!=0{
  t.Fatalf("recovery repeated external mutation: %+v %v",sweep,err)
 }
 if _,err=coord.Execute(ctx,operation.ExecuteCommand{
  OperationID:unknown.ID,ExpectedRevision:unknown.Revision,
  LeaseID:strandedLease,Input:strandedInput,
 });err==nil{
  t.Fatal("blocked unknown-outcome operation replayed without human reconciliation")
 }
 finalFence,err:=goalService.GetGoalTCPObservationFence(ctx,project.ID,goal.ID)
 if err!=nil||finalFence.Status!="paused"||finalFence.Revision!=3{
  t.Fatalf("recovery changed local state or reexecuted adapter: %+v %v",finalFence,err)
 }
 inventory,err:=goalService.GoalRetentionInventory(ctx,project.ID,goal.ID)
 if err!=nil||inventory.UnreconciledObserverOperations!=1||
  len(inventory.StorageAttentionReasons)!=1||
  inventory.StorageAttentionReasons[0]!="observer_mutation_needs_reconciliation"{
  t.Fatalf("uncertain local operation invisible to operator: %+v %v",inventory,err)
 }
 cases,err:=goalService.ListGoalObserverRecoveryCases(ctx,project.ID,goal.ID)
 if err!=nil||len(cases)!=1||cases[0].OperationID!=stranded.ID||
  cases[0].State!=string(operation.StateBlockedUnknownOutcome)||
  !cases[0].HumanReconciliationRequired||cases[0].IndependentDeviceVerification{
  t.Fatalf("blocked effect not available as non-executable recovery pointer: %+v %v",cases,err)
 }
 if _,err:=goalService.ListGoalObserverRecoveryCases(ctx,"other-project",goal.ID);err==nil{
  t.Fatal("observer recovery cases exposed to another Project")
 }
 // 30 more source-only historical unresolved rows exercise bounded
 // listing without loading every stored receipt or original Tool input.
 for i:=0;i<30;i++{
  id:=fmt.Sprintf("synthetic-blocked-%02d",i)
  _,err=db.SQL().ExecContext(ctx,`INSERT INTO operations(
   id,workspace_id,task_id,principal_id,idempotency_key,
   state,capability_id,resource_ref,tool_id,tool_version,
   adapter_id,adapter_version,desired_state_json,precondition_json,
   reconciliation_json,policy_revision,input_hash,revision,created_at,updated_at)
   VALUES(?,'tenant',?,'owner',?,'blocked_unknown_outcome',
    ? ,? ,? ,'1',? ,'1','{}','{}','{}',1,'synthetic',1,?,?)`,
    id,taskID,"idem-"+id,CapabilityID,resource,PauseToolID,
    AdapterID,now,now+60000+int64(i+1))
  if err!=nil{t.Fatal(err)}
 }
 limited,err:=goalService.ListGoalObserverRecoveryCases(ctx,project.ID,goal.ID)
 if err!=nil||len(limited)!=25||limited[0].OperationID!="synthetic-blocked-29"{
  t.Fatalf("recovery list not bounded/newest-first: %d %+v %v",len(limited),limited,err)
 }
 for _,entry:=range limited{
  if entry.OperationID==""||entry.IndependentDeviceVerification||
   !entry.HumanReconciliationRequired{
   t.Fatalf("unsafe recovery list entry: %+v",entry)
  }
 }
 // An actively executing Operation is not automatically a crash or a
 // human reconciliation task until the startup recovery sweep identifies
 // an uncertain outcome. It remains operator-visible without false urgency.
 _,err=db.SQL().ExecContext(ctx,`UPDATE operations SET state='executing'
  WHERE id='synthetic-blocked-29'`)
 if err!=nil{t.Fatal(err)}
 live,err:=goalService.ListGoalObserverRecoveryCases(ctx,project.ID,goal.ID)
 if err!=nil||len(live)!=25||live[0].OperationID!="synthetic-blocked-29"||
  live[0].HumanReconciliationRequired{
  t.Fatalf("normal executing effect mislabeled as crashed: %+v %v",live,err)
 }
}
