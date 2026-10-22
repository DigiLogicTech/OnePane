//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestL5EvidenceOnlyReviewIsIdempotentRevocableAndNeverActivatesGoal(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/evidence-reviews.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,sql:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,sql);err!=nil{t.Fatal(err)}}
 s:=NewService(db.SQL(),db,clock.Real{})
 p,err:=s.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Review",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 w,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Sensors",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 goal,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:w.ID,Objective:"Evidence receipts",ActorPrincipalID:"owner",
  CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns","uptime"]}`)})
 if err!=nil{t.Fatal(err)}
 actor:="owner"
 o,err:=observation.NewService(db.SQL(),db,clock.Real{}).Record(ctx,observation.RecordCommand{
  WorkspaceID:"tenant",SubjectRef:"device://PRIVATE_DEVICE_ID",
  ObservationType:"health.sample",ProbeToolID:"dns",ProbeToolVersion:"v1",
  SourcePrincipalID:&actor,ActorPrincipalID:&actor,
  Value:json.RawMessage(`{"token":"RAW_SECRET_MUST_NOT_LEAK"}`),
  Label:policy.DataLabel{WorkspaceID:"tenant",Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyAny,Trust:policy.TrustAuthoritativeData},
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=s.AttachGoalObservation(ctx,AttachGoalObservationCommand{
  ProjectID:p.ID,GoalID:goal.ID,ObservationID:o.ID,
  ExpectedRevision:goal.Revision,ActorPrincipalID:"owner"});err!=nil{t.Fatal(err)}
 goal,err=s.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 permit,err:=s.ApproveGoalEvidenceReview(ctx,ApproveGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"dns",ExpectedGoalRevision:goal.Revision,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 goal,err=s.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 cmd:=ReviewGoalEvidenceCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"robot",
 }
 // The clock may cross a permit expiry while waiting for the SQLite
 // writer lock. Verify the final transaction, not the caller preflight
 // timestamp, decides permission.
 crossing:=NewService(db.SQL(),db,&twoStepReviewClock{
  before:permit.ExpiresAt-1,after:permit.ExpiresAt+1,
 })
 if _,err:=crossing.ReviewGoalEvidence(ctx,cmd);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("expired permit used stale pre-transaction clock: %v",err)
 }
 run,err:=s.ReviewGoalEvidence(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 if run.EvidenceState!="recent_attached"||run.LastObservedAt!=o.ObservedAt||
    run.MonitoringActive||run.SourceID!="dns"||run.GoalRevision!=goal.Revision{
  t.Fatalf("review fabricated health or lost scope: %+v",run)
 }
 repeat,err:=s.ReviewGoalEvidence(ctx,cmd)
 if err!=nil||repeat.ID!=run.ID{
  t.Fatalf("duplicate cadence window did not deduplicate: %+v %v",repeat,err)
 }
 history,err:=s.ListGoalEvidenceReviewRuns(ctx,p.ID,goal.ID)
 if err!=nil||len(history)!=1||history[0].ID!=run.ID{
  t.Fatalf("review provenance lost: %+v %v",history,err)
 }
 raw,_:=json.Marshal(history)
 if strings.Contains(string(raw),"RAW_SECRET_MUST_NOT_LEAK")||
  strings.Contains(string(raw),"PRIVATE_DEVICE_ID"){
  t.Fatalf("review history leaked private observation content: %s",raw)
 }
 current,err:=s.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Revision!=goal.Revision||current.Status!="configured"||
  current.Health!="unobserved"||current.NextDueAt!=0||
  current.LastVerifiedAt!=0||current.LastCheckedAt!=0{
  t.Fatalf("metadata-only review falsely activated monitoring: %+v",current)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_evidence_review_runs
   SET evidence_state='recent_attached' WHERE id=?`,run.ID);err==nil{
  t.Fatal("immutable review ledger was mutable")
 }
 if _,err:=s.RevokeGoalEvidenceReview(ctx,RevokeGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"owner"});err!=nil{t.Fatal(err)}
 cmd.ExpectedGoalRevision++
 cmd.ExpectedPermitRevision++
 if _,err:=s.ReviewGoalEvidence(ctx,cmd);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("revoked permit admitted a review: %v",err)
 }
 current,err=s.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatal("review or revocation altered active monitoring status")
 }
 var taskCount int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&taskCount);err!=nil||taskCount!=0{
  t.Fatalf("metadata review launched tasks: %d %v",taskCount,err)
 }
}

func TestL5EvidenceReviewDeniedWithoutPermitAndOnPause(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/review-denials.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 s:=NewService(db.SQL(),db,clock.Real{})
 p,err:=s.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Review",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"W",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 goal,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{ProjectID:p.ID,ProjectWorkspaceID:ws.ID,
  Objective:"Read receipts",ActorPrincipalID:"owner",MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`)})
 if err!=nil{t.Fatal(err)}
 cmd:=ReviewGoalEvidenceCommand{ProjectID:p.ID,GoalID:goal.ID,PermitID:"forged",
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:1,ActorPrincipalID:"owner"}
 if _,err:=s.ReviewGoalEvidence(ctx,cmd);err==nil{t.Fatal("review succeeded without a permit")}
 permit,err:=s.ApproveGoalEvidenceReview(ctx,ApproveGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"dns",ExpectedGoalRevision:goal.Revision,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 cmd.PermitID=permit.ID
 if _,err:=s.ReviewGoalEvidence(ctx,cmd);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale goal consent accepted: %v",err)
 }
 updated,err:=s.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 paused,err:=s.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
  ProjectID:p.ID,GoalID:goal.ID,ExpectedRevision:updated.Revision,
  TargetStatus:"paused",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 cmd.ExpectedGoalRevision=paused.Revision
 if _,err:=s.ReviewGoalEvidence(ctx,cmd);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("paused goal dispatched review: %v",err)
 }
}

type twoStepReviewClock struct {
 before int64
 after int64
 calls int
}
func(c *twoStepReviewClock)Now()time.Time{return time.UnixMilli(c.after)}
func(c *twoStepReviewClock)UnixMilli()int64{
 c.calls++
 if c.calls==1{return c.before}
 return c.after
}
