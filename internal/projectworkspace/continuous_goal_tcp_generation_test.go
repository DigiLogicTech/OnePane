package projectworkspace

import(
 "context"
 "bytes"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Testing entirely synthetic SQLite permissions: NO network dial. After a
// changed plan returns to identical JSON, none of the historical human
// TCP, target, pin or instrument grants can become currently executable.
func TestRestoredGoalPlanCannotReviveApprovedTCPAndInstrumentChain(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-generation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)",
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"TCP scope never revives",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Dedicated",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 original:=`{"version":1,"source_ids":["router"]}`
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:w.ID,
  ActorPrincipalID:"operator",Objective:"No restored TCP consent",
  MonitoringPlanJSON:json.RawMessage(original),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.42.37",TargetPort:443,TimeoutMS:200,
  ExpiresAt:now+30*60000,ExpectedGoalRevision:goal.Revision,
  ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 target,err:=svc.DeclareGoalTCPTarget(ctx,DeclareGoalTCPTargetCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  DisplayLabel:"Operator router",ActorPrincipalID:"operator",
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ExpiresAt:now+20*60000,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 pin,err:=svc.ApproveGoalTLSPin(ctx,ApproveGoalTLSPinCommand{
  ProjectID:project.ID,GoalID:goal.ID,DeclaredTargetID:target.ID,
  CertificateSHA256:"sha256:"+strings.Repeat("b",64),
  ActorPrincipalID:"operator",ExpectedGoalRevision:goal.Revision,
  ExpectedTargetRevision:target.Revision,ExpiresAt:now+15*60000,
 })
 if err!=nil{t.Fatal(err)}
 pub,_,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 key,err:=svc.ApproveGoalInstrumentKey(ctx,ApproveGoalInstrumentKeyCommand{
  ProjectID:project.ID,GoalID:goal.ID,TLSPinID:pin.ID,
  PublicKey:pub,ActorPrincipalID:"operator",
  ExpectedGoalRevision:goal.Revision,ExpectedPinRevision:pin.Revision,
  ExpiresAt:now+10*60000,
 })
 if err!=nil{t.Fatal(err)}
 if permit.PlanGeneration<1{
  t.Fatalf("original TCP permit did not pin Goal generation: %+v",permit)
 }
 assertState:=func(targetState,pinState,keyState string){
  t.Helper()
  targets,err:=svc.ListGoalTCPTargets(ctx,project.ID,goal.ID)
  if err!=nil{t.Fatal(err)}
  pins,err:=svc.ListGoalTLSPins(ctx,project.ID,goal.ID)
  if err!=nil{t.Fatal(err)}
  keys,err:=svc.ListGoalInstrumentKeys(ctx,project.ID,goal.ID)
  if err!=nil{t.Fatal(err)}
  if len(targets)!=1||targets[0].ID!=target.ID||
   targets[0].EffectiveIdentityState!=targetState||
   len(pins)!=1||pins[0].ID!=pin.ID||
   pins[0].EffectivePinState!=pinState||
   len(keys)!=1||keys[0].ID!=key.ID||
   keys[0].EffectiveInstrumentState!=keyState{
   t.Fatalf("effective chain: targets=%+v pins=%+v keys=%+v",targets,pins,keys)
  }
 }
 // TEST-ONLY previously admitted metadata: fabricated nonces/signatures
 // here are foreign-key fixtures, not cryptographically verified claims,
 // and in particular not proof of a physical observer or honest sensor.
 observed:=clock.Real{}.UnixMilli()
 if observed<key.ApprovedAt{observed=key.ApprovedAt}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tls_endpoint_checks(
  id,pin_id,project_id,goal_id,claimed_at,cadence_window,status,completed_at)
  VALUES('tls-old-generation',?,?,?,?,0,'matched',?)`,
  pin.ID,project.ID,goal.ID,observed,observed)
 if err!=nil{t.Fatal(err)}
 for i,id:=range []string{"challenge-proof","challenge-report"}{
  _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_instrument_challenges(
   id,instrument_key_id,project_id,goal_id,nonce,signing_payload,
   issued_at,expires_at,status,accepted_at)
   VALUES(?,?,?,?,?,?,?,?,'accepted',?)`,
   id,key.ID,project.ID,goal.ID,bytes.Repeat([]byte{byte(i+1)},32),
   bytes.Repeat([]byte{0x33},256),observed-1,observed+60000,observed)
  if err!=nil{t.Fatal(err)}
 }
 digest:="sha256:"+strings.Repeat("a",64)
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_instrument_proofs(
  id,instrument_key_id,challenge_id,project_id,goal_id,
  declared_target_id,tls_pin_id,signature_sha256,signature,accepted_at)
  VALUES('proof-old-generation',?,?,?,?,?,?,?,?,?)`,
  key.ID,"challenge-proof",project.ID,goal.ID,target.ID,pin.ID,
  digest,bytes.Repeat([]byte{0x42},64),observed)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_instrument_measurement_claims(
  id,project_id,goal_id,instrument_key_id,challenge_id,declared_target_id,
  tls_pin_id,source_id,measurement_type,outcome,observed_at,accepted_at,
  signed_payload,signature,signature_sha256)
  VALUES('report-old-generation',?,?,?,?,?,?,'router',
   'endpoint_tcp_connect_claim','not_connected',?,?,?,?,?)`,
  project.ID,goal.ID,key.ID,"challenge-report",target.ID,pin.ID,
  observed,observed,bytes.Repeat([]byte{0x33},300),
  bytes.Repeat([]byte{0x42},64),digest)
 if err!=nil{t.Fatal(err)}
 before,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(before.Sources)!=1||
  before.Sources[0].TLSCheckState!="matched"||
  before.Sources[0].InstrumentProofState!="recent_signed_challenge"||
  before.Sources[0].SignedMeasurementClaimState!="recent_instrument_self_report_unverified"||
  before.DeviceIdentityVerified||before.RemediationAuthorized{
  t.Fatalf("old-generation metadata fixture not projected conservatively: %+v",before)
 }
 assertState("active","active","active")
 listed,err:=svc.ListGoalTCPProbePermits(ctx,project.ID,goal.ID)
 if err!=nil||len(listed)!=1||listed[0].EffectiveScopeState!="active"{
  t.Fatalf("human-approved original TCP grant not represented as live: %+v %v",listed,err)
 }
 for _,plan:=range []string{
  `{"version":1,"source_ids":["router","other"]}`,original,
 }{
  if _,err=db.SQL().ExecContext(ctx,
   "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
   plan,goal.ID);err!=nil{t.Fatal(err)}
  assertState("permit_inactive","target_inactive","pin_inactive")
  stale,triageErr:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
  if triageErr!=nil{t.Fatal(triageErr)}
  if len(stale.Sources)!=1||
   stale.Sources[0].TLSCheckID!=""||
   stale.Sources[0].InstrumentProofID!=""||
   stale.Sources[0].SignedMeasurementClaimID!=""||
   stale.Sources[0].TLSCheckState!="no_current_approved_check"||
   stale.Sources[0].InstrumentProofState!="no_current_authenticated_proof"||
   stale.Sources[0].SignedMeasurementClaimState!="no_current_signed_claim"{
   t.Fatalf("old TLS/instrument signature metadata revived after plan edit/restore: %+v",stale)
  }
  listed,err=svc.ListGoalTCPProbePermits(ctx,project.ID,goal.ID)
  if err!=nil||len(listed)!=1||
   listed[0].Status!="approved"||
   listed[0].EffectiveScopeState!="goal_scope_inactive"{
   t.Fatalf("historically approved TCP grant improperly active: %+v %v",listed,err)
  }
 }
 var currentGen int64
 if err=db.SQL().QueryRowContext(ctx,
  "SELECT plan_generation FROM project_continuous_goals WHERE id=?",
  goal.ID).Scan(&currentGen);err!=nil{t.Fatal(err)}
 if currentGen!=permit.PlanGeneration+2{
  t.Fatalf("original Goal plan generation not advanced on restore: %d",currentGen)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_goal_tcp_probe_permits SET plan_generation=? WHERE id=?",
  currentGen,permit.ID);err==nil{
  t.Fatal("original human permit generation rewritten after approval")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 if _,err=svc.claimGoalTCPAttempt(ctx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,
  ExpectedPermitRevision:permit.Revision,ActorPrincipalID:"operator",
 });err==nil{
  t.Fatal("old permit admitted new TCP work after restored plan")
 }
}
