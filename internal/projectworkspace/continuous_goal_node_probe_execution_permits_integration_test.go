//go:build integration

package projectworkspace

import (
 "context"
 "errors"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestHumanApprovedNodeExecutionPermitIsOneChallengeOnly(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/node-exec.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Agent','active',1,1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)",
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"One permitted Node",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage("{\"onepane_operations\":{\"version\":1,\"mode\":\"continuous_operations\"}}"),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Private router",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"human",
  Objective:"One approved socket",
  MonitoringPlanJSON:json.RawMessage("{\"version\":1,\"source_ids\":[\"router\"]}"),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 tcp,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.42.71",TargetPort:443,TimeoutMS:900,
  ExpiresAt:now+30*60000,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 target,err:=svc.DeclareGoalTCPTarget(ctx,DeclareGoalTCPTargetCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:tcp.ID,DisplayLabel:"Router socket",
  ExpiresAt:now+20*60000,ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:tcp.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 pin,err:=svc.ApproveGoalTLSPin(ctx,ApproveGoalTLSPinCommand{
  ProjectID:p.ID,GoalID:goal.ID,DeclaredTargetID:target.ID,
  CertificateSHA256:"sha256:"+strings.Repeat("a",64),
  ActorPrincipalID:"human",ExpiresAt:now+10*60000,
  ExpectedGoalRevision:goal.Revision,ExpectedTargetRevision:target.Revision,
 })
 if err!=nil{t.Fatal(err)}
 public,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 operatorPublic,operatorPrivate,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 key,err:=svc.ApproveGoalInstrumentKey(ctx,ApproveGoalInstrumentKeyCommand{
  ProjectID:p.ID,GoalID:goal.ID,TLSPinID:pin.ID,
  PublicKey:public,ActorPrincipalID:"human",
  ExpectedGoalRevision:goal.Revision,ExpectedPinRevision:pin.Revision,
  ExpiresAt:now+8*60000,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 challenge,err:=svc.IssueGoalInstrumentChallenge(ctx,IssueGoalInstrumentChallengeCommand{
  ProjectID:p.ID,GoalID:goal.ID,KeyID:key.ID,ActorPrincipalID:"human",
  ExpectedGoalRevision:goal.Revision,ExpectedKeyRevision:key.Revision,
 })
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,
  "INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('peer','Node',0,'identity','discovered','UNTRUSTED','{}','{}',1,1,1)")
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 approve:=ApproveGoalNodeProbeExecutionPermitCommand{
  ProjectID:p.ID,GoalID:goal.ID,InstrumentKeyID:key.ID,
  ChallengeID:challenge.ID,PeerNodeID:"peer",ActorPrincipalID:"human",
  ExpectedGoalRevision:goal.Revision,OperatorPublicKey:operatorPublic,
 }
 originalApprovalMessage,err:=NodeOperatorExecutionApprovalSigningBytes(approve)
 if err!=nil{t.Fatal(err)}
 approve.OperatorProofSignature=ed25519.Sign(operatorPrivate,originalApprovalMessage)
 invalidProof:=approve
 invalidProof.OperatorProofSignature=append([]byte(nil),approve.OperatorProofSignature...)
 invalidProof.OperatorProofSignature[0]^=1
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,invalidProof);err==nil{
  t.Fatal("operator private signing key possession was not enforced")
 }
 unproved:=approve;unproved.OperatorProofSignature=nil
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,unproved);err==nil{
  t.Fatal("Node permission accepted an unsigned human approval")
 }
 wrongScope:=approve;wrongScope.PeerNodeID="other-peer"
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,wrongScope);err==nil{
  t.Fatal("operator approval signature reused for a different Node")
 }

 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,approve);err==nil{
  t.Fatal("unpaired Node admitted")
 }
 fingerprint:="sha256:"+strings.Repeat("b",64)
 _,err=db.SQL().ExecContext(ctx,
  "INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,revision,created_at,updated_at) VALUES('pair','peer','inbound','paired','token','hash','000000','hash',1,1,?,'',?,?,1,?,?)",
  fingerprint,now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE harness_nodes SET trust_state='paired',trust_zone='PAIRED_MTLS' WHERE id='peer'");err!=nil{t.Fatal(err)}
 robot:=approve;robot.ActorPrincipalID="robot"
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,robot);err==nil{
  t.Fatal("Agent authorised a Node network effect")
 }
 wrong:=approve;wrong.ExpectedGoalRevision++
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,wrong);err==nil{
  t.Fatal("stale Goal revision authorised Node network effect")
 }
 permit,err:=svc.ApproveGoalNodeProbeExecutionPermit(ctx,approve)
 if err!=nil{t.Fatalf("human approved original Node scope rejected: %v",err)}
 if permit.Status!="approved"||permit.PeerTLSFingerprint!=fingerprint||
  permit.ChallengeID!=challenge.ID||permit.TLSPinID!=pin.ID||
  permit.DeclaredTargetID!=target.ID||permit.SourceID!="router"||
  permit.OriginalChallengeSHA256!=nodeProbeHash(challenge.SigningPayload)||
  permit.InstrumentPublicKeySHA256!=nodeProbeHash(public)||
  permit.OperatorPublicKeySHA256!=nodeProbeHash(operatorPublic)||
  permit.PlanGeneration!=1||permit.PlanSHA256==""||
  permit.ExpiresAt!=challenge.ExpiresAt{
  t.Fatalf("issued permit does not bind original Node instrument and target: %+v",permit)
 }
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,approve);err==nil{
  t.Fatal("repeated challenge gained second approval")
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_node_probe_execution_permits SET peer_node_id='other' WHERE id=?",permit.ID);err==nil{
  t.Fatal("Node fingerprint/identity could be rewritten")
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_node_probe_execution_permits SET operator_public_key_sha256=? WHERE id=?",
  "sha256:"+strings.Repeat("0",64),permit.ID);err==nil{
  t.Fatal("human operator signer was changed after authenticated approval")
 }
 if _,err=db.SQL().ExecContext(ctx,
  "DELETE FROM project_goal_node_probe_execution_permits WHERE id=?",permit.ID);err==nil{
  t.Fatal("execution permit immutable history deleted")
 }
 found,err:=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].ID!=permit.ID||
  found[0].EffectiveExecutionState!="active"{
  t.Fatalf("human approval inventory did not reflect current scope: %+v %v",found,err)
 }
 makeSealCalls:=0
 makeSeal:=func(p GoalNodeProbeExecutionPermit)([]byte,error){
  makeSealCalls++
  if p.ID!=permit.ID||p.EffectiveExecutionState!="active"||
   p.OperatorPublicKeySHA256!=permit.OperatorPublicKeySHA256{
   t.Fatalf("transactional server admitted wrong operator or Node approval: %+v",p)
  }
  return []byte(`{"sealed":"original-onepane-approval"}`),nil
 }
 sealCommand:=SealGoalNodeProbeExecutionPermitCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",
 }
 originalSeal,err:=svc.SealGoalNodeProbeExecutionPermit(ctx,sealCommand,makeSeal)
 if err!=nil||len(originalSeal)==0||makeSealCalls!=1{
  t.Fatalf("active human grant not transactionally sealed: %v %s",err,originalSeal)
 }
 var sealEvents int
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM events WHERE event_type='project.goal_node_probe_execution_sealed' AND aggregate_id=?",
  permit.ID).Scan(&sealEvents);err!=nil||sealEvents!=1{
  t.Fatalf("original server seal not immutably audited: %v %d",err,sealEvents)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE harness_nodes SET trust_state='unavailable' WHERE id='peer'");err!=nil{t.Fatal(err)}
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].EffectiveExecutionState!="peer_unavailable"{
  t.Fatalf("Node trust loss hidden by historical approval: %+v %v",found,err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE harness_nodes SET trust_state='paired' WHERE id='peer'");err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE workspace_memberships SET status='suspended' WHERE workspace_id='tenant' AND principal_id='human'");err!=nil{t.Fatal(err)}
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].EffectiveExecutionState!="approver_inactive"{
  t.Fatalf("human reviewer withdrawal was hidden: %+v %v",found,err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE workspace_memberships SET status='active' WHERE workspace_id='tenant' AND principal_id='human'");err!=nil{t.Fatal(err)}
 // Revoking the ORIGINAL human-approved TCP socket consent must also
 // revoke the effective authority of an already sealed Node permit.
 // This may not be inferred just from the still-valid instrument key,
 // declared target, TLS pin and previously paired Node.
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 tcpRevoked,err:=svc.RevokeGoalTCPProbe(ctx,RevokeGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:tcp.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:tcp.Revision,
 })
 if err!=nil||tcpRevoked.Status!="revoked"{
  t.Fatalf("human TCP permit revocation failed: %v %+v",err,tcpRevoked)
 }
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].Status!="approved"||
  found[0].EffectiveExecutionState!="tcp_scope_unavailable"{
  t.Fatalf("TCP consent withdrawn but Node action still appeared live: %v %+v",err,found)
 }
 if _,err=svc.SealGoalNodeProbeExecutionPermit(ctx,sealCommand,makeSeal);
  !errors.Is(err,ErrRevisionConflict)||makeSealCalls!=1{
  t.Fatalf("revoked TCP scope was signed again: %v (seal calls %d)",err,makeSealCalls)
 }
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM events WHERE event_type='project.goal_node_probe_execution_sealed' AND aggregate_id=?",
  permit.ID).Scan(&sealEvents);err!=nil||sealEvents!=1{
  t.Fatalf("revoked Node scope appended a seal audit event: %v %d",err,sealEvents)
 }
 // Changing and restoring original JSON cannot revive the original
 // execution consent, even when source IDs and SHA-256 match again.
 var originalJSON string
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT monitoring_plan_json FROM project_continuous_goals WHERE id=?",
  goal.ID).Scan(&originalJSON);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
  "{\"version\":1,\"source_ids\":[\"router\",\"extra\"]}",goal.ID);err!=nil{t.Fatal(err)}
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].EffectiveExecutionState!="scope_inactive"{
  t.Fatalf("changed plan retained former Node authority: %+v %v",found,err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
  originalJSON,goal.ID);err!=nil{t.Fatal(err)}
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].EffectiveExecutionState!="scope_inactive"{
  t.Fatalf("restored plan resurrected original execution grant: %+v %v",found,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 revoked,err:=svc.RevokeGoalNodeProbeExecutionPermit(ctx,
  RevokeGoalNodeProbeExecutionPermitCommand{
   ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
   ActorPrincipalID:"human",ExpectedGoalRevision:goal.Revision,
   ExpectedPermitRevision:permit.Revision,
  })
 if err!=nil||revoked.Status!="revoked"{
  t.Fatalf("human revocation failed: %+v %v",revoked,err)
 }
 found,err=svc.ListGoalNodeProbeExecutionPermits(ctx,p.ID,goal.ID)
 if err!=nil||len(found)!=1||found[0].EffectiveExecutionState!="revoked"{
  t.Fatalf("revoked approval showed as live: %+v %v",found,err)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 approve.ExpectedGoalRevision=goal.Revision
 if _,err=svc.ApproveGoalNodeProbeExecutionPermit(ctx,approve);err==nil{
  t.Fatal("revoked challenge silently acquired replacement permission")
 }
 if goal.Health!="unobserved"||goal.Status!="configured"||goal.LastVerifiedAt!=0{
  t.Fatalf("human execution approval claimed verified health: %+v",goal)
 }
}
