//go:build integration

package projectworkspace

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestLevel5SignedObservationEnrollmentAntiReplayAndLiveRevocation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/signed-goal.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('source','service','Sensor','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','service','Other','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','source','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('foreign','outsider','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Observe authorised source",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Evidence",ActorPrincipalID:"owner"})
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Authenticate approved local sensor readings",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 expiry:=clock.Real{}.UnixMilli()+60*60*1000
 signer,err:=svc.ApproveGoalObservationSigner(ctx,ApproveGoalObservationSignerCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",SourcePrincipalID:"source",
  ActorPrincipalID:"owner",PublicKey:pub,ExpectedGoalRevision:g.Revision,ExpiresAt:expiry,
 })
 if err!=nil{t.Fatal(err)}
 if signer.Status!="approved"||signer.LastSequence!=0{t.Fatalf("bad signer %+v",signer)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 // An existing active signer cannot silently be replaced by another key.
 altPub,_,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 if _,err=svc.ApproveGoalObservationSigner(ctx,ApproveGoalObservationSignerCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",SourcePrincipalID:"source",
  ActorPrincipalID:"owner",PublicKey:altPub,ExpectedGoalRevision:g.Revision,ExpiresAt:expiry,
 });err==nil{t.Fatal("second active key admitted")}
 obsService:=observation.NewService(db.SQL(),db,clock.Real{})
 record:=func(sourceID string)observation.Observation{
  t.Helper()
  principal:="source"
  o,err:=obsService.Record(ctx,observation.RecordCommand{
   WorkspaceID:"tenant",SubjectRef:"sensor://private",
   ObservationType:"system.sample",ProbeToolID:sourceID,ProbeToolVersion:"1",
   SourcePrincipalID:&principal,ActorPrincipalID:&principal,
   Value:json.RawMessage(`{"secret":"NEVER_COPY_RAW_VALUE","status":"ok"}`),
   Label:policy.DataLabel{WorkspaceID:"tenant",
    Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,
    Trust:policy.TrustUnverifiedDerived},
  })
  if err!=nil{t.Fatal(err)}
  return o
 }
 first:=record("router")
 sign:=func(obs observation.Observation,key ed25519.PrivateKey,seq int64)[]byte{
  t.Helper()
  msg,err:=SignedGoalObservationMessage(p.ID,g.ID,signer.ID,obs.ID,obs.IntegrityHash,seq)
  if err!=nil{t.Fatal(err)}
  return ed25519.Sign(key,msg)
 }
 accept:=func(obs observation.Observation,seq int64,sig []byte)(SignedGoalObservationProof,error){
  return svc.AcceptSignedGoalObservation(ctx,AcceptSignedGoalObservationCommand{
   ProjectID:p.ID,GoalID:g.ID,SignerID:signer.ID,ObservationID:obs.ID,
   ActorPrincipalID:"source",ExpectedGoalRevision:g.Revision,Sequence:seq,Signature:sig,
  })
 }
 firstSig:=sign(first,priv,1)
 broken:=append([]byte(nil),firstSig...)
 broken[0]^=0x10
 if _,err=accept(first,1,broken);err==nil{t.Fatal("invalid signature admitted")}
 incorrect:=record("unapproved")
 if _,err=accept(incorrect,1,sign(incorrect,priv,1));err==nil{
  t.Fatal("signed unapproved source was accepted")
 }
 outsider:="outsider"
 foreign,err:=obsService.Record(ctx,observation.RecordCommand{
  WorkspaceID:"foreign",SubjectRef:"sensor://foreign",
  ObservationType:"system.sample",ProbeToolID:"router",ProbeToolVersion:"1",
  SourcePrincipalID:&outsider,ActorPrincipalID:&outsider,
  Value:json.RawMessage(`{"status":"ok"}`),
  Label:policy.DataLabel{WorkspaceID:"foreign",
   Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyAny,Trust:policy.TrustUnverifiedDerived},
 })
 if err!=nil{t.Fatal(err)}
 if _,err=accept(foreign,1,sign(foreign,priv,1));err==nil{
  t.Fatal("valid cryptographic signature upgraded foreign Workspace evidence")
 }
 accepted,err:=accept(first,1,firstSig)
 if err!=nil{t.Fatal(err)}
 if accepted.Sequence!=1||accepted.SourceID!="router"||accepted.IntegrityHash!=first.IntegrityHash{
  t.Fatalf("signed proof wrong %+v",accepted)
 }
 payload,_:=json.Marshal(accepted)
 if bytes.Contains(payload,[]byte("NEVER_COPY_RAW_VALUE"))||
  bytes.Contains(payload,[]byte("sensor://private")){t.Fatal("signed proof leaked private value")}
 if _,err=accept(first,1,firstSig);err==nil{t.Fatal("signature replay succeeded")}
 second:=record("router")
 if _,err=accept(second,1,sign(second,priv,1));err==nil{
  t.Fatal("same signer sequence reused for different observation")
 }
 // A signature for one goal cannot be reinterpreted for a second goal.
 wrongMsg,err:=SignedGoalObservationMessage(p.ID,"another-goal",signer.ID,
  second.ID,second.IntegrityHash,2)
 if err!=nil{t.Fatal(err)}
 if _,err=accept(second,2,ed25519.Sign(priv,wrongMsg));err==nil{
  t.Fatal("cross-goal signature accepted")
 }
 secondSig:=sign(second,priv,2)
 // Live revocation and identity-type changes override a valid signature
 // at the final durable admission boundary, without consuming sequence 2.
 for _,principal:=range []string{"source","owner"}{
  if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
   SET status='revoked' WHERE workspace_id='tenant' AND principal_id=?`,principal);err!=nil{t.Fatal(err)}
  if _,err=accept(second,2,secondSig);!errors.Is(err,ErrPrincipalIneligible){
   t.Fatalf("revoked %s was accepted as source authority: %v",principal,err)
  }
  if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships
   SET status='active' WHERE workspace_id='tenant' AND principal_id=?`,principal);err!=nil{t.Fatal(err)}
 }
 for _,entry:=range []struct{id,kind string}{
  {"source","human"},{"owner","agent"},
 }{
  if _,err=db.SQL().ExecContext(ctx,`UPDATE principals
   SET principal_type=? WHERE id=?`,entry.kind,entry.id);err!=nil{t.Fatal(err)}
  if _,err=accept(second,2,secondSig);!errors.Is(err,ErrPrincipalIneligible){
   t.Fatalf("demoted %s was accepted as valid source/approver: %v",entry.id,err)
  }
  restored:="service"
  if entry.id=="owner"{restored="human"}
  if _,err=db.SQL().ExecContext(ctx,`UPDATE principals
   SET principal_type=? WHERE id=?`,restored,entry.id);err!=nil{t.Fatal(err)}
 }
 proof2,err:=accept(second,2,secondSig)
 if err!=nil||proof2.Sequence!=2{t.Fatalf("new ordered proof: %+v %v",proof2,err)}
 coverage,err:=svc.GoalEvidenceCoverage(ctx,p.ID,g.ID)
 if err!=nil||coverage.MonitoringActive||coverage.RecentSourceSignedSources!=1||
  len(coverage.Sources)!=1||coverage.Sources[0].EvidenceState!="missing"||
  coverage.Sources[0].SignatureEvidenceState!="recent_source_signature"{
  t.Fatalf("signed source was conflated with manually attached receipts or device health: %+v %v",coverage,err)
 }
 history,err:=svc.ListGoalSignedObservationProofs(ctx,p.ID,g.ID)
 if err!=nil||len(history)!=2||history[0].SourceID!="router"{
  t.Fatalf("signed evidence metadata readback unavailable: %+v %v",history,err)
 }
 listed,_:=json.Marshal(history)
 if bytes.Contains(listed,[]byte("NEVER_COPY_RAW_VALUE"))||
  bytes.Contains(listed,[]byte("sensor://private")){
  t.Fatal("signed evidence metadata readback leaked sensor values")
 }
 if _,err=svc.ListGoalSignedObservationProofs(ctx,"another-project",g.ID);err==nil{
  t.Fatal("cross-Project signed proof history disclosed")
 }
 // Revocation is allowed at the current signer revision after sequence use.
 revoked,err:=svc.RevokeGoalObservationSigner(ctx,RevokeGoalObservationSignerCommand{
  ProjectID:p.ID,GoalID:g.ID,SignerID:signer.ID,ActorPrincipalID:"owner",
  ExpectedGoalRevision:g.Revision,ExpectedSignerRevision:3,
 })
 if err!=nil||revoked.Status!="revoked"||revoked.LastSequence!=2{
  t.Fatalf("revocation failed: %+v %v",revoked,err)
 }
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 coverage,err=svc.GoalEvidenceCoverage(ctx,p.ID,g.ID)
 if err!=nil||coverage.RecentSourceSignedSources!=0||
  len(coverage.Sources)!=1||
  coverage.Sources[0].SignatureEvidenceState!="historical_key_inactive"{
  t.Fatalf("revoked source still presented as currently approved: %+v %v",coverage,err)
 }
 third:=record("router")
 if _,err=accept(third,3,sign(third,priv,3));!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("revoked key accepted new signed proof: %v",err)
 }
 var proofs,manualReceipts,tasks int
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_signed_observation_proofs").Scan(&proofs);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_observation_receipts").Scan(&manualReceipts);err!=nil{t.Fatal(err)}
 if err=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil{t.Fatal(err)}
 if proofs!=2||manualReceipts!=0||tasks!=0{
  t.Fatalf("signed admission started other workflows: proofs=%d manual=%d tasks=%d",
   proofs,manualReceipts,tasks)
 }
 // Stored proof and key provenance cannot be reinterpreted or deleted.
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_signed_observation_proofs SET sequence=99 WHERE id=?",accepted.ID);err==nil{
  t.Fatal("immutable proof rewritten")
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_observation_signers SET status='approved',revision=revision+1 WHERE id=?",signer.ID);err==nil{
  t.Fatal("revoked signer revived")
 }
 var eventCount int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='project.goal_observation_source_signature_accepted'
   AND workspace_id='tenant'`).Scan(&eventCount);err!=nil||eventCount!=2{
  t.Fatalf("missing tenant scoped signed evidence events: %d %v",eventCount,err)
 }
 current,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("signed source keys falsely activated monitoring %+v",current)
 }
 // Emergency rotation creates a NEW signer identity and a fresh sequence
 // namespace; the revoked signer remains immutable and cannot regain
 // rights by replaying an old key or by registering its prior proof.
 newPub,newPriv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 rotated,err:=svc.ApproveGoalObservationSigner(ctx,ApproveGoalObservationSignerCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",SourcePrincipalID:"source",
  ActorPrincipalID:"owner",PublicKey:newPub,ExpiresAt:expiry,
  ExpectedGoalRevision:current.Revision,
 })
 if err!=nil{t.Fatalf("source key rotation failed: %v",err)}
 if rotated.ID==signer.ID||rotated.LastSequence!=0{
  t.Fatalf("rotated key reused revoked identity %+v",rotated)
 }
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 rotatedObservation:=record("router")
 rotatedMessage,err:=SignedGoalObservationMessage(p.ID,g.ID,rotated.ID,
  rotatedObservation.ID,rotatedObservation.IntegrityHash,1)
 if err!=nil{t.Fatal(err)}
 rotatedProof,err:=svc.AcceptSignedGoalObservation(ctx,AcceptSignedGoalObservationCommand{
  ProjectID:p.ID,GoalID:g.ID,SignerID:rotated.ID,
  ObservationID:rotatedObservation.ID,ActorPrincipalID:"source",
  Sequence:1,ExpectedGoalRevision:g.Revision,
  Signature:ed25519.Sign(newPriv,rotatedMessage),
 })
 if err!=nil||rotatedProof.SignerID!=rotated.ID{
  t.Fatalf("new key did not admit independently signed proof: %+v %v",rotatedProof,err)
 }
 oldSigner,err:=scanGoalObservationSigner(db.SQL().QueryRowContext(ctx,
  `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
   WHERE id=?`,signer.ID))
 if err!=nil||oldSigner.Status!="revoked"||oldSigner.LastSequence!=2{
  t.Fatalf("rotation corrupted previous immutable signer state %+v %v",oldSigner,err)
 }
 history,err=svc.ListGoalSignedObservationProofs(ctx,p.ID,g.ID)
 if err!=nil||len(history)!=3{t.Fatalf("rotation discarded history: %+v %v",history,err)}
 // A malfunctioning credential/key may have authority to create signed
 // proofs but must not exhaust a low-resource local audit ledger.
 for seq:=int64(2);seq<=30;seq++{
  sample:=record("router")
  msg,err:=SignedGoalObservationMessage(p.ID,g.ID,rotated.ID,
   sample.ID,sample.IntegrityHash,seq)
  if err!=nil{t.Fatal(err)}
  _,err=svc.AcceptSignedGoalObservation(ctx,AcceptSignedGoalObservationCommand{
   ProjectID:p.ID,GoalID:g.ID,SignerID:rotated.ID,ObservationID:sample.ID,
   ActorPrincipalID:"source",Sequence:seq,
   ExpectedGoalRevision:g.Revision,Signature:ed25519.Sign(newPriv,msg),
  })
  if err!=nil{t.Fatalf("bounded signer receipt %d rejected: %v",seq,err)}
 }
 overQuota:=record("router")
 overMsg,err:=SignedGoalObservationMessage(p.ID,g.ID,rotated.ID,
  overQuota.ID,overQuota.IntegrityHash,31)
 if err!=nil{t.Fatal(err)}
 _,err=svc.AcceptSignedGoalObservation(ctx,AcceptSignedGoalObservationCommand{
  ProjectID:p.ID,GoalID:g.ID,SignerID:rotated.ID,
  ObservationID:overQuota.ID,ActorPrincipalID:"source",
  Sequence:31,ExpectedGoalRevision:g.Revision,
  Signature:ed25519.Sign(newPriv,overMsg),
 })
 if !errors.Is(err,ErrSourceProofRateLimited){
  t.Fatalf("31st same-minute signer proof ignored persisted budget: %v",err)
 }
 saved,err:=scanGoalObservationSigner(db.SQL().QueryRowContext(ctx,
  `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
   WHERE id=?`,rotated.ID))
 if err!=nil||saved.LastSequence!=30{
  t.Fatalf("rate-denied proof consumed signer sequence: %+v %v",saved,err)
 }
}
