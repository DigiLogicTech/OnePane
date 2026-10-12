//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Passive receipt reviews are the ONLY currently wired Level 5 background
// check. It never starts external sensors, Tasks, LLMs or remediations.
func TestL5AutomaticReceiptReviewIsConsentGatedDurableAndRestartSafe(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/review-auto.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 s:=NewService(db.SQL(),db,clock.Real{})
 if err:=s.EnsureGoalEvidenceReviewWorkerPrincipal(ctx);err!=nil{t.Fatal(err)}
 if err:=s.EnsureGoalEvidenceReviewWorkerPrincipal(ctx);err!=nil{t.Fatalf("review principal not idempotent: %v",err)}
 p,err:=s.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Review-only",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Evidence",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 goal,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Review existing receipts, without probing",
  CadenceMinutes:5,MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 before,err:=s.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(before)!=0{t.Fatalf("unapproved monitoring work ran: %+v %v",before,err)}
 permit,err:=s.ApproveGoalEvidenceReview(ctx,ApproveGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"dns",ExpectedGoalRevision:goal.Revision,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 first,err:=s.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(first)!=1||first[0].PermitID!=permit.ID||
  first[0].ReviewedBy!=GoalEvidenceReviewWorkerPrincipal||
  first[0].EvidenceState!="missing"||first[0].MonitoringActive{
  t.Fatalf("consented worker did not record missing receipts accurately: %+v %v",first,err)
 }
 repeat,err:=s.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(repeat)!=0{t.Fatalf("same-window poll repeated review: %+v %v",repeat,err)}
 // Worker restart does not re-review a cadence window, even if the
 // service instance changed and the in-memory scheduling state vanished.
 restarted:=NewService(db.SQL(),db,clock.Real{})
 after,err:=restarted.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(after)!=0{t.Fatalf("restart replayed cadence: %+v %v",after,err)}
 // Next due window must be computed from the current time without
 // replaying all offline windows (no catch-up storm on old hardware).
 future:=NewService(db.SQL(),db,reviewPermitClock{ms:clock.Real{}.UnixMilli()+11*60*1000})
 next,err:=future.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(next)!=1||next[0].CadenceWindow<=first[0].CadenceWindow{
  t.Fatalf("next due window not reviewed once: %+v %v",next,err)
 }
 if later,err:=future.TickDueGoalEvidenceReviews(ctx,8);err!=nil||len(later)!=0{
  t.Fatalf("future window duplicated: %+v %v",later,err)
 }
 updated,err:=s.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 if _,err:=s.RevokeGoalEvidenceReview(ctx,RevokeGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:updated.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"human"});err!=nil{t.Fatal(err)}
 afterStop,err:=future.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(afterStop)!=0{t.Fatalf("revoked permit restarted polling: %+v %v",afterStop,err)}
 current,err:=s.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||
   current.LastCheckedAt!=0||current.NextDueAt!=0||
   current.Status!="configured"{
  t.Fatalf("passive review claimed active device protection: %+v",current)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil||tasks!=0{
  t.Fatalf("automatic receipt reviewer launched Tasks: %d %v",tasks,err)
 }
}
