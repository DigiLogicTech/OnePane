//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "net"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type fakeGoalTCPDialer struct{
 calls int
 network string
 address string
 fail error
}
func(d *fakeGoalTCPDialer)DialContext(ctx context.Context,network,address string)(net.Conn,error){
 d.calls++;d.network=network;d.address=address
 if d.fail!=nil{return nil,d.fail}
 c,peer:=net.Pipe();_ = peer.Close()
 return c,nil
}

func TestManualGoalTCPProbeUsesOnlyExactPermittedSocketAndIsCrashIdempotent(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/manual-tcp-test.db")
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
  WorkspaceID:"tenant",Name:"Approved network",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"One local service",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:w.ID,
  ActorPrincipalID:"human",Objective:"One manual reachability check",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
  CadenceMinutes:5,
 })
 if err!=nil{t.Fatal(err)}
 cmd:=RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:g.ID,PermitID:"made-up",
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:1,
  ActorPrincipalID:"human",
 }
 dialer:=&fakeGoalTCPDialer{}
 if _,err:=svc.runManualGoalTCPProbe(ctx,cmd,dialer);err==nil||dialer.calls!=0{
  t.Fatalf("unapproved socket was attempted: %d %v",dialer.calls,err)
 }
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:g.ID,SourceID:"router",
  TargetIPv4:"192.168.14.21",TargetPort:443,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,
  ExpectedGoalRevision:g.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 cmd.PermitID=permit.ID;cmd.ExpectedGoalRevision=g.Revision
 robot:=cmd;robot.ActorPrincipalID="robot"
 if _,err:=svc.runManualGoalTCPProbe(ctx,robot,dialer);!errors.Is(err,ErrPrincipalIneligible)||dialer.calls!=0{
  t.Fatalf("Agent initiated probe: %d %v",dialer.calls,err)
 }
 first,err:=svc.runManualGoalTCPProbe(ctx,cmd,dialer)
 if err!=nil{t.Fatal(err)}
 if dialer.calls!=1||dialer.network!="tcp4"||dialer.address!="192.168.14.21:443"||
  first.Status!="connected"||first.RecurringGrantID!=nil||first.TargetIPv4!=permit.TargetIPv4||
  first.TargetPort!=443||first.MonitoringActive||first.CompletedAt<first.ClaimedAt{
  t.Fatalf("network dispatch escaped source grant: calls=%d network=%s address=%s receipt=%+v",
   dialer.calls,dialer.network,dialer.address,first)
 }
 // A claim and its completed outcome must both be scoped to the
 // owning tenant. An unscoped final event disappears from Workspace
 // subscriptions even though it may carry security-sensitive metadata.
 var scopedEvents,unscopedEvents int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type IN(
   'project.goal_tcp_probe_claimed','project.goal_tcp_probe_finished')
   AND workspace_id='tenant'`,first.ID).Scan(&scopedEvents);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type IN(
   'project.goal_tcp_probe_claimed','project.goal_tcp_probe_finished')
   AND (workspace_id IS NULL OR workspace_id<>'tenant')`,first.ID).
  Scan(&unscopedEvents);err!=nil{t.Fatal(err)}
 if scopedEvents!=2||unscopedEvents!=0{
  t.Fatalf("TCP audit events escaped tenant scope: scoped=%d unscoped=%d",
   scopedEvents,unscopedEvents)
 }
 // A real TCP handshake never masquerades as an integrity-verified,
 // attached Observation receipt. Both evidence classes are displayed.
 coverage,err:=svc.GoalEvidenceCoverage(ctx,project.ID,g.ID)
 if err!=nil||len(coverage.Sources)!=1||
  coverage.Sources[0].EvidenceState!="missing"||
  coverage.Sources[0].TCPReachability!="connected"||
  coverage.Sources[0].LastTCPTestAt!=first.ClaimedAt||
  coverage.MonitoringActive{
  t.Fatalf("TCP test was misrepresented as verified evidence: %+v %v",coverage,err)
 }
 if _,err:=svc.runManualGoalTCPProbe(ctx,cmd,dialer);!errors.Is(err,ErrInvalidTransition)||dialer.calls!=1{
  t.Fatalf("same-window duplicate dial permitted: calls=%d %v",dialer.calls,err)
 }
 // A service restart cannot clear the unique attempt/cadence fence.
 restarted:=NewService(db.SQL(),db,clock.Real{})
 if _,err:=restarted.runManualGoalTCPProbe(ctx,cmd,dialer);!errors.Is(err,ErrInvalidTransition)||dialer.calls!=1{
  t.Fatalf("restart redialled an already claimed socket: calls=%d %v",dialer.calls,err)
 }
 history,err:=svc.ListGoalTCPProbeAttempts(ctx,project.ID,g.ID)
 if err!=nil||len(history)!=1||history[0].ID!=first.ID{
  t.Fatalf("TCP history not immutable: %+v %v",history,err)
 }
 if _,err:=svc.ListGoalTCPProbeAttempts(ctx,"another-project",g.ID);err==nil{
  t.Fatal("foreign Project read reachability history")
 }
 if _,err:=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_tcp_probe_attempts SET status='connected' WHERE id=?",first.ID);err==nil{
  t.Fatal("finalised TCP receipt was rewritten")
 }
 if _,err:=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_tcp_probe_attempts SET recurring_grant_id='forged' WHERE id=?",first.ID);err==nil{
  t.Fatal("manual TCP receipt was rebound to a recurring consent")
 }
 // Reissuing consent cannot reset this source's cadence rate budget.
 // Otherwise a malicious caller could revoke/reapprove repeatedly and
 // perform a rapid multi-port scan with distinct permit IDs.
 if _,err:=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:g.ID,PermitID:permit.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"human",
 });err!=nil{t.Fatal(err)}
 renewedGoal,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 secondPermit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:g.ID,SourceID:"router",
  TargetIPv4:"192.168.14.22",TargetPort:80,TimeoutMS:300,
  ExpiresAt:clock.Real{}.UnixMilli()+60*60*1000,
  ExpectedGoalRevision:renewedGoal.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 renewedGoal,err=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 regrant:=cmd
 regrant.PermitID=secondPermit.ID
 regrant.ExpectedGoalRevision=renewedGoal.Revision
 if _,err:=svc.runManualGoalTCPProbe(ctx,regrant,dialer);
  !errors.Is(err,ErrInvalidTransition)||dialer.calls!=1{
  t.Fatalf("regrant multiplied same-source TCP test in cadence: calls=%d %v",dialer.calls,err)
 }
 current,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.NextDueAt!=0||current.LastCheckedAt!=0||current.LastVerifiedAt!=0{
  t.Fatalf("TCP success incorrectly became verified health: %+v",current)
 }
 var tasks,observations int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM observations").Scan(&observations);err!=nil{t.Fatal(err)}
 if tasks!=0||observations!=0{
  t.Fatalf("bounded TCP test invoked unrelated workflows: %d %d",tasks,observations)
 }
 // An overloaded local host must not queue unbounded network Claims.
 manualTCPProbeSlots<-struct{}{}
 manualTCPProbeSlots<-struct{}{}
 cancelled,cancel:=context.WithCancel(ctx)
 cancel()
 _,blockedErr:=svc.runManualGoalTCPProbe(cancelled,cmd,&fakeGoalTCPDialer{})
 <-manualTCPProbeSlots
 <-manualTCPProbeSlots
 if !errors.Is(blockedErr,context.Canceled){
  t.Fatalf("cancelled request blocked behind socket capacity: %v",blockedErr)
 }
}

func TestManualTCPProbeRechecksRevocationAndNeverDialsAfterClaim(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-revoke-inflight.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Manual",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Local",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"One manually approved source",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",
  TargetIPv4:"10.20.30.40",TargetPort:80,TimeoutMS:500,
  ExpiresAt:clock.Real{}.UnixMilli()+3600000,
  ExpectedGoalRevision:g.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 claim,err:=svc.claimGoalTCPAttempt(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 // Even an unexpired permit must stop authorising a queued socket
 // if its human approver loses the tenant Workspace membership.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='revoked' WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 allowed,err:=svc.tcpAttemptStillAuthorised(ctx,claim)
 if err!=nil||allowed{t.Fatalf("removed approver still authorised TCP: %t %v",allowed,err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='active' WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 revoked,err:=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"human",
 })
 if err!=nil||revoked.Status!="revoked"{t.Fatalf("permit revoke: %+v %v",revoked,err)}
 allowed,err=svc.tcpAttemptStillAuthorised(ctx,claim)
 if err!=nil||allowed{t.Fatalf("stale claim still valid after revoke: %t %v",allowed,err)}
 cancelled,err:=svc.completeTCPAttempt(ctx,claim,"cancelled")
 if err!=nil||cancelled.Status!="cancelled"{
  t.Fatalf("no-network cancellation unrecorded: %+v %v",cancelled,err)
 }
 if _,err:=svc.completeTCPAttempt(ctx,claim,"connected");!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("cancelled network attempt completed again: %v",err)
 }
}
