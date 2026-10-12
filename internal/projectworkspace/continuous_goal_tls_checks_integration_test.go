//go:build integration

package projectworkspace

import(
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestHumanPinnedTLSEndpointMatchedCertificateIsAuditedNeverDeviceHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tls-attestation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,statement:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,statement);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Bounded cert possession",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"TLS",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,
  Objective:"Check pinned TLS cert only",ActorPrincipalID:"owner",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.42.71",TargetPort:443,TimeoutMS:1300,
  ExpiresAt:now+30*60000,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 target,err:=svc.DeclareGoalTCPTarget(ctx,DeclareGoalTCPTargetCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  DisplayLabel:"Router socket",ExpiresAt:now+20*60000,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 server:=httptest.NewTLSServer(nil)
 defer server.Close()
 rawFingerprint:=sha256.Sum256(server.Certificate().Raw)
 fingerprint:="sha256:"+hex.EncodeToString(rawFingerprint[:])
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 pin,err:=svc.ApproveGoalTLSPin(ctx,ApproveGoalTLSPinCommand{
  ProjectID:project.ID,GoalID:goal.ID,DeclaredTargetID:target.ID,
  CertificateSHA256:fingerprint,ActorPrincipalID:"owner",
  ExpiresAt:now+10*60000,ExpectedGoalRevision:goal.Revision,
  ExpectedTargetRevision:target.Revision,
 })
 if err!=nil{t.Fatal(err)}
 if pin.PermitID!=permit.ID||pin.DeclaredTargetID!=target.ID{
  t.Fatalf("TLS pin did not capture exact permit: %+v",pin)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 command:=RunGoalPinnedTLSCheckCommand{
  ProjectID:project.ID,GoalID:goal.ID,PinID:pin.ID,
  ActorPrincipalID:"owner",ExpectedGoalRevision:goal.Revision,
  ExpectedPinRevision:pin.Revision,
 }
 dialer:=&testTLSPinDialer{
  target:"192.168.42.71:443",address:server.Listener.Addr().String(),
 }
 check,err:=svc.runGoalPinnedTLSCheck(ctx,command,dialer)
 if err!=nil{t.Fatalf("out-of-band pinned TLS check rejected: %v",err)}
 if check.Status!="matched"||!check.CertificatePossessionMatched||
  check.DeviceIdentityVerified||check.MonitoringActive||dialer.called!=1{
  t.Fatalf("matched certificate became trusted device or did not dial: %+v calls=%d",
   check,dialer.called)
 }
 history,err:=svc.ListGoalPinnedTLSChecks(ctx,project.ID,goal.ID)
 if err!=nil||len(history)!=1||history[0].ID!=check.ID||
  history[0].Status!="matched"{t.Fatalf("pin proof not durably listed: %+v %v",history,err)}
 triage,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(triage.Sources)!=1||triage.Sources[0].TLSCheckID!=check.ID||
  triage.Sources[0].TLSPinID!=pin.ID||
  triage.Sources[0].TLSCheckState!="matched"||
  !triage.Sources[0].TLSCertificatePossessionMatched||
  triage.Sources[0].TLSIdentityClass!="operator_pinned_tls_certificate"||
  triage.CrossClassTargetCorrelation!="not_established"||
  triage.DeviceIdentityVerified||triage.MonitoringActive||
  triage.RemediationAuthorized||triage.RecentNegativeSources!=0{
  t.Fatalf("certificate possession became device security/incident: %+v",triage)
 }
 if _,err=svc.runGoalPinnedTLSCheck(ctx,command,dialer);err==nil{
  t.Fatal("a second 5-minute TLS check bypassed resource budget")
 }
 if dialer.called!=1{
  t.Fatalf("second denied TLS claim nonetheless opened another socket: %d",dialer.called)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 revoked,err:=svc.RevokeGoalTLSPin(ctx,RevokeGoalTLSPinCommand{
  ProjectID:project.ID,GoalID:goal.ID,PinID:pin.ID,
  ActorPrincipalID:"owner",ExpectedGoalRevision:goal.Revision,
  ExpectedPinRevision:pin.Revision,
 })
 if err!=nil||revoked.Status!="revoked"{
  t.Fatalf("pin revoke failed: %+v %v",revoked,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 command.ExpectedGoalRevision=goal.Revision
 command.ExpectedPinRevision=revoked.Revision
 if _,err=svc.runGoalPinnedTLSCheck(ctx,command,dialer);err==nil{
  t.Fatal("revoked pin dispatched a network connection")
 }
 if dialer.called!=1{t.Fatalf("revoked pin opened TLS socket: %d",dialer.called)}
 revokedTriage,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(revokedTriage.Sources)!=1||
  revokedTriage.Sources[0].TLSCertificatePossessionMatched||
  revokedTriage.Sources[0].TLSCheckState!="no_current_approved_check"{
  t.Fatalf("revoked pin remained trusted in current triage: %+v",revokedTriage)
 }
 var count int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tls_endpoint_checks
  WHERE pin_id=?`,pin.ID).Scan(&count);err!=nil||count!=1{
  t.Fatalf("replay/revocation appended extra check: %d %v",count,err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_goal_tls_endpoint_checks
  SET status='mismatched' WHERE id=?`,check.ID);err==nil{
  t.Fatal("immutable completed matched TLS receipt was rewritten")
 }
 if _,err=db.SQL().ExecContext(ctx,`DELETE FROM project_goal_tls_endpoint_checks
  WHERE id=?`,check.ID);err==nil{
  t.Fatal("completed TLS receipt was deleted")
 }
 var leakCount int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type LIKE 'project.goal_tls_%'
   AND CAST(payload_json AS TEXT) LIKE ?`,
  "%192.168.42.71%").Scan(&leakCount);err!=nil||leakCount!=0{
  t.Fatalf("TLS event disclosed private endpoint: %d %v",leakCount,err)
 }
 var events int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type IN ('project.goal_tls_endpoint_pin_approved',
   'project.goal_tls_endpoint_check_claimed',
   'project.goal_tls_endpoint_check_finished',
   'project.goal_tls_endpoint_pin_revoked')`).Scan(&events);err!=nil||events!=4{
  t.Fatalf("TLS audit lifecycle incomplete: %d %v",events,err)
 }
 if strings.Contains(string(jsonMarshal(check)),"192.168.42.71"){
  t.Fatal("response leaked exact target IP")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Health!="unobserved"||goal.Status!="configured"||
  goal.LastVerifiedAt!=0{
  t.Fatalf("pin certificate result falsely activated Goal health: %+v",goal)
 }
}

func jsonMarshal(v any)[]byte{
 b,_:=json.Marshal(v)
 return b
}
