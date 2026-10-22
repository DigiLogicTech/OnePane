//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestTCPProbeOnlyExactPrivateIPv4Literal(t *testing.T){
 for _,ip:=range []string{
  "127.0.0.1","localhost","169.254.169.254","8.8.8.8",
  "100.64.0.1","0.0.0.0","224.0.0.1","::1",
  "::ffff:192.168.1.2","fc00::1","192.168.1.2/24",
  "192.168.1.2:443","192.168.001.002"," 192.168.1.2",
  "192.168.1.2 ","10.0.0.1%eth0",
 }{
  if accepted,_:=canonicalPrivateProbeIPv4(ip);accepted!=""{
   t.Fatalf("non-private or nonliteral IP admitted %q -> %q",ip,accepted)
  }
 }
 for _,ip:=range []string{"10.0.0.1","172.16.0.3","172.31.255.254","192.168.1.1"}{
  if accepted,err:=canonicalPrivateProbeIPv4(ip);err!=nil||accepted!=ip{
   t.Fatalf("private IPv4 rejected %q: %q %v",ip,accepted,err)
  }
 }
}

func TestTCPProbePermitIsSeparatelyApprovedHumanOnlyAndRevocable(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-probe-permits.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('other','Other','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Outsider','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('other','outsider','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",Name:"Home monitoring",
  CreatedBy:"owner",ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Devices",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Check explicitly approved LAN service",CadenceMinutes:60,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router","dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 expiry:=clock.Real{}.UnixMilli()+45*60*1000
 approved:=ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"router",TargetIPv4:"192.168.1.1",
  TargetPort:443,TimeoutMS:500,ExpiresAt:expiry,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"owner",
 }
 tests:=[]struct{name string;change func(*ApproveGoalTCPProbeCommand)}{
  {"robot self permission",func(c *ApproveGoalTCPProbeCommand){c.ActorPrincipalID="robot"}},
  {"foreign operator",func(c *ApproveGoalTCPProbeCommand){c.ActorPrincipalID="outsider"}},
  {"foreign project",func(c *ApproveGoalTCPProbeCommand){c.ProjectID="some-other-project"}},
  {"unknown source",func(c *ApproveGoalTCPProbeCommand){c.SourceID="network-admin"}},
  {"public IP",func(c *ApproveGoalTCPProbeCommand){c.TargetIPv4="8.8.8.8"}},
  {"local metadata",func(c *ApproveGoalTCPProbeCommand){c.TargetIPv4="169.254.169.254"}},
  {"loopback",func(c *ApproveGoalTCPProbeCommand){c.TargetIPv4="127.0.0.1"}},
  {"DNS hostname",func(c *ApproveGoalTCPProbeCommand){c.TargetIPv4="router.home.arpa"}},
  {"HTTP URL",func(c *ApproveGoalTCPProbeCommand){c.TargetIPv4="http://192.168.1.1"}},
  {"full port range",func(c *ApproveGoalTCPProbeCommand){c.TargetPort=0}},
  {"too slow",func(c *ApproveGoalTCPProbeCommand){c.TimeoutMS=5000}},
  {"too brief",func(c *ApproveGoalTCPProbeCommand){c.TimeoutMS=1}},
  {"expired",func(c *ApproveGoalTCPProbeCommand){c.ExpiresAt=1}},
  {"overlong",func(c *ApproveGoalTCPProbeCommand){c.ExpiresAt+=48*60*60*1000}},
  {"stale CAS",func(c *ApproveGoalTCPProbeCommand){c.ExpectedGoalRevision=999}},
 }
 for _,tt:=range tests{
  t.Run(tt.name,func(t *testing.T){
   bad:=approved;tt.change(&bad)
   if _,err:=svc.ApproveGoalTCPProbe(ctx,bad);err==nil{
    t.Fatal("invalid or unconsented network target registered")
   }
  })
 }
 pmt,err:=svc.ApproveGoalTCPProbe(ctx,approved)
 if err!=nil{t.Fatal(err)}
 if pmt.SourceID!="router"||pmt.TargetIPv4!="192.168.1.1"||
    pmt.TargetPort!=443||pmt.TimeoutMS!=500||
    pmt.EffectClass!="network_tcp_connect"||pmt.Status!="approved"||
    pmt.PlanHash==""||pmt.ProjectWorkspaceID!=ws.ID{
  t.Fatalf("network permit lost exact scope: %+v",pmt)
 }
 // Network approval must NEVER be editable into a different endpoint
 // or longer expiry after the human has approved the original address.
 for _,stmt:=range []string{
  "UPDATE project_goal_tcp_probe_permits SET target_ipv4='192.168.1.2' WHERE id=?",
  "UPDATE project_goal_tcp_probe_permits SET target_port=22 WHERE id=?",
  "UPDATE project_goal_tcp_probe_permits SET expires_at=expires_at+86400000 WHERE id=?",
  "UPDATE project_goal_tcp_probe_permits SET source_id='dns' WHERE id=?",
  "UPDATE project_goal_tcp_probe_permits SET plan_hash='sha256:0000000000000000000000000000000000000000000000000000000000000000' WHERE id=?",
 }{
  if _,err:=db.SQL().ExecContext(ctx,stmt,pmt.ID);err==nil{
   t.Fatalf("approved TCP scope was modified by %s",stmt)
  }
 }
 updated,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if updated.Status!="configured"||updated.Health!="unobserved"||
    updated.NextDueAt!=0||updated.LastCheckedAt!=0||updated.LastVerifiedAt!=0{
  t.Fatalf("registration activated network monitoring: %+v",updated)
 }
 approved.ExpectedGoalRevision=updated.Revision
 if _,err:=svc.ApproveGoalTCPProbe(ctx,approved);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("duplicate current source permission accepted: %v",err)
 }
 permits,err:=svc.ListGoalTCPProbePermits(ctx,p.ID,goal.ID)
 if err!=nil||len(permits)!=1||permits[0].ID!=pmt.ID{
  t.Fatalf("source permit unavailable: %+v %v",permits,err)
 }
 if _,err:=svc.ListGoalTCPProbePermits(ctx,"other-project",goal.ID);err==nil{
  t.Fatal("foreign Project could inspect network permit")
 }
 revoke:=RevokeGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:pmt.ID,
  ExpectedGoalRevision:updated.Revision,ExpectedPermitRevision:pmt.Revision,
  ActorPrincipalID:"owner",
 }
 bot:=revoke;bot.ActorPrincipalID="robot"
 if _,err:=svc.RevokeGoalTCPProbe(ctx,bot);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent could revoke permission: %v",err)
 }
 revoked,err:=svc.RevokeGoalTCPProbe(ctx,revoke)
 if err!=nil||revoked.Status!="revoked"||
    revoked.Revision!=pmt.Revision+1||revoked.RevokedAt==0{
  t.Fatalf("permission revocation failed: %+v %v",revoked,err)
 }
 if _,err:=svc.RevokeGoalTCPProbe(ctx,revoke);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("replayed revocation accepted: %v",err)
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 approved.ExpectedGoalRevision=current.Revision
 next,err:=svc.ApproveGoalTCPProbe(ctx,approved)
 if err!=nil||next.ID==pmt.ID{t.Fatalf("human regrant denied: %+v %v",next,err)}
 if _,err:=db.SQL().ExecContext(ctx,
  "DELETE FROM project_goal_tcp_probe_permits WHERE id=?",next.ID);err==nil{
  t.Fatal("network authority audit row could be deleted")
 }
 var taskCount,observationCount int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&taskCount);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM observations").Scan(&observationCount);err!=nil{t.Fatal(err)}
 if taskCount!=0||observationCount!=0{
  t.Fatalf("registration performed probe side effect: tasks=%d obs=%d",taskCount,observationCount)
 }
 if serialized,_:=json.Marshal(permits);strings.Contains(string(serialized),"secret") {
  t.Fatal("permit metadata includes secret material")
 }
}
