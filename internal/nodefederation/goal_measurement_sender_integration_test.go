//go:build integration

package nodefederation

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestExplicitNodeSenderUsesPinnedTLSAndDurableSequenceAcrossRestart(t *testing.T){
 ctx:=context.Background()
 hubDB,now:=seedNodeDB(t,"hub","hub-identity")
 defer hubDB.Close()
 peerDB,_:=seedNodeDB(t,"peer","peer-identity")
 defer peerDB.Close()
 hubIdentity,err:=EnsureIdentity(t.TempDir(),"hub")
 if err!=nil{t.Fatal(err)}
 peerIdentity,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 for _,query:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
 }{
  if _,err=hubDB.SQL().ExecContext(ctx,query);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(hubDB.SQL(),hubDB,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Authenticated peer sender",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Receiver",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=projects.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"owner",
  Objective:"Receive exact peer connectivity",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["peer-link"]}`),
 })
 if err!=nil{t.Fatal(err)}
 // Receiver's live pairing binds the exact sender cert.
 _,err=hubDB.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,revision,created_at,updated_at)
  VALUES('peer','Origin peer',0,'origin-peer','paired','PAIRED_MTLS',
   '{}','{}',1,?,?)`,now,now)
 if err!=nil{t.Fatal(err)}
 _,err=hubDB.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at)
  VALUES('pair-origin','peer','inbound','paired','token','hash','000000',
   'hash',1,1,?,?,?, ?,1,?,?)`,
  peerIdentity.Fingerprint,string(peerIdentity.CertPEM),
  now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 grant,err:=projects.ApproveGoalNodeMeasurementGrant(ctx,
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:project.ID,GoalID:goal.ID,PeerNodeID:"peer",
   SourceID:"peer-link",ActorPrincipalID:"owner",
   ExpectedGoalRevision:goal.Revision,ExpiresAt:now+30*60000,
  })
 if err!=nil{t.Fatal(err)}
 receiverSvc:=NewService(hubDB.SQL(),hubDB,clock.Real{},
  NodeView{ID:"hub",Local:true},hubIdentity,"",30*time.Second)
 receiver:=NewServer(receiverSvc,nil)
 live:=httptest.NewUnstartedServer(receiver.Handler())
 live.TLS=receiver.TLSConfig()
 live.StartTLS()
 defer live.Close()
 // Sender's independent database stores the receiver's TLS certificate.
 _,err=peerDB.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  endpoint_json,protocol_json,capabilities_json,revision,created_at,updated_at)
  VALUES('hub','Destination',0,'hub-identity','paired','PAIRED_MTLS',
   ?,'{}','{}',1,?,?)`,
  endpointJSON(live.URL,hubIdentity.Fingerprint),now,now)
 if err!=nil{t.Fatal(err)}
 _,err=peerDB.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at)
  VALUES('pair-hub','hub','outbound','paired','token','hash','000000',
   'hash',1,1,?,?,?, ?,1,?,?)`,
  hubIdentity.Fingerprint,string(hubIdentity.CertPEM),
  now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 service:=NewService(peerDB.SQL(),peerDB,clock.Real{},
  NodeView{ID:"peer",Local:true},peerIdentity,"",30*time.Second)
 command:=SendPeerNodeMeasurementCommand{
  RecipientNodeID:"hub",GrantID:grant.ID,Outcome:"reachable",
 }
 first,err:=service.SendPeerNodeMeasurement(ctx,command)
 if err!=nil{t.Fatalf("real pinned Node-to-Node self-report failed: %v",err)}
 if first.Sequence!=1||first.PeerNodeID!="peer"||
  first.GoalID!=goal.ID||first.TransportKind!="pinned_peer_mtls"{
  t.Fatalf("first sender receipt is not correctly authenticated: %+v",first)
 }
 // Restart the local Node service. Sequence 1 is durably spent.
 service=NewService(peerDB.SQL(),peerDB,clock.Real{},
  NodeView{ID:"peer",Local:true},peerIdentity,"",30*time.Second)
 second,err:=service.SendPeerNodeMeasurement(ctx,command)
 if err!=nil{t.Fatalf("post-restart sender lost durable grant: %v",err)}
 if second.Sequence!=2||second.ID==first.ID{
  t.Fatalf("Node sender reused persisted sequence: %+v %+v",first,second)
 }
 // Revocation is live at the receiver; a failed send still spends a local
 // sequence, preventing reuse even after an unknown transport outcome.
 goal,err=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 _,err=projects.RevokeGoalNodeMeasurementGrant(ctx,
  projectworkspace.RevokeGoalNodeMeasurementGrantCommand{
   ProjectID:project.ID,GoalID:goal.ID,GrantID:grant.ID,
   ActorPrincipalID:"owner",ExpectedGoalRevision:goal.Revision,
   ExpectedGrantRevision:3,
  })
 if err!=nil{t.Fatal(err)}
 if _,err=service.SendPeerNodeMeasurement(ctx,command);err==nil{
  t.Fatal("sender bypassed human grant revocation")
 }
 var sequence int64
 err=peerDB.SQL().QueryRowContext(ctx,`SELECT sequence FROM
  node_outbound_measurement_sequences
  WHERE local_node_id='peer' AND peer_node_id='hub' AND grant_id=?`,
  grant.ID).Scan(&sequence)
 if err!=nil||sequence!=3{
  t.Fatalf("failed transfer reused or lost durable sender sequence: %d %v",sequence,err)
 }
 var count int
 err=hubDB.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM
  project_goal_node_measurement_receipts WHERE grant_id=?`,
  grant.ID).Scan(&count)
 if err!=nil||count!=2{t.Fatalf("revoked mTLS transfer persisted: %d %v",count,err)}
 goal,err=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Status!="configured"||goal.Health!="unobserved"{
  t.Fatalf("Node self-report enabled Goal without incident verification: %+v",goal)
 }
}
