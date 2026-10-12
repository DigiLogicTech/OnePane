//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// This test protects reporting semantics as the real recurring worker is
// introduced. Network permission is not verified endpoint security.
func TestGoalCoverageSeparatesRecurringTCPAuthorizationFromVerifiedHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-permission-coverage.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 service:=NewService(db.SQL(),db,clock.Real{})
 project,err:=service.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Observed reachability",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=service.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"LAN",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=service.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:w.ID,ActorPrincipalID:"owner",
  Objective:"Classify one TCP source without health inflation",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 coverage,err:=service.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil||len(coverage.Sources)!=1||coverage.RecurringTCPAuthorizedSources!=0||
   coverage.Sources[0].TCPRecurringAuthorized||coverage.MonitoringActive{
  t.Fatalf("unapproved recurring rights reported: %+v %v",coverage,err)
 }
 expiry:=clock.Real{}.UnixMilli()+60*60*1000
 manual,err:=service.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.10.31",TargetPort:443,TimeoutMS:450,
  ExpiresAt:expiry,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 coverage,err=service.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil||coverage.RecurringTCPAuthorizedSources!=0||
  coverage.Sources[0].TCPRecurringAuthorized{
  t.Fatalf("one-shot human grant silently became recurring authority: %+v %v",coverage,err)
 }
 goal,err=service.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 grant,err:=service.ApproveTCPRecurringGrant(ctx,ApproveTCPRecurringGrantCommand{
  ProjectID:project.ID,GoalID:goal.ID,ManualPermitID:manual.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedManualPermitRevision:manual.Revision,
  ExpiresAt:expiry,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 coverage,err=service.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil||coverage.RecurringTCPAuthorizedSources!=1||
   !coverage.Sources[0].TCPRecurringAuthorized||
   coverage.Sources[0].EvidenceState!="missing"||
   coverage.GoalHealth!="unobserved"||coverage.MonitoringActive{
  t.Fatalf("recurring permission conflated with observed endpoint health: %+v %v",coverage,err)
 }
 // Removing the consenting person's active tenant membership makes an
 // unexpired authorization ineffective without requiring a new probe.
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships SET status='revoked'
  WHERE workspace_id='tenant' AND principal_id='owner'`)
 if err!=nil{t.Fatal(err)}
 coverage,err=service.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil||coverage.RecurringTCPAuthorizedSources!=0||
   coverage.Sources[0].TCPRecurringAuthorized{
  t.Fatalf("inactive human consent was still reported active: %+v %v",coverage,err)
 }
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships SET status='active'
  WHERE workspace_id='tenant' AND principal_id='owner'`)
 if err!=nil{t.Fatal(err)}
 goal,err=service.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 _,err=service.RevokeTCPRecurringGrant(ctx,RevokeTCPRecurringGrantCommand{
  ProjectID:project.ID,GoalID:goal.ID,GrantID:grant.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedGrantRevision:grant.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 coverage,err=service.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil||coverage.RecurringTCPAuthorizedSources!=0||
   coverage.Sources[0].TCPRecurringAuthorized{
  t.Fatalf("revoked recurring consent presented as authorized: %+v %v",coverage,err)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&attempts);err!=nil||attempts!=0{
  t.Fatalf("coverage projection opened network sockets: %d %v",attempts,err)
 }
}
