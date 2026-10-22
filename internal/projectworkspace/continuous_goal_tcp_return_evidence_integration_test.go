//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type mutableAlertTestClock struct{ now time.Time }
func (c *mutableAlertTestClock) Now() time.Time { return c.now }
func (c *mutableAlertTestClock) UnixMilli() int64 { return c.now.UnixMilli() }

func TestTCPAlertCorrelatesLaterBaselineReturnWithoutClaimingDeviceHealth(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/return-to-baseline.db")
 if err!=nil { t.Fatal(err) }
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil { t.Fatal(err) }
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 } { if _,err:=db.SQL().ExecContext(ctx,q);err!=nil { t.Fatal(err) } }
 clk:=&mutableAlertTestClock{now:time.Now().UTC().Truncate(time.Second)}
 svc:=NewService(db.SQL(),db,clk)
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Return to baseline",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil {t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Local",ActorPrincipalID:"human",
 })
 if err!=nil {t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Track historical TCP deviation and return",
  CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil {t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.1.15",TargetPort:8443,TimeoutMS:300,
  ExpiresAt:clk.UnixMilli()+2*60*60*1000,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"human",
 })
 if err!=nil {t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil {t.Fatal(err)}
 _,err=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedTCP:"not_connected",
  ExpectedBaselineRevision:0,ExpectedGoalRevision:goal.Revision,
 })
 if err!=nil {t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil {t.Fatal(err)}
 cmd:=RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:permit.Revision,
 }
 // The first authorised observation deviates from the TCP baseline.
 unexpected,err:=svc.runManualGoalTCPProbe(ctx,cmd,&fakeGoalTCPDialer{})
 if err!=nil||unexpected.Status!="connected"{t.Fatalf("drift: %+v %v",unexpected,err)}
 alerts,err:=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(alerts)!=1 {t.Fatalf("expected one drift notice: %+v %v",alerts,err)}
 if alerts[0].ReturnToBaselineEvidenceState!="not_observed"||
  alerts[0].ReturnToBaselineAttemptID!=""||
  alerts[0].DeviceHealth!="unverified" {
  t.Fatalf("fabricated return observation: %+v",alerts[0])
 }
 // A later (different permitted cadence window) negative socket result
 // equals the operator-approved baseline. It is not a device-security test.
 clk.now=clk.now.Add(31*time.Minute)
 returned,err:=svc.runManualGoalTCPProbe(ctx,cmd,
  &fakeGoalTCPDialer{fail:errors.New("synthetic TCP connection refused")})
 if err!=nil||returned.Status!="not_connected"{
  t.Fatalf("baseline TCP observation: %+v %v",returned,err)
 }
 alerts,err=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(alerts)!=1{t.Fatalf("alert history changed: %+v %v",alerts,err)}
 a:=alerts[0]
 if a.ReturnToBaselineEvidenceState!="historical_tcp_return_observed"||
  a.ReturnToBaselineAttemptID!=returned.ID||
  a.ReturnToBaselineAt!=returned.CompletedAt||
  a.Status!="open"||a.DeviceHealth!="unverified"{
  t.Fatalf("return evidence not bounded to original alert: %+v",a)
 }
 if a.ReturnToBaselineAt<=a.DetectedAt{t.Fatalf("nonchronological return: %+v",a)}
 if _,err:=svc.ListTCPReachabilityAlerts(ctx,"other",goal.ID);err==nil {
  t.Fatal("cross-Project read unexpectedly accepted")
 }
 latest,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil {t.Fatal(err)}
 if latest.Health!="unobserved"||latest.LastVerifiedAt!=0||latest.NextDueAt!=0 {
  t.Fatalf("TCP return incorrectly activated protection: %+v",latest)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil||tasks!=0{
  t.Fatalf("reading correlation created Tasks: %d %v",tasks,err)
 }
}
