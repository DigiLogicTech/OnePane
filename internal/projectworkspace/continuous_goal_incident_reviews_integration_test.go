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
 // Readiness must surface the operator's durable stop latch. The
 // response candidate itself remains permanently non-executable.
 latestGoal,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 pausedFence,err:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
  ProjectID:project.ID,GoalID:goal.ID,ActorPrincipalID:"human",
  TargetStatus:"paused",ExpectedGoalRevision:latestGoal.Revision,
  ExpectedFenceRevision:0,
 })
 if err!=nil{t.Fatal(err)}
 pausedReadiness,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 pausedReason:=false
 for _,reason:=range pausedReadiness.Blockers{
  if reason=="observer_pause_active"{pausedReason=true}
 }
 if !pausedReason||pausedReadiness.ExecutionAuthorized||pausedReadiness.State!="blocked"{
  t.Fatalf("operator observer pause omitted from response assessment: %+v",pausedReadiness)
 }
 resumedFence,err:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
  ProjectID:project.ID,GoalID:goal.ID,ActorPrincipalID:"human",
  TargetStatus:"running",ExpectedGoalRevision:latestGoal.Revision,
  ExpectedFenceRevision:pausedFence.Revision,
 })
 if err!=nil||resumedFence.Status!="running"{t.Fatalf("resume latch: %+v %v",resumedFence,err)}
 resumedReadiness,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 for _,reason:=range resumedReadiness.Blockers{
  if reason=="observer_pause_active"{t.Fatalf("stale pause diagnosis persisted after resume: %+v",resumedReadiness)}
 }
 if resumedReadiness.ExecutionAuthorized{t.Fatal("resume latch granted response execution")}
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
 // Even a formerly valid historical response intent cannot mask the
 // human revocation of the original network observation permit.
 g,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 _,err=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 revokedReadiness,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 foundRevocation:=false
 for _,reason:=range revokedReadiness.Blockers{
  if reason=="original_observation_permission_inactive"{foundRevocation=true}
 }
 if !foundRevocation||revokedReadiness.ExecutionAuthorized{
  t.Fatalf("revoked source authority survived as candidate approval: %+v",revokedReadiness)
 }
 // Withdrawn status is immutable history. A human reviewer whose
 // tenant membership is suspended cannot remain a *current* reviewer.
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE workspace_memberships SET status='suspended',updated_at=updated_at+1
   WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 staleReviewer,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 inactiveReview:=false
 for _,reason:=range staleReviewer.Blockers{
  if reason=="incident_reviewer_inactive"{inactiveReview=true}
 }
 if !inactiveReview||staleReviewer.ExecutionAuthorized{
  t.Fatalf("membership suspension preserved live incident reviewer endorsement: %+v",staleReviewer)
 }
 if _,err:=db.SQL().ExecContext(ctx,
  `UPDATE workspace_memberships SET status='active',updated_at=updated_at+1
   WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 restoredReviewer,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 for _,reason:=range restoredReviewer.Blockers{
  if reason=="incident_reviewer_inactive"{
   t.Fatalf("reinstated human reviewer remained marked inactive: %+v",restoredReviewer)
  }
 }
 if restoredReviewer.ExecutionAuthorized{t.Fatal("membership reinstatement granted execution")}
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);
  err!=nil||tasks!=0{t.Fatalf("review created Tasks: %d %v",tasks,err)}
 // Even in the same tenant, a different Project's Task must not
 // pollute this exact Goal's reconciliation diagnosis. Deliberately use
 // the target Goal's resource string as hostile/corrupt external metadata;
 // the canonical Task's Project/Workspace must still win.
 otherProject,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Other isolated observer",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 otherWorkspace,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:otherProject.ID,Name:"Other sandbox",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 foreignTask:="different-project-observer"
 nowForeign:=time.Now().UnixMilli()
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO tasks(
  id,workspace_id,project_id,project_workspace_id,objective,state,
  scheduling_class,priority,completion_json,revision,created_at,updated_at)
  VALUES(?,?,?,?,?,'running','normal_task',0,'{}',1,?,?)`,
  foreignTask,"tenant",otherProject.ID,otherWorkspace.ID,"Foreign evidence",nowForeign,nowForeign)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO operations(
   id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,
   resource_ref,tool_id,tool_version,adapter_id,adapter_version,
   desired_state_json,precondition_json,reconciliation_json,
   policy_revision,input_hash,revision,created_at,updated_at)
   VALUES(?,?,?,?,?,?,?, ?,?,'1',?,'1','{}','{}','{}',1,'testhash',1,?,?)`,
   "foreign-uncertain","tenant",foreignTask,"human","foreign-unknown-unique",
   "unknown_outcome","goal.tcp_observer.control",
   "goal-tcp-observer://"+project.ID+"/"+goal.ID,
   "project.goal.tcp_observer.pause","onepane.goal.tcp_observer.control",
   nowForeign,nowForeign)
 if err!=nil{t.Fatal(err)}
 isolated,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 for _,reason:=range isolated.Blockers{
  if reason=="observer_mutation_needs_reconciliation"||
   reason=="observer_mutation_in_flight"{
   t.Fatalf("another Project's observer contaminated exact Goal readiness: %+v",isolated)
  }
 }
 // A pre-existing, human-scoped observer effect with an unknown outcome
 // requires reconciliation; another executing operation is not a verified
 // postcondition. These synthetic durable rows never invoke an adapter,
 // socket, Tool Gateway or autonomous response.
 taskID:="human-recovery-inspection"
 now:=time.Now().UnixMilli()
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO tasks(
  id,workspace_id,project_id,project_workspace_id,objective,state,
  scheduling_class,priority,completion_json,revision,created_at,updated_at)
  VALUES(?,?,?,?,?,'running','normal_task',0,'{}',1,?,?)`,
  taskID,"tenant",project.ID,ws.ID,"Human reconciliation evidence",now,now)
 if err!=nil{t.Fatal(err)}
 for _,fixture:=range []struct{id,state string}{
  {"effect-unknown","unknown_outcome"},
  {"effect-executing","executing"},
 }{
  resource:="goal-tcp-observer://"+project.ID+"/"+goal.ID
  _,err=db.SQL().ExecContext(ctx,`INSERT INTO operations(
   id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,
   resource_ref,tool_id,tool_version,adapter_id,adapter_version,
   desired_state_json,precondition_json,reconciliation_json,
   policy_revision,input_hash,revision,created_at,updated_at)
   VALUES(?,?,?,?,?,?,?, ?,?,'1',?,'1','{}','{}','{}',1,'testhash',1,?,?)`,
   fixture.id,"tenant",taskID,"human",fixture.id+"-unique",fixture.state,
   "goal.tcp_observer.control",resource,"project.goal.tcp_observer.pause",
   "onepane.goal.tcp_observer.control",now,now)
  if err!=nil{t.Fatalf("durable operator effect fixture: %v",err)}
 }
 uncertain,err:=svc.GoalResponseCandidateReadiness(ctx,project.ID,goal.ID,candidate.ID)
 if err!=nil{t.Fatal(err)}
 needsReconciliation,inFlight:=false,false
 for _,reason:=range uncertain.Blockers{
  if reason=="observer_mutation_needs_reconciliation"{needsReconciliation=true}
  if reason=="observer_mutation_in_flight"{inFlight=true}
 }
 if !needsReconciliation||!inFlight||uncertain.ExecutionAuthorized||
  uncertain.State!="blocked"{
  t.Fatalf("unknown or executing observer effects hidden from human review: %+v",uncertain)
 }
}
