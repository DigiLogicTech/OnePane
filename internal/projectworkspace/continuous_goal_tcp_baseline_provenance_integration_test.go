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

// An operator's new or edited reachability expectation MUST NOT assign
// verdicts to a TCP test that ran before that human decision. A matching
// verdict requires a fresh, finalised test admitted under the exact same
// immutable baseline version.
func TestTCPBaselineRequiresFreshPostDecisionEvidence(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/baseline-redecision.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
     VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
     VALUES('human','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
     VALUES('tenant','human','active',1,1)`,
 } {if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"TCP baseline provenance",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Local test",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:w.ID,ActorPrincipalID:"human",
  Objective:"Compare only tests admitted under chosen baseline",
  CadenceMinutes:5,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"dns",
  TargetIPv4:"192.168.50.53",TargetPort:53,TimeoutMS:300,
  ExpiresAt:now+55*60*1000,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 latestGoal:=func()*ContinuousGoal{
  t.Helper()
  g,e:=svc.ContinuousGoal(ctx,goal.ID)
  if e!=nil{t.Fatal(e)}
  return &g
 }
 check:=func(runner *Service,dialer *fakeGoalTCPDialer)GoalTCPProbeAttempt{
  t.Helper()
  result,e:=runner.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
   ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
   ExpectedGoalRevision:latestGoal().Revision,
   ExpectedPermitRevision:permit.Revision,
   ActorPrincipalID:"human",
  },dialer)
  if e!=nil{t.Fatal(e)}
  return result
 }
 // First test is intentionally before a human has defined any expectation.
 early:=check(svc,&fakeGoalTCPDialer{})
 if early.BaselineRevisionAtClaim!=0||early.BaselineIDAtClaim!=""{
  t.Fatalf("unconfigured expectation was invented during test: %+v",early)
 }
 v1,err:=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedTCP:"connected",ExpectedGoalRevision:latestGoal().Revision,
  ExpectedBaselineRevision:0,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 earlyAssessment,err:=svc.GoalTCPReachabilityAssessment(ctx,p.ID,goal.ID,"dns")
 if err!=nil||earlyAssessment.Assessment!="no_baseline_at_test"||
  earlyAssessment.BaselineRevision!=1||
  earlyAssessment.SampleBaselineRevision!=0||
  earlyAssessment.DeviceHealth!="unverified"{
  t.Fatalf("new baseline retroactively claimed historical test: %+v %v",earlyAssessment,err)
 }
 // More than one five-minute window passes, allowing exactly one fresh TCP
 // attempt under baseline revision one; there is no real socket in tests.
 atV1:=NewService(db.SQL(),db,reviewPermitClock{ms:now+6*60*1000})
 first:=check(atV1,&fakeGoalTCPDialer{})
 if first.BaselineRevisionAtClaim!=1||first.BaselineIDAtClaim!=v1.ID{
  t.Fatalf("test did not snapshot its approved baseline: %+v",first)
 }
 matching,err:=atV1.GoalTCPReachabilityAssessment(ctx,p.ID,goal.ID,"dns")
 if err!=nil||matching.Assessment!="matches_last_tcp_test"||
  matching.SampleBaselineRevision!=1{
  t.Fatalf("fresh matching handshake not correctly compared: %+v %v",matching,err)
 }
 // A later human preference changes, but the most recent test still
 // belongs to v1, not v2. The correct result is neither match nor drift.
 v2,err:=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedTCP:"not_connected",ExpectedGoalRevision:latestGoal().Revision,
  ExpectedBaselineRevision:1,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 outdated,err:=atV1.GoalTCPReachabilityAssessment(ctx,p.ID,goal.ID,"dns")
 if err!=nil||outdated.Assessment!="baseline_updated_since_test"||
  outdated.BaselineRevision!=2||outdated.SampleBaselineRevision!=1{
  t.Fatalf("new desired state was applied to older measurement: %+v %v",outdated,err)
 }
 // A new result admitted under v2 can be compared. It is STILL NOT proof
 // that the service/device is secure; only TCP reachability is evaluated.
 atV2:=NewService(db.SQL(),db,reviewPermitClock{ms:now+12*60*1000})
 down:=&fakeGoalTCPDialer{fail:errors.New("connection refused")}
 second:=check(atV2,down)
 if second.Status!="not_connected"||second.BaselineRevisionAtClaim!=2||
  second.BaselineIDAtClaim!=v2.ID||down.calls!=1{
  t.Fatalf("new expectation did not bind fresh network sample: %+v calls=%d",second,down.calls)
 }
 resolved,err:=atV2.GoalTCPReachabilityAssessment(ctx,p.ID,goal.ID,"dns")
 if err!=nil||resolved.Assessment!="matches_last_tcp_test"||
  resolved.BaselineRevision!=2||resolved.SampleBaselineRevision!=2||
  resolved.ObservedTCP!="not_connected"||resolved.DeviceHealth!="unverified"||
  resolved.MonitoringActive{
  t.Fatalf("fresh expected TCP failure misreported: %+v %v",resolved,err)
 }
 // Snapshot provenance survives restart and cannot be edited on finalised
 // attempts, so an old verdict can never be retrospectively rewritten.
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE project_goal_tcp_probe_attempts SET baseline_id_at_claim=? WHERE id=?`,
  v2.ID,first.ID);err==nil{
  t.Fatal("immutable baseline provenance could be rewritten")
 }
 current:=latestGoal()
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||
  current.Status!="configured"{
  t.Fatalf("comparison changed verified goal health: %+v",current)
 }
}
