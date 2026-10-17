//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestNodeMeasurementRequiresUnchangedGoalPlanGeneration(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"hub","hub-identity")
 defer db.Close()
 for _,stmt:=range []string{
  "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)",
 }{
  if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}
 }
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Plan version admission",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage("{\"onepane_operations\":{\"version\":1,\"mode\":\"continuous_operations\"}}"),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Enrolled source",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,
  ActorPrincipalID:"human",Objective:"Human-approved Node evidence",
  MonitoringPlanJSON:json.RawMessage("{\"version\":1,\"source_ids\":[\"peer-link\"]}"),
 })
 if err!=nil{t.Fatal(err)}
 peer,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  "INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('peer','Remote',0,'peer-id','paired','PAIRED_MTLS','{}','{}',1,1,1)",
 }{
  if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 _,err=db.SQL().ExecContext(ctx,
  "INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,revision,created_at,updated_at) VALUES('pair-peer','peer','inbound','paired','token','hash','000000','hash',1,1,?,?,?, ?,1,?,?)",
  peer.Fingerprint,string(peer.CertPEM),now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 grant,err:=svc.ApproveGoalNodeMeasurementGrant(ctx,
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:project.ID,GoalID:goal.ID,PeerNodeID:"peer",
   SourceID:"peer-link",ActorPrincipalID:"human",
   ExpectedGoalRevision:goal.Revision,ExpiresAt:now+30*60000,
  })
 if err!=nil{t.Fatal(err)}
 if grant.PlanGeneration!=1||!strings.HasPrefix(grant.PlanSHA256,"sha256:"){
  t.Fatalf("human approval missed canonical current plan: %+v",grant)
 }
 nodeIdentity,err:=EnsureIdentity(t.TempDir(),"hub")
 if err!=nil{t.Fatal(err)}
 transport:=NewService(db.SQL(),db,clock.Real{},
  NodeView{ID:"hub",Local:true},nodeIdentity,"",30*time.Second)
 send:=func(grantID string,seq int64)error{
  _,err:=transport.AcceptPairedNodeMeasurement(ctx,"peer",peer.Fingerprint,
   PeerNodeMeasurement{
    Protocol:ProtocolVersion,GrantID:grantID,Sequence:seq,
    SignalType:"peer_connectivity",Outcome:"reachable",
    ObservedAt:clock.Real{}.UnixMilli(),
   })
  return err
 }
 if err=send(grant.ID,1);err!=nil{
  t.Fatalf("first exact-plan authenticated receipt rejected: %v",err)
 }
 var original string
 var gen int64
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT monitoring_plan_json,plan_generation FROM project_continuous_goals WHERE id=?",
  goal.ID).Scan(&original,&gen);err!=nil||gen!=1{
  t.Fatalf("unexpected initial plan: %v %d",err,gen)
 }
 // Keep the old source ID while changing the security-sensitive
 // allowlist. Old approval must stop receiving samples immediately.
 _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
  "{\"version\":1,\"source_ids\":[\"peer-link\",\"other-source\"]}",goal.ID)
 if err!=nil{t.Fatal(err)}
 if err=send(grant.ID,2);!errors.Is(err,ErrNodeMeasurementNotAdmitted){
  t.Fatalf("new source plan retained stale transport privilege: %v",err)
 }
 inventory,err:=svc.ListGoalNodeMeasurementGrants(ctx,project.ID,goal.ID)
 if err!=nil||len(inventory)!=1||inventory[0].EffectiveTransportState!="scope_inactive"{
  t.Fatalf("operator inventory hid stale plan authority: %+v %v",inventory,err)
 }
 // Restore the exact old bytes: a monotonic plan generation prevents
 // a stale grant from resurrecting even when SHA-256 again matches.
 _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
  original,goal.ID)
 if err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT plan_generation FROM project_continuous_goals WHERE id=?",
  goal.ID).Scan(&gen);err!=nil||gen!=3{
  t.Fatalf("plan generation failed to count both edits: %v %d",err,gen)
 }
 if err=send(grant.ID,2);!errors.Is(err,ErrNodeMeasurementNotAdmitted){
  t.Fatalf("restoring old plan resurrected stale Node grant: %v",err)
 }
 var oldSeq int64
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT last_sequence FROM project_goal_node_measurement_grants WHERE id=?",
  grant.ID).Scan(&oldSeq);err!=nil||oldSeq!=1{
  t.Fatalf("stale admission spent sequence or wrote receipt: %v %d",err,oldSeq)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_node_measurement_grants SET plan_sha256=? WHERE id=?",
  "sha256:"+strings.Repeat("0",64),grant.ID);err==nil{
  t.Fatal("immutable human-approved grant fingerprint overwritten")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 revoked,err:=svc.RevokeGoalNodeMeasurementGrant(ctx,
  projectworkspace.RevokeGoalNodeMeasurementGrantCommand{
   ProjectID:project.ID,GoalID:goal.ID,GrantID:grant.ID,
   ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
   ExpectedGrantRevision:2,
  })
 if err!=nil||revoked.Status!="revoked"{
  t.Fatalf("human could not retire stale grant: %v %+v",err,revoked)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 replacement,err:=svc.ApproveGoalNodeMeasurementGrant(ctx,
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:project.ID,GoalID:goal.ID,PeerNodeID:"peer",
   SourceID:"peer-link",ActorPrincipalID:"human",
   ExpectedGoalRevision:goal.Revision,ExpiresAt:now+30*60000,
  })
 if err!=nil||replacement.PlanGeneration!=3{
  t.Fatalf("fresh explicit plan approval unavailable: %v %+v",err,replacement)
 }
 if err=send(replacement.ID,1);err!=nil{
  t.Fatalf("fresh independently approved Node grant was denied: %v",err)
 }
 if err=send(grant.ID,2);!errors.Is(err,ErrNodeMeasurementNotAdmitted){
  t.Fatalf("revoked earlier grant became active: %v",err)
 }
 var count int
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_node_measurement_receipts WHERE project_id=?",
  project.ID).Scan(&count);err!=nil||count!=2{
  t.Fatalf("noisy repeated rejections mutated immutable evidence: %d %v",count,err)
 }
 updatedGoal,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil||updatedGoal.Health!="unobserved"{
  t.Fatalf("Node transport was treated as verified device health: %v %+v",err,updatedGoal)
 }
}
