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

func TestTCPReachabilityDriftNoticeIsEvidenceBoundHumanAckOnly(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-drift-alerts.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Reachability drift",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Local",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Unexpected reachability notice",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`)})
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.1.15",TargetPort:8443,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+3600000,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 baseline,err:=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedTCP:"not_connected",
  ExpectedBaselineRevision:0,ExpectedGoalRevision:goal.Revision,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 initial,err:=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(initial)!=0{t.Fatalf("no test yet but alert exists: %+v %v",initial,err)}
 attempt,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{})
 if err!=nil||attempt.Status!="connected"||
  attempt.BaselineIDAtClaim!=baseline.ID{
  t.Fatalf("test evidence failed to retain baseline: %+v %v",attempt,err)
 }
 alerts,err:=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(alerts)!=1{t.Fatalf("mismatch not recorded once: %+v %v",alerts,err)}
 a:=alerts[0]
 if a.SourceID!="router"||a.AttemptID!=attempt.ID||
  a.BaselineID!=baseline.ID||a.BaselineRevision!=1||
  a.ExpectedTCP!="not_connected"||a.ObservedTCP!="connected"||
  a.Classification!="unexpected_reachable"||
  a.Status!="open"||a.DeviceHealth!="unverified"{
  t.Fatalf("false or unscoped TCP warning: %+v",a)
 }
 if _,err:=svc.ListTCPReachabilityAlerts(ctx,"other",goal.ID);err==nil{
  t.Fatal("foreign Project listed drift history")
 }
 robot:=AcknowledgeTCPReachabilityAlertCommand{
  ProjectID:project.ID,GoalID:goal.ID,AlertID:a.ID,
  ExpectedRevision:1,ActorPrincipalID:"robot"}
 if _,err:=svc.AcknowledgeTCPReachabilityAlert(ctx,robot);
  !errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent dismissed network warning: %v",err)
 }
 robot.ActorPrincipalID="human"
 ack,err:=svc.AcknowledgeTCPReachabilityAlert(ctx,robot)
 if err!=nil||ack.Status!="acknowledged"||ack.Revision!=2||
  ack.AcknowledgedBy==nil||*ack.AcknowledgedBy!="human"{
  t.Fatalf("human acknowledgement not retained: %+v %v",ack,err)
 }
 if _,err:=svc.AcknowledgeTCPReachabilityAlert(ctx,robot);
  !errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale acknowledgement replay admitted: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_tcp_reachability_alerts
   SET observed_tcp='not_connected' WHERE id=?`,a.ID);err==nil{
  t.Fatal("alert evidence could be rewritten")
 }
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM project_goal_tcp_reachability_alerts
   WHERE id=?`,a.ID);err==nil{
  t.Fatal("alert history could be deleted")
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||
   current.NextDueAt!=0||current.Status!="configured"{
  t.Fatalf("reachability alert falsely verified device or activated remediation: %+v",current)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil||tasks!=0{
  t.Fatalf("reachability alert ran arbitrary Tasks: %d %v",tasks,err)
 }
}
