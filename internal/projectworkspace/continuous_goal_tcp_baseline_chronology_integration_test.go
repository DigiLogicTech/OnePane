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

// A baseline created AFTER a handshake cannot classify that earlier
// handshake, even if their wall-clock times happened to share one millisec.
func TestTCPBaselineDoesNotRetroactivelyClaimEarlierSocketEvidence(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/baseline-revision-claim.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Baseline chronology",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Scope",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Evidence chronology",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`)})
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"dns",
  TargetIPv4:"192.168.7.8",TargetPort:53,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+3600000,
  ExpectedGoalRevision:g.Revision,ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 first,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{})
 if err!=nil||first.Status!="connected"||
   first.BaselineRevisionAtClaim!=0||first.BaselineIDAtClaim!=""{
  t.Fatalf("socket before baseline was retroactively tagged: %+v %v",first,err)
 }
 _,err=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  ExpectedBaselineRevision:0,ExpectedTCP:"connected"})
 if err!=nil{t.Fatal(err)}
 assessed,err:=svc.GoalTCPReachabilityAssessment(ctx,p.ID,g.ID,"dns")
 if err!=nil||assessed.Assessment!="no_baseline_at_test"||
    assessed.BaselineRevision!=1||assessed.SampleBaselineRevision!=0||
    assessed.DeviceHealth!="unverified"{
  t.Fatalf("later human baseline accepted earlier evidence: %+v %v",assessed,err)
 }
 current,err:=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 if _,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:current.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{});!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("tried to repeat TCP test within source cadence: %v",err)
 }
 future:=NewService(db.SQL(),db,reviewPermitClock{ms:clock.Real{}.UnixMilli()+31*60000})
 second,err:=future.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:current.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{})
 if err!=nil||second.Status!="connected"||second.BaselineRevisionAtClaim!=1||
    second.BaselineIDAtClaim==""{
  t.Fatalf("new TCP sample failed to bind approved baseline: %+v %v",second,err)
 }
 fresh,err:=future.GoalTCPReachabilityAssessment(ctx,p.ID,g.ID,"dns")
 if err!=nil||fresh.Assessment!="matches_last_tcp_test"||
    fresh.SampleBaselineRevision!=1||fresh.DeviceHealth!="unverified"{
  t.Fatalf("current baseline not matched by newer bounded evidence: %+v %v",fresh,err)
 }
}
