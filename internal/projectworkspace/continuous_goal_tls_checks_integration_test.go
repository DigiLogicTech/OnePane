//go:build integration

package projectworkspace

import(
 "context"
 "crypto/ed25519"
 "crypto/rand"
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
 // A second independently enrolled instrument key is HUMAN-BOUND to
 // this already pinned TLS target. Ed25519 possession does not attest a
 // physical router or authenticate a measurement of that router.
 public,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 _,forger,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 key,err:=svc.ApproveGoalInstrumentKey(ctx,ApproveGoalInstrumentKeyCommand{
  ProjectID:project.ID,GoalID:goal.ID,TLSPinID:pin.ID,
  PublicKey:public,ActorPrincipalID:"owner",
  ExpectedGoalRevision:goal.Revision,ExpectedPinRevision:pin.Revision,
  ExpiresAt:now+8*60000,
 })
 if err!=nil{t.Fatalf("human out-of-band Ed25519 enrollment failed: %v",err)}
 if key.TLSPinID!=pin.ID||key.DeclaredTargetID!=target.ID||
  key.Status!="approved"{t.Fatalf("independent key escaped exact TLS scope: %+v",key)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 issue:=IssueGoalInstrumentChallengeCommand{
  ProjectID:project.ID,GoalID:goal.ID,KeyID:key.ID,ActorPrincipalID:"owner",
  ExpectedGoalRevision:goal.Revision,ExpectedKeyRevision:key.Revision,
 }
 ch,err:=svc.IssueGoalInstrumentChallenge(ctx,issue)
 if err!=nil{t.Fatalf("bounded one-time challenge issuance failed: %v",err)}
 if len(ch.Nonce)!=32||len(ch.SigningPayload)==0||
  ch.ProjectID!=project.ID||ch.GoalID!=goal.ID{
  t.Fatalf("nonce not correctly bound to human approved Goal: %+v",ch)
 }
 accept:=AcceptGoalInstrumentChallengeCommand{
  ProjectID:project.ID,GoalID:goal.ID,KeyID:key.ID,
  ChallengeID:ch.ID,ActorPrincipalID:"owner",
  ExpectedGoalRevision:goal.Revision,
  Signature:ed25519.Sign(forger,ch.SigningPayload),
 }
 if _,err=svc.AcceptGoalInstrumentChallenge(ctx,accept);err==nil{
  t.Fatal("wrong instrument private key forged a signed challenge")
 }
 accept.Signature=ed25519.Sign(private,ch.SigningPayload)
 wrong:=accept;wrong.ProjectID="other-tenant"
 if _,err=svc.AcceptGoalInstrumentChallenge(ctx,wrong);err==nil{
  t.Fatal("signed instrument challenge crossed tenant Project boundary")
 }
 proof,err:=svc.AcceptGoalInstrumentChallenge(ctx,accept)
 if err!=nil{t.Fatalf("real out-of-band signed response rejected: %v",err)}
 if proof.ProofClass!="human_bound_instrument_key_possession"||
  proof.DeclaredTargetID!=target.ID||proof.TLSPinID!=pin.ID||
  proof.DeviceIdentityVerified||proof.RemediationAuthorized{
  t.Fatalf("signed instrument proof promoted physical-device trust: %+v",proof)
 }
 bundle,err:=svc.GetGoalInstrumentVerificationBundle(ctx,project.ID,goal.ID,proof.ID)
 if err!=nil{t.Fatalf("accepted proof lost independent verification material: %v",err)}
 if !bundle.DetachedSignatureVerified||bundle.DeviceIdentityVerified||
  bundle.RemediationAuthorized||
  string(bundle.SigningPayload)!=string(ch.SigningPayload)||
  !ed25519.Verify(ed25519.PublicKey(bundle.PublicKey),
   bundle.SigningPayload,bundle.Signature){
  t.Fatalf("human proof bundle cannot be verified offline: %+v",bundle)
 }
 changed:=append([]byte(nil),bundle.SigningPayload...)
 changed[0]^=1
 if ed25519.Verify(ed25519.PublicKey(bundle.PublicKey),changed,bundle.Signature){
  t.Fatal("tampered instrument signing payload passed independent Ed25519 check")
 }
 signedTriage,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(signedTriage.Sources)!=1||
  signedTriage.Sources[0].InstrumentProofID!=proof.ID||
  signedTriage.Sources[0].InstrumentKeyID!=key.ID||
  signedTriage.Sources[0].InstrumentProofState!="recent_signed_challenge"||
  !signedTriage.Sources[0].InstrumentKeyPossessionProven||
  signedTriage.Sources[0].InstrumentProofClass!="human_bound_instrument_key_possession"||
  signedTriage.RecentNegativeSources!=0||
  signedTriage.CrossClassTargetCorrelation!="not_established"||
  signedTriage.DeviceIdentityVerified||signedTriage.MonitoringActive||
  signedTriage.RemediationAuthorized{
  t.Fatalf("signer-key proof became physical-device identity or incident: %+v",signedTriage)
 }
 if _,err=svc.AcceptGoalInstrumentChallenge(ctx,accept);err==nil{
  t.Fatal("spent Ed25519 challenge was replayed successfully")
 }
 for i:=0;i<3;i++{
  another,issueErr:=svc.IssueGoalInstrumentChallenge(ctx,issue)
  if issueErr!=nil{
   t.Fatalf("bounded instrument challenge %d unexpectedly failed: %v",i,issueErr)
  }
  if i==0{
   // A valid signature over the FIRST challenge is not transferable to
   // the second nonce, even for the same human-approved instrument key.
   crossed:=accept
   crossed.ChallengeID=another.ID
   if _,err=svc.AcceptGoalInstrumentChallenge(ctx,crossed);err==nil{
    t.Fatal("signed response for one nonce was accepted for another challenge")
   }
  }
 }
 if _,err=svc.IssueGoalInstrumentChallenge(ctx,issue);err==nil{
  t.Fatal("fifth instrument challenge bypassed 4 per 5-minute quota")
 }
 proofs,err:=svc.ListGoalInstrumentProofs(ctx,project.ID,goal.ID)
 if err!=nil||len(proofs)!=1||proofs[0].ID!=proof.ID{
  t.Fatalf("instrument proof history was not immutable/readable: %+v %v",proofs,err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  `DELETE FROM project_goal_instrument_proofs WHERE id=?`,proof.ID);err==nil{
  t.Fatal("signed instrument possession proof was deletable")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_goal_instrument_challenges
  SET signing_payload=? WHERE id=?`,[]byte("{}"),ch.ID);err==nil{
  t.Fatal("original signed challenge bytes were mutable")
 }
 var instrumentEvents int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type LIKE 'project.goal_instrument_%'`).Scan(&instrumentEvents);
  err!=nil||instrumentEvents!=7{
  t.Fatalf("instrument key/proof audit lifecycle incomplete: %d %v",
   instrumentEvents,err)
 }
 var leaked int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type LIKE 'project.goal_instrument_%'
   AND CAST(payload_json AS TEXT) LIKE '%192.168.42.71%'`).Scan(&leaked);
  err!=nil||leaked!=0{
  t.Fatalf("instrument challenge audit exposed private endpoint: %d %v",
   leaked,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 revokedKey,err:=svc.RevokeGoalInstrumentKey(ctx,RevokeGoalInstrumentKeyCommand{
  ProjectID:project.ID,GoalID:goal.ID,KeyID:key.ID,ActorPrincipalID:"owner",
  ExpectedGoalRevision:goal.Revision,ExpectedKeyRevision:key.Revision,
 })
 if err!=nil||revokedKey.Status!="revoked"{
  t.Fatalf("human could not revoke independent instrument identity: %+v %v",revokedKey,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 issue.ExpectedGoalRevision=goal.Revision
 if _,err=svc.IssueGoalInstrumentChallenge(ctx,issue);err==nil{
  t.Fatal("revoked instrument key minted a new challenge")
 }
 if _,err=svc.AcceptGoalInstrumentChallenge(ctx,accept);err==nil{
  t.Fatal("revoked key admitted old signed response")
 }
 historical,err:=svc.GetGoalInstrumentVerificationBundle(ctx,project.ID,goal.ID,proof.ID)
 if err!=nil||!historical.DetachedSignatureVerified||
  !ed25519.Verify(ed25519.PublicKey(historical.PublicKey),
   historical.SigningPayload,historical.Signature){
  t.Fatalf("revocation destroyed independently auditable signed proof: %+v %v",
   historical,err)
 }
 removedTriage,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(removedTriage.Sources)!=1||
  removedTriage.Sources[0].InstrumentKeyPossessionProven||
  removedTriage.Sources[0].InstrumentProofID!=""{
  t.Fatalf("revoked key still displayed as current possession: %+v",removedTriage)
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
