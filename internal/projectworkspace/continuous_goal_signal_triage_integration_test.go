//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestGoalSignalTriageRequiresLiveIndependentEvidenceAndNeverPromotesHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-signal-triage.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"No automatic health or recovery",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Signals",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:view.ID,ActorPrincipalID:"owner",
  Objective:"Compare narrowly scoped metadata only",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["tcp-link","peer-link"]}`),
 })
 if err!=nil{t.Fatal(err)}
 first,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if first.State!="no_recent_measurements"||len(first.Sources)!=2||
  first.DeviceIdentityVerified||first.MonitoringActive||first.RemediationAuthorized{
  t.Fatalf("empty coverage was called verified or monitoring: %+v",first)
 }
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"tcp-link",
  TargetIPv4:"192.168.1.20",TargetPort:443,TimeoutMS:500,
  ExpiresAt:now+30*60*1000,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 // A synthetic pre-approval receipt is NOT an authorised historical
 // measurement even if its source name and TCP target happen to match.
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_attempts(
  id,project_id,goal_id,permit_id,project_workspace_id,source_id,
  target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,
  permit_revision,requested_by,status,claimed_at,completed_at)
  VALUES('attempt-preapproval',?,?,?,?,?,?,?,?,?,?,?,'owner','not_connected',?,?)`,
  p.ID,goal.ID,permit.ID,view.ID,"tcp-link",permit.TargetIPv4,
  permit.TargetPort,permit.TimeoutMS,1,goal.Revision,permit.Revision,
  permit.ApprovedAt-1,permit.ApprovedAt-1)
 if err!=nil{t.Fatal(err)}
 preapproval,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if preapproval.RecentNegativeSources!=0||preapproval.State!="no_recent_measurements"{
  t.Fatalf("TCP receipt dated before human approval gained trust: %+v",preapproval)
 }
 // Seed a TEST-ONLY post-approval metadata receipt. No network socket.
 tcpAfterApproval:=clock.Real{}.UnixMilli()
 if tcpAfterApproval<permit.ApprovedAt{tcpAfterApproval=permit.ApprovedAt}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_attempts(
  id,project_id,goal_id,permit_id,project_workspace_id,source_id,
  target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,
  permit_revision,requested_by,status,claimed_at,completed_at)
  VALUES('attempt-adverse',?,?,?,?,?,?,?,?,?,?,?,'owner','not_connected',?,?)`,
  p.ID,goal.ID,permit.ID,view.ID,"tcp-link",permit.TargetIPv4,
  permit.TargetPort,permit.TimeoutMS,1,goal.Revision,permit.Revision,
  tcpAfterApproval,tcpAfterApproval)
 if err!=nil{t.Fatal(err)}
 tcpOnly,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if tcpOnly.State!="adverse_indicator_needs_review"||
  tcpOnly.RecentNegativeSources!=1||tcpOnly.RecentNodeNegativeSources!=0{
  t.Fatalf("single TCP test was called a multi-signal incident: %+v",tcpOnly)
 }
 fingerprint:="sha256:"+strings.Repeat("d",64)
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-id','paired','PAIRED_MTLS','{}','{}',1,?,?)`,
  now,now)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at)
  VALUES('pair','peer','inbound','paired','token','hash',
   '000000','hash',1,1,?,'',?,?,1,?,?)`,
  fingerprint,now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 grant,err:=svc.ApproveGoalNodeMeasurementGrant(ctx,
  ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:p.ID,GoalID:goal.ID,SourceID:"peer-link",
   PeerNodeID:"peer",ActorPrincipalID:"owner",
   ExpectedGoalRevision:goal.Revision,ExpiresAt:now+30*60*1000,
  })
 if err!=nil{t.Fatal(err)}
 // A trusted receiver never treats pre-approval timestamps as live
 // evidence. The synthetic receipt must follow human grant approval.
 observedAfterApproval:=clock.Real{}.UnixMilli()
 if observedAfterApproval<grant.ApprovedAt{
  observedAfterApproval=grant.ApprovedAt
 }
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_node_measurement_receipts(
  id,grant_id,project_id,goal_id,project_workspace_id,peer_node_id,
  source_id,sequence,signal_type,outcome,observed_at,received_at,transport_kind)
  VALUES('node-adverse',?,?,?,?,'peer','peer-link',1,
   'peer_connectivity','unreachable',?,?,'pinned_peer_mtls')`,
  grant.ID,p.ID,goal.ID,view.ID,observedAfterApproval,observedAfterApproval)
 if err!=nil{t.Fatal(err)}
 multiple,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if multiple.State!="multiple_adverse_indicators_investigate"||
  multiple.RecentNegativeSources!=2||
  multiple.RecentTCPNegativeSources!=1||
  multiple.RecentNodeNegativeSources!=1||
  len(multiple.Limitations)!=4||
  multiple.DeviceIdentityVerified||multiple.MonitoringActive||
  multiple.RemediationAuthorized{
  t.Fatalf("distinct adverse reports were misclassified as verified health: %+v",multiple)
 }
 for _,src:=range multiple.Sources{
  if src.TCPState=="not_connected"&&src.TCPAttemptID!="attempt-adverse"{
   t.Fatal("TCP receipt lost immutable evidence pointer")
  }
  if src.NodeState=="unreachable"&&
   (src.NodeReceiptID!="node-adverse"||src.PeerNodeID!="peer"){
   t.Fatal("mTLS receipt lost exact Node source provenance")
  }
 }
 payload,_:=json.Marshal(multiple)
 for _,secret:=range []string{"192.168.1.20","443","token","device_healthy"}{
  if strings.Contains(string(payload),secret){
   t.Fatalf("triage leaked target/credential or health assertion %q",secret)
  }
 }
 // Historical Node receipts remain immutable, but withdrawing the paired
 // Node must remove them from CURRENT multi-source decision support.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE harness_nodes
  SET trust_state='unavailable' WHERE id='peer'`);err!=nil{t.Fatal(err)}
 suspended,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if suspended.RecentNegativeSources!=1||
  suspended.RecentNodeNegativeSources!=0||
  suspended.State!="adverse_indicator_needs_review"{
  t.Fatalf("unavailable peer still counted as current signal: %+v",suspended)
 }
 // A suspended human permit approver cancels live TCP triage too.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
  SET status='suspended' WHERE workspace_id='tenant' AND principal_id='owner'`);
  err!=nil{t.Fatal(err)}
 inactive,err:=svc.GoalSignalTriage(ctx,p.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if inactive.RecentNegativeSources!=0||
  inactive.State!="no_recent_measurements"{
  t.Fatalf("human revocation left current adverse evidence trusted: %+v",inactive)
 }
 persisted,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if persisted.Health!="unobserved"||persisted.Status!="configured"||
  persisted.LastVerifiedAt!=0{
  t.Fatalf("triage mutated Goal lifecycle or health: %+v",persisted)
 }
 if _,err=svc.GoalSignalTriage(ctx,"other-tenant",goal.ID);err==nil{
  t.Fatal("foreign Project retrieved triage of another tenant")
 }
}
