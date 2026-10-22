//go:build integration

package projectworkspace

import(
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestContinuousGoalRegistrationRequiresL5AndCanonicalWorkspace(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/l5goals.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Background agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Protect network",CreatedBy:"owner",
 });if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Monitoring",ActorPrincipalID:"owner",
 });if err!=nil{t.Fatal(err)}
 cmd:=ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:w.ID,ActorPrincipalID:"owner",
  Objective:"Keep selected devices monitored",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns","uptime"]}`),
 }
 if _,err:=svc.ConfigureContinuousGoal(ctx,cmd);err==nil{
  t.Fatal("Level 4 Project unexpectedly activated a Level 5 goal")
 }
 p,err=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
  ProjectID:p.ID,ExpectedRevision:p.Revision,ActorPrincipalID:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 other,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Other",CreatedBy:"owner",
 });if err!=nil{t.Fatal(err)}
 otherWS,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:other.ID,Name:"Other Workspace",ActorPrincipalID:"owner",
 });if err!=nil{t.Fatal(err)}
 wrong:=cmd;wrong.ProjectWorkspaceID=otherWS.ID
 if _,err:=svc.ConfigureContinuousGoal(ctx,wrong);err==nil{
  t.Fatal("cross-Project Workspace accepted")
 }
 malformed:=cmd
 malformed.MonitoringPlanJSON=json.RawMessage(`{"version":1,"source_ids":["*"]}`)
 if _,err:=svc.ConfigureContinuousGoal(ctx,malformed);err==nil{
  t.Fatal("unbounded source glob accepted")
 }
 g,err:=svc.ConfigureContinuousGoal(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 if g.ProjectID!=p.ID||g.ProjectWorkspaceID!=w.ID||
  g.Status!="configured"||g.Health!="unobserved"||
  g.NextDueAt!=0||g.LastVerifiedAt!=0||g.CadenceMinutes!=30{
  t.Fatalf("Goal misrepresented initial state: %+v",g)
 }
 list,err:=svc.ContinuousGoals(ctx,p.ID)
 if err!=nil||len(list)!=1||list[0].ID!=g.ID{t.Fatalf("goal not durable: %+v %v",list,err)}
 var n int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&n);err!=nil||n!=0{
  t.Fatalf("registration unexpectedly started a Task: %d %v",n,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET health='healthy' WHERE id=?",g.ID);err==nil{
  t.Fatal("database allowed declaring healthy without verified evidence")
 }
 if _,err:=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET status='retired',next_due_at=123 WHERE id=?",g.ID);err==nil{
  t.Fatal("database accepted recurring dispatch after retirement")
 }
 if _,err=db.SQL().ExecContext(ctx,
    "UPDATE project_continuous_goals SET next_due_at=123 WHERE id=?",g.ID);err!=nil{t.Fatal(err)}
 // User-triggered pause atomically invalidates the next due time and health.
 paused,err:=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"paused",
   ExpectedRevision:g.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 if paused.Status!="paused"||paused.NextDueAt!=0||paused.Health!="unobserved"||
    paused.Revision!=g.Revision+1{t.Fatalf("pause did not revoke schedule atomically: %+v",paused)}
 if _,err=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"retired",
   ExpectedRevision:g.Revision,ActorPrincipalID:"owner",
 });err!=ErrRevisionConflict{t.Fatalf("stale retirement revision accepted: %v",err)}
 if _,err=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:other.ID,GoalID:g.ID,TargetStatus:"retired",
   ExpectedRevision:paused.Revision,ActorPrincipalID:"owner",
 });err==nil{t.Fatal("foreign Project retired a goal")}
 if _,err=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"active",
   ExpectedRevision:paused.Revision,ActorPrincipalID:"owner",
 });err==nil{t.Fatal("goal activation bypassed monitor readiness")}
 if _,err=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"retired",
   ExpectedRevision:paused.Revision,ActorPrincipalID:"robot",
 });!errors.Is(err,ErrPrincipalIneligible){t.Fatalf("Agent could retire a goal: %v",err)}
 retired,err:=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"retired",
   ExpectedRevision:paused.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 if retired.Status!="retired"||retired.NextDueAt!=0||retired.Revision!=paused.Revision+1{
    t.Fatalf("retirement retained a schedule: %+v",retired)
 }
 if _,err=svc.TransitionContinuousGoal(ctx,TransitionContinuousGoalCommand{
   ProjectID:p.ID,GoalID:g.ID,TargetStatus:"paused",
   ExpectedRevision:retired.Revision,ActorPrincipalID:"owner",
 });err!=ErrInvalidTransition{t.Fatalf("retired goal could be revived: %v",err)}
 var count int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events WHERE
    aggregate_type='project_continuous_goal' AND aggregate_id=?`,g.ID).Scan(&count);err!=nil{
    t.Fatal(err)
 }
 if count!=3{t.Fatalf("expected configured+paused+retired event ledger, got %d",count)}
 var result string
 if err:=db.SQL().QueryRowContext(ctx,"PRAGMA quick_check").Scan(&result);err!=nil||result!="ok"{
  t.Fatalf("goal migration failed integrity check: %v %s",err,result)
 }
}
