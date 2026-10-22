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

func TestRecurringTCPNeedsFreshIndependentHumanConsent(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/recurring-tcp.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,statement:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Foreign','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','agent','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('foreign','outsider','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,statement);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Network opt-in",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Isolated",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Review one consenting source",
  CadenceMinutes:30,MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 expiry:=clock.Real{}.UnixMilli()+60*60*1000
 noGrant,err:=svc.ListTCPRecurringGrants(ctx,p.ID,goal.ID)
 if err!=nil||len(noGrant)!=0{t.Fatalf("manual consent should not mint recurring authority: %+v %v",noGrant,err)}
 manual,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.10.4",TargetPort:443,TimeoutMS:400,
  ExpiresAt:expiry,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 manualOnly,err:=svc.ListTCPRecurringGrants(ctx,p.ID,goal.ID)
 if err!=nil||len(manualOnly)!=0{t.Fatalf("manual grant secretly enabled recurring work: %+v %v",manualOnly,err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 cmd:=ApproveTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:goal.ID,ManualPermitID:manual.ID,
  ExpectedManualPermitRevision:manual.Revision,
  ExpectedGoalRevision:goal.Revision,ExpiresAt:expiry,
  ActorPrincipalID:"human",
 }
 bads:=[]struct{name string;apply func(*ApproveTCPRecurringGrantCommand)}{
  {"Agent cannot self authorise",func(c *ApproveTCPRecurringGrantCommand){c.ActorPrincipalID="agent"}},
  {"foreign member",func(c *ApproveTCPRecurringGrantCommand){c.ActorPrincipalID="outsider"}},
  {"other goal",func(c *ApproveTCPRecurringGrantCommand){c.GoalID="other"}},
  {"bad parent",func(c *ApproveTCPRecurringGrantCommand){c.ManualPermitID="none"}},
  {"stale manual revision",func(c *ApproveTCPRecurringGrantCommand){c.ExpectedManualPermitRevision++}},
  {"stale goal revision",func(c *ApproveTCPRecurringGrantCommand){c.ExpectedGoalRevision++}},
  {"extra duration",func(c *ApproveTCPRecurringGrantCommand){c.ExpiresAt+=3600000}},
  {"expired",func(c *ApproveTCPRecurringGrantCommand){c.ExpiresAt=1}},
 }
 for _,tt:=range bads{
  t.Run(tt.name,func(t *testing.T){
   bad:=cmd;tt.apply(&bad)
   if _,err:=svc.ApproveTCPRecurringGrant(ctx,bad);err==nil{
    t.Fatal("unapproved network schedule consent persisted")
   }
  })
 }
 grant,err:=svc.ApproveTCPRecurringGrant(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 if grant.Status!="approved"||grant.EffectClass!="recurring_tcp_connect"||
  grant.ManualPermitID!=manual.ID||grant.TargetIPv4!=manual.TargetIPv4||
  grant.TargetPort!=443||grant.CadenceMinutes!=30||
  grant.ProjectWorkspaceID!=ws.ID||grant.ApprovedBy!="human"{
  t.Fatalf("recurring target authority drifted: %+v",grant)
 }
 allowed,err:=svc.TCPRecurringGrantAllowed(ctx,grant.ID)
 if err!=nil||!allowed{t.Fatalf("explicit recurring authority unavailable: %t %v",allowed,err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 stale:=cmd
 if _,err:=svc.ApproveTCPRecurringGrant(ctx,stale);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale recurring approval re-used: %v",err)
 }
 for _,sql:=range []string{
  "UPDATE project_goal_tcp_recurring_grants SET target_port=22 WHERE id=?",
  "UPDATE project_goal_tcp_recurring_grants SET expires_at=expires_at+3600000 WHERE id=?",
  "UPDATE project_goal_tcp_recurring_grants SET cadence_minutes=15 WHERE id=?",
  "DELETE FROM project_goal_tcp_recurring_grants WHERE id=?",
 }{
  if _,err:=db.SQL().ExecContext(ctx,sql,grant.ID);err==nil{
   t.Fatalf("human-approved recurring scope was mutable: %s",sql)
  }
 }
 revoke:=RevokeTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:goal.ID,GrantID:grant.ID,
  ExpectedGoalRevision:goal.Revision,
  ExpectedGrantRevision:grant.Revision,ActorPrincipalID:"human",
 }
 agent:=revoke;agent.ActorPrincipalID="agent"
 if _,err:=svc.RevokeTCPRecurringGrant(ctx,agent);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent revoked human scope: %v",err)
 }
 revoked,err:=svc.RevokeTCPRecurringGrant(ctx,revoke)
 if err!=nil||revoked.Status!="revoked"||revoked.Revision!=2{
  t.Fatalf("recurring revoke failed: %+v %v",revoked,err)
 }
 allowed,err=svc.TCPRecurringGrantAllowed(ctx,grant.ID)
 if err!=nil||allowed{t.Fatalf("revoked recurring authority still valid: %t %v",allowed,err)}
 if _,err:=svc.RevokeTCPRecurringGrant(ctx,revoke);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("replayed recurring revoke accepted: %v",err)
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.LastCheckedAt!=0||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("consent alone activated scanning or health: %+v",current)
 }
 var calls,observations int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&calls);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM observations").Scan(&observations);err!=nil{t.Fatal(err)}
 if calls!=0||observations!=0{
  t.Fatalf("recurring consent issued unintended network probe: attempts=%d observations=%d",calls,observations)
 }
}

func TestRecurringTCPDeniedAfterManualRevocationOrApproverLoss(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/recurring-liveness.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('person','human','Person','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','person','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Source",CreatedBy:"person",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Local",ActorPrincipalID:"person"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"person",
  Objective:"One source",CadenceMinutes:15,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["svc"]}`)})
 if err!=nil{t.Fatal(err)}
 expiry:=clock.Real{}.UnixMilli()+3600000
 manual,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"svc",TargetIPv4:"10.5.4.3",
  TargetPort:443,TimeoutMS:200,ExpiresAt:expiry,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"person"})
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 recurring,err:=svc.ApproveTCPRecurringGrant(ctx,ApproveTCPRecurringGrantCommand{
  ProjectID:p.ID,GoalID:goal.ID,ManualPermitID:manual.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedManualPermitRevision:manual.Revision,
  ActorPrincipalID:"person",ExpiresAt:expiry})
 if err!=nil{t.Fatal(err)}
 if valid,err:=svc.TCPRecurringGrantAllowed(ctx,recurring.ID);err!=nil||!valid{
  t.Fatalf("valid recurring permission missing: %t %v",valid,err)
 }
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='revoked' WHERE workspace_id='tenant' AND principal_id='person'`)
 if err!=nil{t.Fatal(err)}
 if valid,err:=svc.TCPRecurringGrantAllowed(ctx,recurring.ID);err!=nil||valid{
  t.Fatalf("inactive human authorisation still grants recurring probes: %t %v",valid,err)
 }
 _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='active' WHERE workspace_id='tenant' AND principal_id='person'`)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 _,err=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:manual.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:manual.Revision,
  ActorPrincipalID:"person"})
 if err!=nil{t.Fatal(err)}
 if valid,err:=svc.TCPRecurringGrantAllowed(ctx,recurring.ID);err!=nil||valid{
  t.Fatalf("revoked underlying TCP permit remained recurring authority: %t %v",valid,err)
 }
}
