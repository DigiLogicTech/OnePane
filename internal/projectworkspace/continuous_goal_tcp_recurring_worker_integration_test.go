//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestRecurringTCPWorkerRequiresOptInAndPersistsCadenceAcrossRestart(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/recurring-worker.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 if err:=svc.EnsureTCPRecurringWorkerPrincipal(ctx);err!=nil{t.Fatal(err)}
 if err:=svc.EnsureTCPRecurringWorkerPrincipal(ctx);err!=nil{
  t.Fatalf("recurring worker identity not idempotent: %v",err)
 }
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Home network test",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"LAN",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Only the specifically approved LAN source",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 fake:=&fakeGoalTCPDialer{}
 before,err:=svc.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(before)!=0||fake.calls!=0{
  t.Fatalf("unapproved goal opened TCP socket: %+v %v",before,err)
 }
 expiry:=clock.Real{}.UnixMilli()+60*60*1000
 manual,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",
  TargetIPv4:"192.168.9.23",TargetPort:443,TimeoutMS:350,
  ExpiresAt:expiry,ExpectedGoalRevision:g.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 before,err=svc.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(before)!=0||fake.calls!=0{
  t.Fatalf("manual grant silently activated unattended network I/O: %+v %v",before,err)
 }
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 grant,err:=svc.ApproveTCPRecurringGrant(ctx,ApproveTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:g.ID,ManualPermitID:manual.ID,
  ExpectedGoalRevision:g.Revision,ExpectedManualPermitRevision:manual.Revision,
  ActorPrincipalID:"owner",ExpiresAt:expiry,
 })
 if err!=nil{t.Fatal(err)}
 // No socket is created by a human pause, including after a restart.
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 beforeFence,err:=svc.GetGoalTCPObservationFence(ctx,p.ID,g.ID)
 if err!=nil||beforeFence.Revision!=0||beforeFence.ObservationAuthorized{
  t.Fatalf("unexpected implicit observer permission: %+v %v",beforeFence,err)
 }
 paused,err:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
  ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
  TargetStatus:"paused",ExpectedGoalRevision:g.Revision,
  ExpectedFenceRevision:beforeFence.Revision,
 })
 if err!=nil||paused.Status!="paused"||paused.Revision!=1{
  t.Fatalf("operator pause rejected: %+v %v",paused,err)
 }
 if _,err:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
  ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
  TargetStatus:"running",ExpectedGoalRevision:g.Revision,
  ExpectedFenceRevision:0,
 });!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale resume override accepted: %v",err)
 }
 hold:=NewService(db.SQL(),db,clock.Real{})
 skipped,err:=hold.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(skipped)!=0||fake.calls!=0{
  t.Fatalf("paused observer dialled: %+v %v calls=%d",skipped,err,fake.calls)
 }
 _,err=svc.claimGoalTCPAttempt(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:manual.ID,
  ActorPrincipalID:GoalTCPRecurringWorkerPrincipal,RecurringGrantID:grant.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:manual.Revision,
 })
 if !errors.Is(err,ErrInvalidTransition){
  t.Fatalf("suspended source admitted a recurring claim in writer tx: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM project_goal_tcp_observation_fences
  WHERE goal_id=?`,g.ID);err==nil{
  t.Fatal("observer pause was deletable without audit")
 }
 restored,err:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
  ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
  TargetStatus:"running",ExpectedGoalRevision:g.Revision,
  ExpectedFenceRevision:paused.Revision,
 })
 if err!=nil||restored.Status!="running"||restored.Revision!=2||
  restored.ObservationAuthorized{
  t.Fatalf("resume incorrectly granted monitoring: %+v %v",restored,err)
 }
 // Bounded churn regression on lightweight hardware: rapid operator
 // pause/resume cycles must not cause a socket, activate monitoring,
 // spawn Tasks, lose CAS monotonicity or bypass the original cadence.
 // This is NOT a 24-hour endurance or physical Node acceptance test.
 for cycle:=0;cycle<64;cycle++{
  suspended,e:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
   ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
   TargetStatus:"paused",ExpectedGoalRevision:g.Revision,
   ExpectedFenceRevision:restored.Revision,
  })
  if e!=nil||suspended.Status!="paused"||suspended.ObservationAuthorized{
   t.Fatalf("bounded pause cycle %d: %+v %v",cycle,suspended,e)
  }
  if cycle%8==0{
   fresh:=NewService(db.SQL(),db,clock.Real{})
   none,e:=fresh.tickDueTCPRecurringProbes(ctx,1,fake)
   if e!=nil||len(none)!=0||fake.calls!=0{
    t.Fatalf("paused churn triggered TCP at %d: %+v %v",cycle,none,e)
   }
  }
  restored,e=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
   ProjectID:p.ID,GoalID:g.ID,ActorPrincipalID:"owner",
   TargetStatus:"running",ExpectedGoalRevision:g.Revision,
   ExpectedFenceRevision:suspended.Revision,
  })
  if e!=nil||restored.Status!="running"||
   restored.Revision!=int64(4+2*cycle)||restored.ObservationAuthorized{
   t.Fatalf("bounded resume cycle %d: %+v %v",cycle,restored,e)
  }
 }
 checked,err:=svc.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(checked)!=1||fake.calls!=1||
  fake.network!="tcp4"||fake.address!="192.168.9.23:443"||
  checked[0].Status!="connected"||
  checked[0].RequestedBy!=GoalTCPRecurringWorkerPrincipal||
  checked[0].RecurringGrantID==nil||*checked[0].RecurringGrantID!=grant.ID||
  checked[0].MonitoringActive{
  t.Fatalf("consented source not scoped or status misleading: %+v %v dialer=%+v",checked,err,fake)
 }
 checked,err=svc.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(checked)!=0||fake.calls!=1{
  t.Fatalf("same cadence re-probed source: %+v %v",checked,err)
 }
 restarted:=NewService(db.SQL(),db,clock.Real{})
 checked,err=restarted.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(checked)!=0||fake.calls!=1{
  t.Fatalf("restart lost rate fence: %+v %v",checked,err)
 }
 future:=NewService(db.SQL(),db,reviewPermitClock{ms:clock.Real{}.UnixMilli()+31*60*1000})
 checked,err=future.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(checked)!=1||fake.calls!=2{
  t.Fatalf("new cadence did not admit one approved source: %+v %v calls=%d",checked,err,fake.calls)
 }
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 _,err=svc.RevokeTCPRecurringGrant(ctx,RevokeTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:g.ID,GrantID:grant.ID,
  ExpectedGoalRevision:g.Revision,ExpectedGrantRevision:grant.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 further:=NewService(db.SQL(),db,reviewPermitClock{ms:clock.Real{}.UnixMilli()+45*60*1000})
 checked,err=further.tickDueTCPRecurringProbes(ctx,1,fake)
 if err!=nil||len(checked)!=0||fake.calls!=2{
  t.Fatalf("revocation failed to stop new recurring sockets: %+v %v",checked,err)
 }
 current,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||
  current.NextDueAt!=0||current.LastCheckedAt!=0||
  current.Status!="configured"{
  t.Fatalf("reachability monitoring falsely verified device health: %+v",current)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil||tasks!=0{
  t.Fatalf("bounded worker launched unrelated Tasks: %d %v",tasks,err)
 }
}

func TestRecurringTCPWorkerDeniesStaleGrantOrInactiveHumanBeforeSocket(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/recurring-revoke.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 if err:=svc.EnsureTCPRecurringWorkerPrincipal(ctx);err!=nil{t.Fatal(err)}
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Case",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"LAN",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"One service",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["service"]}`),
 })
 if err!=nil{t.Fatal(err)}
 expiry:=clock.Real{}.UnixMilli()+3600000
 manual,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"service",
  TargetIPv4:"10.9.8.7",TargetPort:8443,TimeoutMS:200,
  ExpiresAt:expiry,ExpectedGoalRevision:g.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 grant,err:=svc.ApproveTCPRecurringGrant(ctx,ApproveTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:g.ID,ManualPermitID:manual.ID,
  ExpectedGoalRevision:g.Revision,ExpectedManualPermitRevision:manual.Revision,
  ExpiresAt:expiry,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 candidate:=dueTCPRecurringGrant{
  projectID:p.ID,goalID:g.ID,grantID:grant.ID,manualPermitID:manual.ID,
  goalRevision:g.Revision,permitRevision:manual.Revision,
 }
 claim,err:=svc.claimGoalTCPAttempt(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:manual.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:manual.Revision,
  ActorPrincipalID:GoalTCPRecurringWorkerPrincipal,RecurringGrantID:grant.ID,
 })
 if err!=nil{t.Fatal(err)}
 if ok,err:=svc.tcpRecurringClaimStillAuthorised(ctx,claim,grant.ID);err!=nil||!ok{
  t.Fatalf("valid consent not admitted: %t %v",ok,err)
 }
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
   SET status='revoked' WHERE workspace_id='tenant' AND principal_id='owner'`)
 if err!=nil{t.Fatal(err)}
 if ok,err:=svc.tcpRecurringClaimStillAuthorised(ctx,claim,grant.ID);err!=nil||ok{
  t.Fatalf("revoked human source permission admitted: %t %v",ok,err)
 }
 dialer:=&fakeGoalTCPDialer{}
 if _,err:=svc.runRecurringGoalTCPProbe(ctx,candidate,dialer);
   !errors.Is(err,ErrPrincipalIneligible)&&!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("stale source accepted by worker: %v",err)
 }
 if dialer.calls!=0{t.Fatal("revoked source opened a socket")}
 cancelled,err:=svc.completeTCPAttempt(ctx,claim,"cancelled")
 if err!=nil||cancelled.Status!="cancelled"{
  t.Fatalf("cancelled claim not preserved: %+v %v",cancelled,err)
 }
}
