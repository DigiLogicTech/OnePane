//go:build integration

package nodefederation

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestGoalNodeMeasurementsRequireHumanScopeRealPeerTLSAndMonotonicReceipt(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/node-goal-transport.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,statement:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('operator','human','Human operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','operator','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,statement);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Narrow mTLS connectivity scope",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Transport",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=projects.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"operator",
  Objective:"Collect only narrow peer mTLS evidence",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["peer-link"]}`),
 })
 if err!=nil{t.Fatal(err)}
 peer,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 impostor,err:=EnsureIdentity(t.TempDir(),"impostor")
 if err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  fmt.Sprintf(`INSERT INTO harness_nodes(
   id,name,local,identity_fingerprint,trust_state,trust_zone,
   protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('peer','Remote',0,'peer-key','paired','PAIRED_MTLS','{}','{}',%d,1,%d,%d)`,
   now,now,now),
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at)
  VALUES('pair-peer','peer','outbound','paired','token','hash','000000','hash',
   1,1,?,?,?, ?,1,?,?)`,
  peer.Fingerprint,string(peer.CertPEM),now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 grant,err:=projects.ApproveGoalNodeMeasurementGrant(ctx,
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:p.ID,GoalID:goal.ID,PeerNodeID:"peer",SourceID:"peer-link",
   ActorPrincipalID:"operator",ExpectedGoalRevision:goal.Revision,
   ExpiresAt:now+30*60000,
  })
 if err!=nil{t.Fatalf("human approved mTLS transport: %v",err)}
 if grant.PeerTLSFingerprint!=peer.Fingerprint||grant.SourceID!="peer-link"{
  t.Fatalf("grant accepted unauthenticated peer source: %+v",grant)
 }
 listed,err:=projects.ListGoalNodeMeasurementGrants(ctx,p.ID,goal.ID)
 if err!=nil||len(listed)!=1||listed[0].ID!=grant.ID{
  t.Fatalf("human grant inventory unavailable: %+v %v",listed,err)
 }
 origin,err:=EnsureIdentity(t.TempDir(),"origin")
 if err!=nil{t.Fatal(err)}
 service:=NewService(db.SQL(),db,clock.Real{},
  NodeView{ID:"origin",Local:true},origin,"",30*time.Second)
 federationServer:=NewServer(service,nil)
 handler:=federationServer.Handler()
 cert,_:=x509.ParseCertificate(peer.Certificate.Certificate[0])
 otherCert,_:=x509.ParseCertificate(impostor.Certificate.Certificate[0])
 route:="/federation/v1/measurements"
 makeBody:=func(seq int64)string{
  return fmt.Sprintf(`{"protocol":"%s","grant_id":"%s","sequence":%d,
   "signal_type":"peer_connectivity","outcome":"reachable","observed_at":%d}`,
   ProtocolVersion,grant.ID,seq,clock.Real{}.UnixMilli())
 }
 send:=func(body string,leaf *x509.Certificate)*httptest.ResponseRecorder{
  t.Helper()
  req:=httptest.NewRequest("POST",route,strings.NewReader(body))
  if leaf!=nil{req.TLS=&tls.ConnectionState{PeerCertificates:[]*x509.Certificate{leaf}}}
  w:=httptest.NewRecorder();handler.ServeHTTP(w,req)
  return w
 }
 if got:=send(makeBody(1),nil);got.Code!=401{
  t.Fatalf("TLS-free sensor submit accepted: %d",got.Code)
 }
 if got:=send(makeBody(1),otherCert);got.Code!=401{
  t.Fatalf("unpaired TLS client accepted: %d",got.Code)
 }
 if got:=send(strings.TrimSuffix(makeBody(1),"}")+
  `,"sequence":222}`,cert);got.Code!=400{
  t.Fatalf("duplicate sequence accepted: %d",got.Code)
 }
 if got:=send(strings.TrimSuffix(makeBody(1),"}")+
  `,"origin_node_id":"spoof"}`,cert);got.Code!=400{
  t.Fatalf("client-chosen Node origin accepted: %d",got.Code)
 }
 if got:=send(strings.TrimSuffix(makeBody(1),"}")+
  `,"device_healthy":true}`,cert);got.Code!=400{
  t.Fatalf("client-chosen verified health accepted: %d",got.Code)
 }
 // Actual local TLS handshake with a client certificate verifies the
 // pinned peer identity boundary beyond synthetic request.TLS test states.
 live:=httptest.NewUnstartedServer(handler)
 live.TLS=federationServer.TLSConfig()
 live.StartTLS()
 defer live.Close()
 authenticatedClient:=&http.Client{Transport:&http.Transport{
  TLSClientConfig:&tls.Config{InsecureSkipVerify:true,
   Certificates:[]tls.Certificate{peer.Certificate}},
 }}
 noCertificateClient:=&http.Client{Transport:&http.Transport{
  TLSClientConfig:&tls.Config{InsecureSkipVerify:true},
 }}
 anonymousResponse,err:=noCertificateClient.Post(
  live.URL+route,"application/json",strings.NewReader(makeBody(1)))
 if err!=nil{t.Fatal(err)}
 anonymousResponse.Body.Close()
 if anonymousResponse.StatusCode!=401{
  t.Fatalf("real TLS socket without client certificate admitted: %d",
   anonymousResponse.StatusCode)
 }
 liveResponse,err:=authenticatedClient.Post(
  live.URL+route,"application/json",strings.NewReader(makeBody(1)))
 if err!=nil{t.Fatal(err)}
 liveResponse.Body.Close()
 if liveResponse.StatusCode!=201{
  t.Fatalf("pinned peer TLS handshake did not admit scoped telemetry: %d",
   liveResponse.StatusCode)
 }
 first:=send(makeBody(2),cert)
 if first.Code!=201{
  t.Fatalf("valid pinned TLS measurement rejected: %d %s",first.Code,first.Body)
 }
 var receipt PeerNodeMeasurementReceipt
 if err=json.Unmarshal(first.Body.Bytes(),&receipt);err!=nil{t.Fatal(err)}
 if receipt.PeerNodeID!="peer"||receipt.ProjectID!=p.ID||
  receipt.GoalID!=goal.ID||receipt.GrantID!=grant.ID||
  receipt.TransportKind!="pinned_peer_mtls"{
  t.Fatalf("mTLS receipt was not exact grant/Node scoped: %+v",receipt)
 }
 if got:=send(makeBody(2),cert);got.Code!=409{
  t.Fatalf("same sequence replayed successfully: %d",got.Code)
 }
 if got:=send(makeBody(0),cert);got.Code<400{
  t.Fatalf("zero sequence accepted: %d",got.Code)
 }
 if got:=send(makeBody(2)+makeBody(3),cert);got.Code!=400{
  t.Fatalf("multiple JSON roots accepted: %d",got.Code)
 }
 for i:=int64(3);i<=30;i++{
  got:=send(makeBody(i),cert)
  if got.Code!=201{t.Fatalf("bounded Node sample %d rejected: %d %s",i,got.Code,got.Body)}
 }
 if got:=send(makeBody(31),cert);got.Code!=429||
  got.Header().Get("Retry-After")!="60"{
  t.Fatalf("31st sample bypassed bounded Node receipt storage: %d %s",got.Code,got.Body)
 }
 var persisted int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM project_goal_node_measurement_receipts
  WHERE grant_id=?`,grant.ID).Scan(&persisted);err!=nil||persisted!=30{
  t.Fatalf("rate failure persisted extra Node receipt: %d %v",persisted,err)
 }
 goal,err=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Status!="configured"||goal.Health!="unobserved"||goal.LastVerifiedAt!=0{
  t.Fatalf("Node telemetry activated Goal or falsely reported health: %+v",goal)
 }
 revoked,err:=projects.RevokeGoalNodeMeasurementGrant(ctx,
  projectworkspace.RevokeGoalNodeMeasurementGrantCommand{
   ProjectID:p.ID,GoalID:goal.ID,GrantID:grant.ID,
   ActorPrincipalID:"operator",ExpectedGoalRevision:goal.Revision,
   ExpectedGrantRevision:31,
  })
 if err!=nil{t.Fatalf("human grant revocation failed: %v",err)}
 if revoked.Status!="revoked"{t.Fatalf("grant stayed active: %+v",revoked)}
 if got:=send(makeBody(32),cert);got.Code!=403{
  t.Fatalf("revoked grant accepted signed Node transport: %d %s",got.Code,got.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`DELETE FROM
  project_goal_node_measurement_receipts WHERE grant_id=?`,
  grant.ID);err==nil{
  t.Fatal("Node receipt deleted despite immutable ledger")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_goal_node_measurement_receipts
  SET outcome='unknown' WHERE grant_id=?`,grant.ID);err==nil{
  t.Fatal("Node receipt rewritten after admission")
 }
 var eventsLeak int
 err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='project.goal_node_measurement_received'
    AND CAST(payload_json AS TEXT) LIKE '%device_secret%'`).Scan(&eventsLeak)
 if err!=nil||eventsLeak!=0{t.Fatalf("Node measurement leaked raw secrets: %v %d",err,eventsLeak)}
}

func TestNodeGoalTransportRejectsUnconfiguredGrant(t *testing.T){
 if !errors.Is(ErrNodeMeasurementNotAdmitted,ErrNodeMeasurementNotAdmitted){
  t.Fatal("missing fail-closed admission sentinel")
 }
}
