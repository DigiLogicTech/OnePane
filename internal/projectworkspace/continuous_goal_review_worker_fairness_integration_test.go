//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "fmt"
 "testing"
 
 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestL5ReviewCursorReachesHealthySourceBeyondCorruptFirstPage(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/fair-goal-review.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 if err:=svc.EnsureGoalEvidenceReviewWorkerPrincipal(ctx);err!=nil{t.Fatal(err)}
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Fair review",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Review",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 count:=0
 for group,size:=range []int{32,32,1}{
  ids:=make([]string,0,size)
  for i:=0;i<size;i++{ids=append(ids,fmt.Sprintf("src%03d",count+i))}
  raw,_:=json.Marshal(map[string]any{"version":1,"source_ids":ids})
  goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
   ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
   Objective:fmt.Sprintf("Corrupt permit set %d",group),
   MonitoringPlanJSON:raw,CadenceMinutes:5,
  })
  if err!=nil{t.Fatal(err)}
  for _,id:=range ids{
   permitID:=fmt.Sprintf("bad%03d",count)
   count++
   // This is intentionally legacy-corrupted persisted input. A bad plan
   // hash passes the DB shape but never obtains runtime review authority.
   _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_evidence_review_permits(
     id,project_id,goal_id,project_workspace_id,source_id,plan_hash,
     effect_class,status,revision,approved_by,approved_at,expires_at)
     VALUES(?,?,?,?,?,'sha256:0000000000000000000000000000000000000000000000000000000000000000',
      'existing_receipt_metadata','approved',1,'human',?,?)`,
      permitID,project.ID,goal.ID,ws.ID,id,now,now+3600000)
   if err!=nil{t.Fatal(err)}
  }
 }
 if count!=65{t.Fatalf("fixture lacks cursor boundary: %d",count)}
 goodGoal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Healthy source should run",CadenceMinutes:5,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["healthy"]}`),
 })
 if err!=nil{t.Fatal(err)}
 good,err:=svc.ApproveGoalEvidenceReview(ctx,ApproveGoalEvidenceReviewCommand{
  ProjectID:project.ID,GoalID:goodGoal.ID,SourceID:"healthy",
  ExpectedGoalRevision:goodGoal.Revision,
  ActorPrincipalID:"human",ExpiresAt:now+3600000,
 })
 if err!=nil{t.Fatal(err)}
 batch1,err:=svc.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(batch1)!=0{t.Fatalf("corrupt cursor page unexpectedly executed: %+v %v",batch1,err)}
 var cursor string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT after_permit_id
   FROM goal_review_worker_cursors WHERE worker_id='receipt_metadata'`).Scan(&cursor);err!=nil{
  t.Fatal(err)
 }
 if cursor!="bad063"{t.Fatalf("first page cursor not durable: %q",cursor)}
 // Restart proves fairness isn't dependent on transient in-memory state.
 restarted:=NewService(db.SQL(),db,clock.Real{})
 batch2,err:=restarted.TickDueGoalEvidenceReviews(ctx,8)
 if err!=nil||len(batch2)!=1||batch2[0].PermitID!=good.ID||
  batch2[0].EvidenceState!="missing"{
  t.Fatalf("healthy source starved beyond 64 broken permits: %+v %v",batch2,err)
 }
 if _,err:=restarted.TickDueGoalEvidenceReviews(ctx,8);err!=nil{
  t.Fatalf("cursor did not wrap safely: %v",err)
 }
 var reviewed int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_evidence_review_runs WHERE permit_id=?",
  good.ID).Scan(&reviewed);err!=nil||reviewed!=1{
  t.Fatalf("healthy source reviewed more than once: %d %v",reviewed,err)
 }
}
