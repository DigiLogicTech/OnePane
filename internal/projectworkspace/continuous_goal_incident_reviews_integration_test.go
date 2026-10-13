//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestTCPIncidentCaseIsOperatorReviewedNotAutonomousRemediation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/incident-review.db")
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
  WorkspaceID:"tenant",Name:"Operator cases",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Local",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,
  ActorPrincipalID:"human",Objective:"Investigate a TCP deviation",
  CadenceMinutes:30,MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.1.15",TargetPort:8443,TimeoutMS:300,
  ExpiresAt:time.Now().Add(2*time.Hour).UnixMilli(),
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 _,err=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedTCP:"not_connected",
  ExpectedBaselineRevision:0,ExpectedGoalRevision:goal.Revision,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 _,err=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{})
 if err!=nil{t.Fatal(err)}
 alerts,err:=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(alerts)!=1{t.Fatalf("drift fixture: %+v %v",alerts,err)}
 open:=OpenGoalIncidentReviewCommand{
  ProjectID:project.ID,GoalID:goal.ID,TCPAlertID:alerts[0].ID,
  ExpectedAlertRevision:alerts[0].Revision,ActorPrincipalID:"robot",
 }
 if _,err:=svc.OpenGoalIncidentReview(ctx,open);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent created operator case: %v",err)
 }
 open.ActorPrincipalID="human"
 if _,err:=svc.OpenGoalIncidentReview(ctx,OpenGoalIncidentReviewCommand{
  ProjectID:"foreign",GoalID:goal.ID,TCPAlertID:alerts[0].ID,
  ExpectedAlertRevision:alerts[0].Revision,ActorPrincipalID:"human",
 });err==nil{t.Fatal("foreign Project admitted")}
 review,err:=svc.OpenGoalIncidentReview(ctx,open)
 if err!=nil||review.Status!="pending_review"||review.Decision!="unassessed"||
  review.DeviceHealth!="unverified"||review.RemediationAuthorized{
  t.Fatalf("review improperly opened: %+v %v",review,err)
 }
 if _,err:=svc.OpenGoalIncidentReview(ctx,open);err==nil{
  t.Fatal("one alert produced duplicate operator cases")
 }
 listed,err:=svc.ListGoalIncidentReviews(ctx,project.ID,goal.ID)
 if err!=nil||len(listed)!=1||listed[0].ID!=review.ID{
  t.Fatalf("case not persisted: %+v %v",listed,err)
 }
 if _,err:=svc.ListGoalIncidentReviews(ctx,"foreign",goal.ID);err==nil{
  t.Fatal("foreign Project exposed operator case")
 }
 evidence,err:=svc.GoalIncidentEvidence(ctx,project.ID,goal.ID,review.ID)
 if err!=nil||evidence.Alert.ID!=alerts[0].ID||
  evidence.Alert.ReturnToBaselineEvidenceState!="not_observed"||
  evidence.EndpointAssociation!="not_established"||
  len(evidence.SignedEndpointClaims)!=0||
  evidence.IncidentConfirmed||evidence.DeviceIdentityVerified||
  evidence.RemediationAuthorized{
  t.Fatalf("historical case evidence incorrectly trusted: %+v %v",evidence,err)
 }
 if _,err:=svc.GoalIncidentEvidence(ctx,"foreign",goal.ID,review.ID);err==nil{
  t.Fatal("cross-Project incident evidence was exposed")
 }
 decision:=CompleteGoalIncidentReviewCommand{
  ProjectID:project.ID,GoalID:goal.ID,ReviewID:review.ID,
  ExpectedRevision:review.Revision,ActorPrincipalID:"robot",
  Decision:"needs_investigation",
 }
 if _,err:=svc.CompleteGoalIncidentReview(ctx,decision);
  !errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent reviewed case: %v",err)
 }
 decision.ActorPrincipalID="human"
 if _,err:=svc.CompleteGoalIncidentReview(ctx,CompleteGoalIncidentReviewCommand{
  ProjectID:project.ID,GoalID:goal.ID,ReviewID:review.ID,
  ExpectedRevision:review.Revision,ActorPrincipalID:"human",
  Decision:"device_secure",
 });!errors.Is(err,ErrInvalidCommand){
  t.Fatalf("invalid health claim accepted: %v",err)
 }
 done,err:=svc.CompleteGoalIncidentReview(ctx,decision)
 if err!=nil||done.Status!="reviewed"||done.Decision!="needs_investigation"||
  done.Revision!=review.Revision+1||done.DeviceHealth!="unverified"{
  t.Fatalf("human classification failed: %+v %v",done,err)
 }
 if _,err:=svc.CompleteGoalIncidentReview(ctx,decision);
  !errors.Is(err,ErrRevisionConflict){t.Fatalf("stale review retry: %v",err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_incident_reviews
  SET tcp_alert_id='swapped' WHERE id=?`,review.ID);err==nil{
  t.Fatal("incident provenance was mutable")
 }
 a,err:=svc.ListTCPReachabilityAlerts(ctx,project.ID,goal.ID)
 if err!=nil||len(a)!=1||a[0].Status!="open"{
  t.Fatalf("human review changed original alert: %+v %v",a,err)
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("case falsely activated health monitoring: %+v",current)
 }
 // Response preparation is a narrowly scoped human-only intent, never
 // authorisation to restart a process or change a network/device.
 g,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 propose:=ProposeGoalResponseCandidateCommand{
  ProjectID:project.ID,GoalID:goal.ID,IncidentReviewID:review.ID,
  ActorPrincipalID:"robot",ActionCode:"restart_local_observation_worker",
  ExpectedGoalRevision:g.Revision,ExpectedReviewRevision:done.Revision,
  ExpiresAt:time.Now().Add(time.Hour).UnixMilli(),
 }
 if _,err:=svc.ProposeGoalResponseCandidate(ctx,propose);
  !errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent proposed host mutation: %v",err)
 }
 propose.ActorPrincipalID="human"
 propose.ActionCode="arbitrary_router_reconfiguration"
 if _,err:=svc.ProposeGoalResponseCandidate(ctx,propose);
  !errors.Is(err,ErrInvalidCommand){
  t.Fatalf("unapproved response action accepted: %v",err)
 }
 propose.ActionCode="restart_local_observation_worker"
 propose.ExpectedReviewRevision=review.Revision
 if _,err:=svc.ProposeGoalResponseCandidate(ctx,propose);
  !errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale incident authority accepted: %v",err)
 }
 propose.ExpectedReviewRevision=done.Revision
 candidate,err:=svc.ProposeGoalResponseCandidate(ctx,propose)
 if err!=nil||candidate.ExecutionAuthorized||
  candidate.Status!="proposed"||candidate.RiskClass!="requires_separate_approval"{
  t.Fatalf("response intent assumed execution right: %+v %v",candidate,err)
 }
 readiness,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil||readiness.ExecutionAuthorized||readiness.State!="blocked"||
  len(readiness.Blockers)<3{
  t.Fatalf("response readiness was not fail-closed: %+v %v",readiness,err)
 }
 if _,err:=svc.ProposeGoalResponseCandidate(ctx,propose);err==nil{
  t.Fatal("duplicate pending response intent admitted")
 }
 if _,err:=svc.ListGoalResponseCandidates(ctx,"foreign",goal.ID);err==nil{
  t.Fatal("cross Project response intent listed")
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_response_candidates
    SET execution_authorized=1 WHERE id=?`,candidate.ID);err==nil{
  t.Fatal("direct database mutation granted response execution")
 }
 withdraw:=WithdrawGoalResponseCandidateCommand{
  ProjectID:project.ID,GoalID:goal.ID,CandidateID:candidate.ID,
  ActorPrincipalID:"robot",ExpectedRevision:candidate.Revision,
 }
 if _,err:=svc.WithdrawGoalResponseCandidate(ctx,withdraw);
  !errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent withdrew response intent: %v",err)
 }
 withdraw.ActorPrincipalID="human"
 withdrawn,err:=svc.WithdrawGoalResponseCandidate(ctx,withdraw)
 if err!=nil||withdrawn.Status!="withdrawn"||withdrawn.Revision!=2{
  t.Fatalf("human withdrawal failed: %+v %v",withdrawn,err)
 }
 if _,err:=svc.WithdrawGoalResponseCandidate(ctx,withdraw);
  !errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale withdrawal replay accepted: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`DELETE FROM project_goal_response_candidates
    WHERE id=?`,candidate.ID);err==nil{
  t.Fatal("response history could be deleted")
 }
 after,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil||after.ExecutionAuthorized||len(after.Blockers)<4{
  t.Fatalf("withdrawn candidate appeared executable: %+v %v",after,err)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);
  err!=nil||tasks!=0{t.Fatalf("review created Tasks: %d %v",tasks,err)}
}
