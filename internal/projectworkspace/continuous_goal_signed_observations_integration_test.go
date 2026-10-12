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
 proof2,err:=accept(second,2,secondSig)
 if err!=nil||proof2.Sequence!=2{t.Fatalf("new ordered proof: %+v %v",proof2,err)}
 coverage,err:=svc.GoalEvidenceCoverage(ctx,p.ID,g.ID)
 if err!=nil||coverage.MonitoringActive||coverage.RecentSourceSignedSources!=1||
  len(coverage.Sources)!=1||coverage.Sources[0].EvidenceState!="missing"||
  coverage.Sources[0].SignatureEvidenceState!="recent_source_signature"{
  t.Fatalf("signed source was conflated with manually attached receipts or device health: %+v %v",coverage,err)
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
}
