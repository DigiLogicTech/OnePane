package api

import(
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "fmt"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestHumanPinnedTLSCertificateRequiresExactExistingTargetAndOneBoundedCheck(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tls-pin-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Explicit out-of-band TLS pin",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Pin",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:w.ID,
  Objective:"Certificate possession, not device security",
  ActorPrincipalID:"operator",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"router",ActorPrincipalID:"operator",
  TargetIPv4:"192.168.42.247",TargetPort:443,TimeoutMS:100,
  ExpiresAt:now+30*60000,ExpectedGoalRevision:goal.Revision,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 declared,err:=svc.DeclareGoalTCPTarget(ctx,projectworkspace.DeclareGoalTCPTargetCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  DisplayLabel:"Known router TLS endpoint",ActorPrincipalID:"operator",
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ExpiresAt:now+20*60000,
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 handler:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 base:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID
 pins:=base+"/tls-pins"
 request:=func(method,who,path,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  rec:=httptest.NewRecorder();handler.ServeHTTP(rec,req)
  return rec
 }
 fingerprint:="sha256:"+strings.Repeat("a",64)
 body:=fmt.Sprintf(`{"declared_target_id":"%s",
  "certificate_sha256":"%s","expected_goal_revision":%d,
  "expected_target_revision":1,"expires_at":%d}`,
  declared.ID,fingerprint,goal.Revision,now+10*60000)
 for _,tc:=range []struct{who string;code int}{
  {"",401},{"outsider",403},{"robot",403},
 }{
  got:=request("POST",tc.who,pins,body)
  if got.Code!=tc.code{t.Fatalf("unapproved %s pin %d %s",
   tc.who,got.Code,got.Body)}
 }
 if got:=request("POST","operator",pins,
  strings.Replace(body,fingerprint,"sha256:INVALID",1));got.Code<400{
  t.Fatalf("malformed out-of-band certificate fingerprint accepted: %d",got.Code)
 }
 if got:=request("POST","operator",pins,
  strings.Replace(body,declared.ID,"foreign-declared-target",1));got.Code<400{
  t.Fatalf("pin accepted a foreign target: %d",got.Code)
 }
 approved:=request("POST","operator",pins,body)
 if approved.Code!=201{t.Fatalf("human TLS pin approval failed: %d %s",
  approved.Code,approved.Body)}
 var pin projectworkspace.GoalTLSPin
 if err=json.Unmarshal(approved.Body.Bytes(),&pin);err!=nil{t.Fatal(err)}
 if pin.CertificateSHA256!=fingerprint||pin.Status!="approved"||
  pin.DeclaredTargetID!=declared.ID||pin.PermitID!=permit.ID{
  t.Fatalf("TLS approval drifted off exact target: %+v",pin)
 }
 if got:=request("POST","operator",pins,body);got.Code<400{
  t.Fatalf("same Goal revision reissued pin: %d",got.Code)
 }
 if got:=request("GET","robot",pins,"");got.Code!=403{
  t.Fatalf("agent retrieved TLS pin history: %d",got.Code)
 }
 list:=request("GET","operator",pins,"")
 if list.Code!=200||!strings.Contains(list.Body.String(),pin.ID)||
  !strings.Contains(list.Body.String(),"\"effective_pin_state\":\"active\""){
  t.Fatalf("human cannot inspect active bounded pin inventory: %d %s",list.Code,list.Body)
 }
 // A change to the Goal plan, even retaining the approved source name,
 // invalidates the pin's effective network authority.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router","other"]}`,goal.ID);err!=nil{t.Fatal(err)}
 list=request("GET","operator",pins,"")
 if list.Code!=200||!strings.Contains(list.Body.String(),
  "\"effective_pin_state\":\"target_inactive\""){
  t.Fatalf("modified Goal plan left previous TLS pin active: %d %s",
   list.Code,list.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router"]}`,goal.ID);err!=nil{t.Fatal(err)}
 // The private instrument key is generated independently. Only its
 // public counterpart is human enrolled and bound to the existing pin.
 public,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 enrollPath:=base+"/instrument-keys"
 enrollBody,_:=json.Marshal(map[string]any{
  "tls_pin_id":pin.ID,"public_key":public,
  "expected_goal_revision":goal.Revision,
  "expected_pin_revision":pin.Revision,
  "expires_at":now+8*60000,
 })
 for _,who:=range []string{"","robot","outsider"}{
  got:=request("POST",who,enrollPath,string(enrollBody))
  if got.Code<400{
   t.Fatalf("%q principal enrolled an independent instrument key: %d",who,got.Code)
  }
 }
 issued:=request("POST","operator",enrollPath,string(enrollBody))
 if issued.Code!=201{
  t.Fatalf("human could not enroll independent instrument: %d %s",issued.Code,issued.Body)
 }
 var instrument projectworkspace.GoalInstrumentKey
 if err=json.Unmarshal(issued.Body.Bytes(),&instrument);err!=nil{t.Fatal(err)}
 if instrument.TLSPinID!=pin.ID||instrument.DeclaredTargetID!=declared.ID{
  t.Fatalf("instrument key escaped human pinned endpoint: %+v",instrument)
 }
 listedKeys:=request("GET","operator",enrollPath,"")
 if listedKeys.Code!=200||!strings.Contains(listedKeys.Body.String(),
  "\"effective_instrument_state\":\"active\""){
  t.Fatalf("active instrument key status was not available: %d %s",
   listedKeys.Code,listedKeys.Body)
 }
 // A changed Goal plan preserving the original source must still
 // invalidate the pin and all independently enrolled instrument keys.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router","other"]}`,goal.ID);err!=nil{t.Fatal(err)}
 listedKeys=request("GET","operator",enrollPath,"")
 if listedKeys.Code!=200||!strings.Contains(listedKeys.Body.String(),
  "\"effective_instrument_state\":\"pin_inactive\""){
  t.Fatalf("changed plan left human instrument key live: %d %s",
   listedKeys.Code,listedKeys.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router"]}`,goal.ID);err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 keyPath:=enrollPath+"/"+instrument.ID
 challengeBody,_:=json.Marshal(map[string]any{
  "expected_goal_revision":goal.Revision,
  "expected_key_revision":instrument.Revision,
 })
 if got:=request("POST","robot",keyPath+"/challenge",string(challengeBody));
  got.Code!=403{t.Fatalf("Agent minted human-only signing challenge: %d",got.Code)}
 chResult:=request("POST","operator",keyPath+"/challenge",string(challengeBody))
 if chResult.Code!=201{t.Fatalf("instrument challenge endpoint rejected: %d %s",
  chResult.Code,chResult.Body)}
 var challenge projectworkspace.GoalInstrumentChallenge
 if err=json.Unmarshal(chResult.Body.Bytes(),&challenge);err!=nil{t.Fatal(err)}
 if len(challenge.Nonce)!=32||len(challenge.SigningPayload)==0{
  t.Fatalf("server did not issue securely bound signing bytes: %+v",challenge)
 }
 proveBody,_:=json.Marshal(map[string]any{
  "challenge_id":challenge.ID,"expected_goal_revision":goal.Revision,
  "signature":ed25519.Sign(private,challenge.SigningPayload),
 })
 if got:=request("POST","robot",keyPath+"/prove",string(proveBody));
  got.Code!=403{t.Fatalf("Agent admitted a signed proof: %d",got.Code)}
 proofHTTP:=request("POST","operator",keyPath+"/prove",string(proveBody))
 if proofHTTP.Code!=201{t.Fatalf("real Ed25519 proof rejected at human API: %d %s",
  proofHTTP.Code,proofHTTP.Body)}
 var proof projectworkspace.GoalInstrumentProof
 if err=json.Unmarshal(proofHTTP.Body.Bytes(),&proof);err!=nil{t.Fatal(err)}
 if proof.ProofClass!="human_bound_instrument_key_possession"||
  proof.DeviceIdentityVerified||proof.RemediationAuthorized{
  t.Fatalf("source key possession became physical device verification: %+v",proof)
 }
 bundlePath:=base+"/instrument-proofs/"+proof.ID+"/verification-bundle"
 for _,who:=range []string{"","robot","outsider"}{
  if got:=request("GET",who,bundlePath,"");got.Code<400{
   t.Fatalf("%q accessed another actor's historical instrument proof: %d",who,got.Code)
  }
 }
 exported:=request("GET","operator",bundlePath,"")
 if exported.Code!=200{t.Fatalf("human cannot export proof for independent review: %d %s",
  exported.Code,exported.Body)}
 var bundle projectworkspace.GoalInstrumentVerificationBundle
 if err=json.Unmarshal(exported.Body.Bytes(),&bundle);err!=nil{t.Fatal(err)}
 if !bundle.DetachedSignatureVerified||bundle.DeviceIdentityVerified||
  bundle.RemediationAuthorized||bundle.Proof.ID!=proof.ID||
  !ed25519.Verify(ed25519.PublicKey(bundle.PublicKey),
   bundle.SigningPayload,bundle.Signature){
  t.Fatalf("operator proof export is not independently verifiable: %+v",bundle)
 }
 if got:=request("POST","operator",keyPath+"/prove",string(proveBody));
  got.Code<400{t.Fatalf("same signed challenge replayed over HTTP: %d",got.Code)}
 if got:=request("GET","robot",base+"/instrument-proofs","");got.Code!=403{
  t.Fatalf("Agent listed security evidence: %d",got.Code)
 }
 if got:=request("GET","operator",base+"/instrument-proofs","");
  got.Code!=200||!strings.Contains(got.Body.String(),proof.ID){
  t.Fatalf("human could not audit accepted instrument proof: %d %s",got.Code,got.Body)
 }
 // A second challenge supports one exact target-scoped, signed instrument
 // SELF-REPORT; neither an Agent nor a foreign tenant may prepare/admit it.
 measureChallengeResponse:=request("POST","operator",keyPath+"/challenge",
  string(challengeBody))
 if measureChallengeResponse.Code!=201{
  t.Fatalf("measurement challenge issue failed: %d %s",
   measureChallengeResponse.Code,measureChallengeResponse.Body)
 }
 var mch projectworkspace.GoalInstrumentChallenge
 if err=json.Unmarshal(measureChallengeResponse.Body.Bytes(),&mch);err!=nil{t.Fatal(err)}
 targetPlanURL:=keyPath+"/probe-plan"
 targetPlanBody,_:=json.Marshal(map[string]any{
  "challenge_id":mch.ID,"expected_goal_revision":goal.Revision,
 })
 for _,who:=range []string{"","robot","outsider"}{
  got:=request("POST",who,targetPlanURL,string(targetPlanBody))
  if got.Code<400{
   t.Fatalf("nonhuman %s obtained private endpoint plan: %d %s",
    who,got.Code,got.Body)
  }
 }
 staleGoalBody,_:=json.Marshal(map[string]any{
  "challenge_id":mch.ID,"expected_goal_revision":goal.Revision+1,
 })
 if got:=request("POST","operator",targetPlanURL,string(staleGoalBody));
  got.Code<400{
  t.Fatalf("stale Goal revision exported private approved endpoint: %d",got.Code)
 }
 crossChallengeBody,_:=json.Marshal(map[string]any{
  "challenge_id":"another-challenge","expected_goal_revision":goal.Revision,
 })
 if got:=request("POST","operator",targetPlanURL,string(crossChallengeBody));
  got.Code<400{
  t.Fatalf("foreign nonce exported human-only endpoint plan: %d",got.Code)
 }
 if got:=request("POST","operator",targetPlanURL,
  strings.TrimSuffix(string(targetPlanBody),"}")+`,"target_ipv4":"8.8.8.8"}`);
  got.Code!=400{
  t.Fatalf("operator injected target address into signed plan: %d %s",
   got.Code,got.Body)
 }
 scoped:=request("POST","operator",targetPlanURL,string(targetPlanBody))
 if scoped.Code!=200{
  t.Fatalf("active human cannot export approved one-shot target: %d %s",
   scoped.Code,scoped.Body)
 }
 var scopedPlan projectworkspace.GoalInstrumentProbePlan
 if err=json.Unmarshal(scoped.Body.Bytes(),&scopedPlan);err!=nil{t.Fatal(err)}
 if scopedPlan.ProjectID!=p.ID||scopedPlan.GoalID!=goal.ID||
  scopedPlan.ChallengeID!=mch.ID||
  scopedPlan.DeclaredTargetID!=declared.ID||
  scopedPlan.TLSPinID!=pin.ID||scopedPlan.TargetIPv4!="192.168.42.247"||
  scopedPlan.TargetPort!=443||scopedPlan.TimeoutMS!=100||
  scopedPlan.ObserverVantageVerified||scopedPlan.TargetDeviceIdentityVerified||
  scopedPlan.AttestationClass!="human_authorised_endpoint_snapshot_not_device_attestation"{
  t.Fatalf("server-issued snapshot deviates from exact original permit: %+v",scopedPlan)
 }
 again:=request("POST","operator",targetPlanURL,string(targetPlanBody))
 if again.Code!=200{
  t.Fatalf("same human cannot safely re-read approved plan: %d %s",
   again.Code,again.Body)
 }
 var exportCount int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*)
  FROM project_goal_instrument_probe_plan_exports
  WHERE challenge_id=? AND actor_principal_id=?`,
  mch.ID,"operator").Scan(&exportCount);err!=nil||exportCount!=1{
  t.Fatalf("re-read duplicated private-topology access ledger: %d %v",
   exportCount,err)
 }
 var eventCount int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='project.goal_instrument_probe_plan_first_exported'
   AND CAST(payload_json AS TEXT) NOT LIKE '%192.168.42.247%'`).
   Scan(&eventCount);err!=nil||eventCount!=1{
  t.Fatalf("private endpoint export audit missing or duplicated: %d %v",
   eventCount,err)
 }
 var leaked int
 if err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='project.goal_instrument_probe_plan_first_exported'
   AND CAST(payload_json AS TEXT) LIKE '%192.168.42.247%'`).
   Scan(&leaked);err!=nil||leaked!=0{
  t.Fatalf("private endpoint appeared in audit payload: %d %v",leaked,err)
 }
 if _,err=db.SQL().ExecContext(ctx,`DELETE FROM
  project_goal_instrument_probe_plan_exports WHERE challenge_id=?`,mch.ID);err==nil{
  t.Fatal("human private topology-export audit could be deleted")
 }
 observedAt:=clock.Real{}.UnixMilli()
 if observedAt<mch.IssuedAt{observedAt=mch.IssuedAt}
 preparation:=map[string]any{
  "challenge_id":mch.ID,"outcome":"not_connected",
  "observed_at":observedAt,"expected_goal_revision":goal.Revision,
 }
 prepareJSON,_:=json.Marshal(preparation)
 prepareURL:=keyPath+"/measurement/prepare"
 acceptURL:=keyPath+"/measurement/accept"
 for _,who:=range []string{"","robot","outsider"}{
  if got:=request("POST",who,prepareURL,string(prepareJSON));got.Code<400{
   t.Fatalf("nonhuman %s prepared measurement signing payload: %d",who,got.Code)
  }
 }
 preparedResponse:=request("POST","operator",prepareURL,string(prepareJSON))
 if preparedResponse.Code!=200{
  t.Fatalf("human cannot prepare exact signed observation: %d %s",
   preparedResponse.Code,preparedResponse.Body)
 }
 var prepared projectworkspace.InstrumentMeasurementSigningPayload
 if err=json.Unmarshal(preparedResponse.Body.Bytes(),&prepared);err!=nil{t.Fatal(err)}
 if len(prepared.SigningPayload)==0||prepared.ObserverVantageVerified{
  t.Fatalf("prepared measurement claims verified source vantage: %+v",prepared)
 }
 preparation["signature"]=ed25519.Sign(private,prepared.SigningPayload)
 acceptanceJSON,_:=json.Marshal(preparation)
 if got:=request("POST","robot",acceptURL,string(acceptanceJSON));got.Code!=403{
  t.Fatalf("agent admitted instrument signed claim: %d",got.Code)
 }
 accepted:=request("POST","operator",acceptURL,string(acceptanceJSON))
 if accepted.Code!=201{
  t.Fatalf("signed instrument self-report admission rejected: %d %s",
   accepted.Code,accepted.Body)
 }
 var mclaim projectworkspace.SignedMeasurementClaim
 if err=json.Unmarshal(accepted.Body.Bytes(),&mclaim);err!=nil{t.Fatal(err)}
 if mclaim.Outcome!="not_connected"||mclaim.DeviceIdentityVerified||
  mclaim.ObserverVantageVerified||mclaim.TargetCorrelationEstablished||
  mclaim.RemediationAuthorized{
  t.Fatalf("signed source self-report became verified network truth: %+v",mclaim)
 }
 if got:=request("POST","operator",acceptURL,string(acceptanceJSON));got.Code<400{
  t.Fatalf("instrument signed measurement replay passed HTTP: %d",got.Code)
 }
 if got:=request("POST","operator",targetPlanURL,string(targetPlanBody));got.Code<400{
  t.Fatalf("spent instrument challenge still exported an executable one-shot plan: %d",got.Code)
 }
 mlistURL:=base+"/instrument-measurements"
 if got:=request("GET","robot",mlistURL,"");got.Code!=403{
  t.Fatalf("Agent read human-only signed measurement list: %d",got.Code)
 }
 if got:=request("GET","operator",mlistURL,"");got.Code!=200||
  !strings.Contains(got.Body.String(),mclaim.ID){
  t.Fatalf("signed measurement list missing valid proof: %d %s",got.Code,got.Body)
 }
 mbundleURL:=mlistURL+"/"+mclaim.ID+"/verification-bundle"
 if got:=request("GET","outsider",mbundleURL,"");got.Code!=403{
  t.Fatalf("cross tenant accessed signed instrument measurement: %d",got.Code)
 }
 mexport:=request("GET","operator",mbundleURL,"")
 if mexport.Code!=200{t.Fatalf("signed measurement bundle unavailable: %d %s",
  mexport.Code,mexport.Body)}
 var mverify projectworkspace.InstrumentMeasurementVerificationBundle
 if err=json.Unmarshal(mexport.Body.Bytes(),&mverify);err!=nil{t.Fatal(err)}
 if !mverify.DetachedSignatureVerified||
  !ed25519.Verify(ed25519.PublicKey(mverify.PublicKey),
   mverify.SigningPayload,mverify.Signature)||
  strings.Contains(mexport.Body.String(),"192.168.42.247"){
  t.Fatalf("offline signed measurement verification or privacy failed: %+v",mverify)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 revokeKeyBody,_:=json.Marshal(map[string]any{
  "expected_goal_revision":goal.Revision,
  "expected_key_revision":instrument.Revision,
 })
 if got:=request("POST","operator",keyPath+"/revoke",string(revokeKeyBody));
  got.Code!=200{t.Fatalf("human could not revoke instrument key: %d %s",got.Code,got.Body)}
 listedKeys=request("GET","operator",enrollPath,"")
 if listedKeys.Code!=200||!strings.Contains(listedKeys.Body.String(),
  "\"effective_instrument_state\":\"revoked\""){
  t.Fatalf("revoked instrument identity remained active: %d %s",
   listedKeys.Code,listedKeys.Body)
 }
 if got:=request("GET","operator",bundlePath,"");got.Code!=200{
  t.Fatalf("human cannot inspect immutable instrument proof after key revoke: %d %s",
   got.Code,got.Body)
 }
 if got:=request("GET","operator",mbundleURL,"");got.Code!=200{
  t.Fatalf("human could not audit signed measurement after key revocation: %d %s",
   got.Code,got.Body)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 verify:=pins+"/"+pin.ID+"/verify"
 verifyBody:=fmt.Sprintf(`{"expected_goal_revision":%d,
  "expected_pin_revision":1}`,goal.Revision)
 if got:=request("POST","robot",verify,verifyBody);got.Code!=403{
  t.Fatalf("agent initiated TLS connection: %d",got.Code)
 }
 if got:=request("POST","operator",verify,
  strings.TrimSuffix(verifyBody,"}")+`,"target_ipv4":"8.8.8.8"}`);
  got.Code!=400{
  t.Fatalf("target injection was accepted: %d",got.Code)
 }
 // One bounded connect toward an explicit private test address without a
 // server. No response may claim possession, trusted device or health.
 result:=request("POST","operator",verify,verifyBody)
 if result.Code!=201{t.Fatalf("one manual TLS check was denied: %d %s",
  result.Code,result.Body)}
 var checked projectworkspace.GoalPinnedTLSCheck
 if err=json.Unmarshal(result.Body.Bytes(),&checked);err!=nil{t.Fatal(err)}
 if checked.Status=="matched"||checked.DeviceIdentityVerified||
  checked.CertificatePossessionMatched||checked.MonitoringActive{
  t.Fatalf("unavailable TLS connection was counted as attested: %+v",checked)
 }
 if got:=request("POST","operator",verify,verifyBody);got.Code<400{
  t.Fatalf("same 5-minute TLS claim was allowed twice: %d",got.Code)
 }
 checks:=request("GET","operator",base+"/tls-checks","")
 if checks.Code!=200||!strings.Contains(checks.Body.String(),checked.ID)||
  strings.Contains(checks.Body.String(),"192.168.42.247"){
  t.Fatalf("bounded check history missing or leaked private target: %d %s",
   checks.Code,checks.Body)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 revokeBody:=fmt.Sprintf(`{"expected_goal_revision":%d,
  "expected_pin_revision":1}`,goal.Revision)
 if got:=request("POST","operator",pins+"/"+pin.ID+"/revoke",revokeBody);
  got.Code!=200{t.Fatalf("human could not revoke TLS pin: %d %s",got.Code,got.Body)}
 list=request("GET","operator",pins,"")
 if list.Code!=200||!strings.Contains(list.Body.String(),
  "\"effective_pin_state\":\"revoked\""){
  t.Fatalf("revoked TLS cert remained effective in inventory: %d %s",list.Code,list.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`DELETE FROM project_goal_tls_endpoint_pins
  WHERE id=?`,pin.ID);err==nil{t.Fatal("TLS pin audit could be deleted")}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_goal_tls_endpoint_pins
  SET certificate_sha256=? WHERE id=?`,fingerprint,pin.ID);err==nil{
  t.Fatal("TLS approval fingerprint could be changed")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Health!="unobserved"||goal.Status!="configured"||
  goal.LastVerifiedAt!=0{t.Fatalf("TLS check activated Goal: %+v",goal)}
 _=time.Second
}
