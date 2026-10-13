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

func TestTCPBaselineComparisonsAreVersionedReadOnlyAndNeverVerifyHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-baseline.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Host reachability",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"LAN",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,
  Objective:"Compare a manual TCP result to operator intent",
  ActorPrincipalID:"operator",MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.18.40",TargetPort:443,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 before,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,goal.ID,"router")
 if err!=nil||before.Assessment!="unconfigured_baseline"||
   before.DeviceHealth!="unverified"||before.MonitoringActive{
  t.Fatalf("no baseline was treated as verified: %+v %v",before,err)
 }
 baselineCmd:=SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedTCP:"connected",ExpectedGoalRevision:goal.Revision,
  ExpectedBaselineRevision:0,ActorPrincipalID:"operator",
 }
 fake:=baselineCmd;fake.ActorPrincipalID="robot"
 if _,err:=svc.SetGoalTCPBaseline(ctx,fake);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent authored security baseline: %v",err)
 }
 invalid:=baselineCmd;invalid.ExpectedTCP="healthy"
 if _,err:=svc.SetGoalTCPBaseline(ctx,invalid);!errors.Is(err,ErrInvalidCommand){
  t.Fatalf("baseline claimed device health: %v",err)
 }
 baseline,err:=svc.SetGoalTCPBaseline(ctx,baselineCmd)
 if err!=nil{t.Fatal(err)}
 if baseline.Revision!=1||baseline.ExpectedTCP!="connected"||baseline.PermitID!=permit.ID{
  t.Fatalf("baseline not bound to exact source: %+v",baseline)
 }
 if _,err:=svc.SetGoalTCPBaseline(ctx,baselineCmd);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale goal/baseline CAS accepted: %v",err)
 }
 missing,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,goal.ID,"router")
 if err!=nil||missing.Assessment!="missing_evidence"||missing.DeviceHealth!="unverified"{
  t.Fatalf("missing evidence treated as health: %+v %v",missing,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 result,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"operator",
 },&fakeGoalTCPDialer{})
 if err!=nil||result.Status!="connected"{t.Fatalf("fake test setup failed: %+v %v",result,err)}
 matching,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,goal.ID,"router")
 if err!=nil||matching.Assessment!="matches_last_tcp_test"||
    matching.ObservedTCP!="connected"||matching.SampleBaselineRevision!=1||
    matching.DeviceHealth!="unverified"||
    matching.MonitoringActive{
  t.Fatalf("TCP handshake comparison incorrectly verified security: %+v %v",matching,err)
 }
 change:=baselineCmd
 change.ExpectedGoalRevision=goal.Revision
 change.ExpectedBaselineRevision=baseline.Revision
 change.ExpectedTCP="not_connected"
 baseline2,err:=svc.SetGoalTCPBaseline(ctx,change)
 if err!=nil||baseline2.Revision!=2{
  t.Fatalf("revised baseline was not retained: %+v %v",baseline2,err)
 }
 mismatch,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,goal.ID,"router")
 if err!=nil||mismatch.Assessment!="baseline_updated_since_test"||
   mismatch.BaselineRevision!=2||mismatch.SampleBaselineRevision!=1||
   mismatch.DeviceHealth!="unverified"{
  t.Fatalf("new baseline retroactively reclassified an old TCP test: %+v %v",mismatch,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_tcp_probe_attempts
    SET baseline_revision_at_claim=999 WHERE id=?`,result.ID);err==nil{
  t.Fatal("TCP attempt baseline provenance could be rewritten")
 }
 revisions,err:=svc.ListGoalTCPBaselines(ctx,project.ID,goal.ID)
 if err!=nil||len(revisions)!=2||revisions[0].ID==revisions[1].ID{
  t.Fatalf("immutable baseline history missing: %+v %v",revisions,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_tcp_baseline_revisions
    SET expected_tcp='connected' WHERE id=?`,baseline2.ID);err==nil{
  t.Fatal("a prior user-approved baseline was rewritten")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if _,err:=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"operator",
 });err!=nil{t.Fatal(err)}
 inactive,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,goal.ID,"router")
 if err!=nil||inactive.Assessment!="permission_inactive"{
  t.Fatalf("expired/revoked probe still proved compliance: %+v %v",inactive,err)
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||
   current.NextDueAt!=0||current.LastCheckedAt!=0||
   current.Status!="configured"{
  t.Fatalf("TCP baseline made false Goal health claims: %+v",current)
 }
 var taskCount int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&taskCount);err!=nil||taskCount!=0{
  t.Fatalf("baseline unexpectedly queued work: %d %v",taskCount,err)
 }

 // The exact same monitoring plan JSON must not restore an old human
 // permit, its baseline, or its previously matching TCP measurement.
 restoredGoal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,
  Objective:"Generation-fenced TCP baseline",ActorPrincipalID:"operator",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["restore-test"]}`),
 })
 if err!=nil{t.Fatal(err)}
 restoredPermit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:restoredGoal.ID,SourceID:"restore-test",
  TargetIPv4:"192.168.18.41",TargetPort:443,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,
  ExpectedGoalRevision:restoredGoal.Revision,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 restoredGoal,err=svc.ContinuousGoal(ctx,restoredGoal.ID)
 if err!=nil{t.Fatal(err)}
 restoredBaseline,err:=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:restoredGoal.ID,PermitID:restoredPermit.ID,
  ExpectedTCP:"connected",ExpectedGoalRevision:restoredGoal.Revision,
  ExpectedBaselineRevision:0,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 restoredGoal,err=svc.ContinuousGoal(ctx,restoredGoal.ID)
 if err!=nil{t.Fatal(err)}
 restoredSample,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:restoredGoal.ID,PermitID:restoredPermit.ID,
  ExpectedGoalRevision:restoredGoal.Revision,ExpectedPermitRevision:restoredPermit.Revision,
  ActorPrincipalID:"operator",
 },&fakeGoalTCPDialer{})
 if err!=nil||restoredSample.Status!="connected"{
  t.Fatalf("synthetic TCP baseline setup failed: %+v %v",restoredSample,err)
 }
 beforeRestore,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,restoredGoal.ID,"restore-test")
 if err!=nil||beforeRestore.Assessment!="matches_last_tcp_test"{
  t.Fatalf("initial synthetic baseline did not match: %+v %v",beforeRestore,err)
 }
 var originalPlan string
 var originalGeneration int64
 if err=db.SQL().QueryRowContext(ctx,`SELECT monitoring_plan_json,plan_generation
   FROM project_continuous_goals WHERE id=?`,restoredGoal.ID).
   Scan(&originalPlan,&originalGeneration);err!=nil{t.Fatal(err)}
 if restoredPermit.PlanGeneration!=originalGeneration{t.Fatal("approved TCP permit generation mismatch")}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
   SET monitoring_plan_json=? WHERE id=?`,
   `{"version":1,"source_ids":["restore-test","other-source"]}`,restoredGoal.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
   SET monitoring_plan_json=? WHERE id=?`,originalPlan,restoredGoal.ID);err!=nil{t.Fatal(err)}
 var advancedGeneration int64
 if err=db.SQL().QueryRowContext(ctx,`SELECT plan_generation
   FROM project_continuous_goals WHERE id=?`,restoredGoal.ID).
   Scan(&advancedGeneration);err!=nil{t.Fatal(err)}
 if advancedGeneration!=originalGeneration+2{
  t.Fatalf("monitoring plan rollback did not advance generation: %d",advancedGeneration)
 }
 afterRestore,err:=svc.GoalTCPReachabilityAssessment(ctx,project.ID,restoredGoal.ID,"restore-test")
 if err!=nil||afterRestore.Assessment!="permission_inactive"||
   afterRestore.DeviceHealth!="unverified"||afterRestore.MonitoringActive{
  t.Fatalf("historical TCP baseline was reactivated: %+v %v",afterRestore,err)
 }
 restoredGoal,err=svc.ContinuousGoal(ctx,restoredGoal.ID)
 if err!=nil{t.Fatal(err)}
 _,err=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:restoredGoal.ID,PermitID:restoredPermit.ID,
  ExpectedTCP:"not_connected",ExpectedGoalRevision:restoredGoal.Revision,
  ExpectedBaselineRevision:restoredBaseline.Revision,ActorPrincipalID:"operator",
 })
 if !errors.Is(err,ErrInvalidTransition){
  t.Fatalf("stale human approval could revise a baseline: %v",err)
 }

}
