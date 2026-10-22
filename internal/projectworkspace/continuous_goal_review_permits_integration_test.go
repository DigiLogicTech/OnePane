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

func TestL5EvidenceReviewPermitIsScopedRevocableAndDoesNotActivateMonitoring(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/review-permit.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Outsider','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('foreign','outsider','active',1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 s:=NewService(db.SQL(),db,clock.Real{})
 l5,err:=s.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Owned devices",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:l5.ID,Name:"Monitoring",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 g,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:l5.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Review approved evidence only",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns","uptime"]}`),
 })
 if err!=nil{t.Fatal(err)}
 other,err:=s.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Other",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 approval:=ApproveGoalEvidenceReviewCommand{
  ProjectID:l5.ID,GoalID:g.ID,SourceID:"dns",
  ExpectedGoalRevision:g.Revision,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,
  ActorPrincipalID:"owner",
 }
 for _,c:=range []struct{name string;cmd ApproveGoalEvidenceReviewCommand}{
  {"agent self approval",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.ActorPrincipalID="robot";return x}()},
  {"foreign principal",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.ActorPrincipalID="outsider";return x}()},
  {"other Project",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.ProjectID=other.ID;return x}()},
  {"unknown source",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.SourceID="router.admin";return x}()},
  {"expired request",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.ExpiresAt=1;return x}()},
  {"unbounded grant",func()ApproveGoalEvidenceReviewCommand{x:=approval;x.ExpiresAt+=40*24*60*60*1000;return x}()},
 }{
  t.Run(c.name,func(t *testing.T){
   if _,err:=s.ApproveGoalEvidenceReview(ctx,c.cmd);err==nil{
    t.Fatal("unauthorised review grant persisted")
   }
  })
 }
 permit,err:=s.ApproveGoalEvidenceReview(ctx,approval)
 if err!=nil{t.Fatal(err)}
 if permit.ProjectID!=l5.ID||permit.ProjectWorkspaceID!=ws.ID||
    permit.SourceID!="dns"||permit.EffectClass!="existing_receipt_metadata"||
    permit.Status!="approved"||permit.PlanHash==""{
  t.Fatalf("review grant did not bind canonical scope: %+v",permit)
 }
 allow,err:=s.GoalEvidenceReviewAllowed(ctx,l5.ID,g.ID,"dns")
 if err!=nil||!allow{t.Fatalf("approved source denied: %t %v",allow,err)}
 for _,source:=range []string{"uptime","router.admin"}{
  allow,err:=s.GoalEvidenceReviewAllowed(ctx,l5.ID,g.ID,source)
  if err!=nil||allow{t.Fatalf("unapproved source %q allowed: %t %v",source,allow,err)}
 }
 stale:=approval
 if _,err:=s.ApproveGoalEvidenceReview(ctx,stale);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale goal revision accepted: %v",err)
 }
 updated,err:=s.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if updated.Status!="configured"||updated.Health!="unobserved"||
    updated.NextDueAt!=0||updated.LastVerifiedAt!=0{
  t.Fatalf("review permit activated monitoring: %+v",updated)
 }
 approval.ExpectedGoalRevision=updated.Revision
 if _,err:=s.ApproveGoalEvidenceReview(ctx,approval);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("duplicate unrevoked permit accepted: %v",err)
 }
 perms,err:=s.ListGoalEvidenceReviews(ctx,l5.ID,g.ID)
 if err!=nil||len(perms)!=1||perms[0].ID!=permit.ID{t.Fatalf("durable permit list: %+v %v",perms,err)}
 if _,err:=s.ListGoalEvidenceReviews(ctx,other.ID,g.ID);err==nil{
  t.Fatal("foreign Project read review grant")
 }
 revoke:=RevokeGoalEvidenceReviewCommand{
  ProjectID:l5.ID,GoalID:g.ID,PermitID:permit.ID,
  ExpectedGoalRevision:updated.Revision,
  ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"owner",
 }
 bot:=revoke;bot.ActorPrincipalID="robot"
 if _,err:=s.RevokeGoalEvidenceReview(ctx,bot);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent revoked a human permit: %v",err)
 }
 revoked,err:=s.RevokeGoalEvidenceReview(ctx,revoke)
 if err!=nil{t.Fatal(err)}
 if revoked.Status!="revoked"||revoked.Revision!=permit.Revision+1||
    revoked.RevokedAt==0||revoked.RevokedBy==nil{
  t.Fatalf("revocation not durable: %+v",revoked)
 }
 allow,err=s.GoalEvidenceReviewAllowed(ctx,l5.ID,g.ID,"dns")
 if err!=nil||allow{t.Fatalf("revoked source allowed: %t %v",allow,err)}
 if _,err:=s.RevokeGoalEvidenceReview(ctx,revoke);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("replayed revocation accepted: %v",err)
 }
 newGoal,err:=s.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 approval.ExpectedGoalRevision=newGoal.Revision
 second,err:=s.ApproveGoalEvidenceReview(ctx,approval)
 if err!=nil||second.ID==permit.ID{t.Fatalf("explicit reapproval failed: %+v %v",second,err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_evidence_review_permits SET plan_hash=? WHERE id=?",
  "sha256:0000000000000000000000000000000000000000000000000000000000000000",second.ID);err!=nil{t.Fatal(err)}
 allow,err=s.GoalEvidenceReviewAllowed(ctx,l5.ID,g.ID,"dns")
 if err!=nil||allow{t.Fatalf("changed grant plan hash accepted: %t %v",allow,err)}
 if _,err=db.SQL().ExecContext(ctx,
  "DELETE FROM project_goal_evidence_review_permits WHERE id=?",second.ID);err==nil{
  t.Fatal("audit permit was deleted")
 }
 var taskCount int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&taskCount);err!=nil||taskCount!=0{
  t.Fatalf("review permission launched Tasks: %d %v",taskCount,err)
 }
}

func TestL5EvidenceReviewExpirationAndWorkspaceRevocationFailClosed(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/review-expiry.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('t','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('h','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('t','h','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 s:=NewService(db.SQL(),db,clock.Real{})
 p,err:=s.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"t",Name:"Test",CreatedBy:"h",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"W",ActorPrincipalID:"h"})
 if err!=nil{t.Fatal(err)}
 g,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,Objective:"Read receipts",ActorPrincipalID:"h",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`)})
 if err!=nil{t.Fatal(err)}
 permit,err:=s.ApproveGoalEvidenceReview(ctx,ApproveGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"dns",ExpectedGoalRevision:g.Revision,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,ActorPrincipalID:"h"})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_goal_evidence_review_permits
 SET expires_at=? WHERE id=?`,permit.ApprovedAt-1,permit.ID);err==nil{
  // Schema enforces expiry after approval. A real clock advance is needed.
  t.Fatal("database accepted expiry predating grant issuance")
 }
 later:=NewService(db.SQL(),db,reviewPermitClock{ms:permit.ExpiresAt+1})
 allow,err:=later.GoalEvidenceReviewAllowed(ctx,p.ID,g.ID,"dns")
 if err!=nil||allow{t.Fatalf("expired permit permitted review: %t %v",allow,err)}
 if _,err:=db.SQL().ExecContext(ctx,
  "UPDATE project_workspaces SET status='archived' WHERE id=?",ws.ID);err!=nil{t.Fatal(err)}
 allow,err=s.GoalEvidenceReviewAllowed(ctx,p.ID,g.ID,"dns")
 if err!=nil||allow{t.Fatalf("archived workspace permitted review: %t %v",allow,err)}
}
type reviewPermitClock struct{ms int64}
func(c reviewPermitClock)Now()time.Time{return time.UnixMilli(c.ms)}
func(c reviewPermitClock)UnixMilli()int64{return c.ms}
