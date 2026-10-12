package main

import(
 "bytes"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "encoding/json"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
func TestLocalInstrumentKeygenIsExclusiveAndPrivate(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"instrument.secret")
 if err:=runKeygen([]string{"--private-key-out",path});err!=nil{
  t.Fatalf("local key generation rejected: %v",err)
 }
 key,err:=readPrivateKey(path)
 if err!=nil||len(key)!=ed25519.PrivateKeySize{
  t.Fatalf("generated local private key not usable: %v",err)
 }
 pub:=key.Public().(ed25519.PublicKey)
 if len(pub)!=ed25519.PublicKeySize{
  t.Fatal("generated instrument Ed25519 key is invalid")
 }
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if !bytes.Equal(bytes.TrimSpace(raw),[]byte(base64.StdEncoding.EncodeToString(key))){
  t.Fatal("private key file was not properly generated")
 }
 if err=runKeygen([]string{"--private-key-out",path});err==nil{
  t.Fatal("instrument key generation overwrote an existing private key")
 }
 info,err:=os.Stat(path)
 if err!=nil{t.Fatal(err)}
 if runtime.GOOS!="windows"{
  if info.Mode().Perm()!=0600{
   t.Fatalf("private file permissions not owner-only: %v",info.Mode().Perm())
  }
  if err=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
  if _,err=readPrivateKey(path);err==nil{
   t.Fatal("insecure world-readable instrument key was loaded")
  }
 }
}
func TestInstrumentProbeCLIRefusesMalformedKey(t *testing.T){
 path:=filepath.Join(t.TempDir(),"bad-key")
 if err:=os.WriteFile(path,[]byte("definitely-not-a-secret-key"),0600);err!=nil{t.Fatal(err)}
 if _,err:=readPrivateKey(path);err==nil{
  t.Fatal("invalid key file was accepted")
 }
 if err:=runProbe([]string{"--private-key",path});err==nil{
  t.Fatal("one-shot execution proceeded without signed human challenge")
 }
 if err:=runSign([]string{"--private-key",path});err==nil{
  t.Fatal("offline admission signed without private key or evidence")
 }
}

func TestProbeJSONIsCompatibleWithActualGoalInstrumentAPIResponses(t *testing.T){
 dir:=t.TempDir()
 challenge:=projectworkspace.GoalInstrumentChallenge{
  ID:"challenge1",KeyID:"key1",ProjectID:"project1",GoalID:"goal1",
  Nonce:make([]byte,32),IssuedAt:1000,ExpiresAt:121000,
  Status:"issued",SigningPayload:[]byte("signed domain payload"),
 }
 path:=filepath.Join(dir,"challenge.json")
 b,err:=json.Marshal(challenge)
 if err!=nil{t.Fatal(err)}
 if err=os.WriteFile(path,b,0600);err!=nil{t.Fatal(err)}
 var cli instrumentprobe.Challenge
 if err=readJSON(path,&cli);err!=nil{
  t.Fatalf("instrument CLI cannot read actual OnePane challenge JSON: %v",err)
 }
 if cli.KeyID!=challenge.KeyID||cli.Status!="issued"{
  t.Fatalf("CLI challenge identity lost: %+v",cli)
 }
 prepared:=projectworkspace.InstrumentMeasurementSigningPayload{
  ChallengeID:challenge.ID,MeasurementType:"endpoint_tcp_connect_claim",
  Outcome:"not_connected",ObservedAt:2000,
  SigningPayload:[]byte("prepared envelope"),ExpiresAt:challenge.ExpiresAt,
  ObserverVantageVerified:false,
 }
 b,err=json.Marshal(prepared)
 if err!=nil{t.Fatal(err)}
 path=filepath.Join(dir,"prepared.json")
 if err=os.WriteFile(path,b,0600);err!=nil{t.Fatal(err)}
 var parsed instrumentprobe.PreparedMeasurement
 if err=readJSON(path,&parsed);err!=nil{
  t.Fatalf("instrument CLI cannot read actual OnePane preparation JSON: %v",err)
 }
 if parsed.Outcome!="not_connected"||parsed.ObserverVantageVerified{
  t.Fatalf("server measurement status changed during CLI JSON import: %+v",parsed)
 }
 probePlan:=projectworkspace.GoalInstrumentProbePlan{
  ProjectID:"project1",GoalID:"goal1",
  InstrumentKeyID:"key1",ChallengeID:"challenge1",
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:"192.168.42.77",TargetPort:443,TimeoutMS:500,
  OriginalChallengeSHA256:"sha256:"+strings.Repeat("a",64),
  ExpiresAt:challenge.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 b,err=json.Marshal(probePlan)
 if err!=nil{t.Fatal(err)}
 planPath:=filepath.Join(dir,"plan.json")
 if err=os.WriteFile(planPath,b,0600);err!=nil{t.Fatal(err)}
 var plan instrumentprobe.ProbePlan
 if err=readJSON(planPath,&plan);err!=nil{
  t.Fatalf("CLI cannot parse exact human-only scope plan: %v",err)
 }
 if plan.TargetIPv4!=probePlan.TargetIPv4||
  plan.OriginalChallengeSHA256!=probePlan.OriginalChallengeSHA256||
  plan.TLSPinID!=probePlan.TLSPinID{
  t.Fatalf("plan lost server approved exact endpoint: %+v",plan)
 }
 // The CLI's accepted detached-signature body must use field names
 // consumed by the existing strict OnePane admission endpoint.
 accept:=instrumentprobe.Acceptance{
  ChallengeID:challenge.ID,ObservedAt:2000,Outcome:"not_connected",
  Signature:make([]byte,64),ExpectedGoalRevision:3,
 }
 b,err=json.Marshal(accept)
 if err!=nil{t.Fatal(err)}
 var got map[string]any
 if err=json.Unmarshal(b,&got);err!=nil{t.Fatal(err)}
 for _,k:=range []string{"challenge_id","outcome","observed_at",
  "signature","expected_goal_revision"}{
  if _,ok:=got[k];!ok{t.Fatalf("missing strict API admission field %s",k)}
 }
}

func TestCLIRequiresExplicitApprovedPlanOrDeliberateManualDowngrade(t *testing.T){
 dir:=t.TempDir()
 keyPath:=filepath.Join(dir,"instrument.key")
 if err:=runKeygen([]string{"--private-key-out",keyPath});err!=nil{t.Fatal(err)}
 challengePath:=filepath.Join(dir,"challenge.json")
 canonicalChallenge,err:=json.Marshal(instrumentprobe.Challenge{})
 if err!=nil{t.Fatal(err)}
 if err=os.WriteFile(challengePath,canonicalChallenge,0600);err!=nil{t.Fatal(err)}
 // No plan, even with a plausible RFC1918 address, must NOT silently
 // fall back to a caller-chosen target.
 args:=[]string{"--challenge",challengePath,"--private-key",keyPath,
  "--ipv4","192.168.20.4","--port","443","--timeout-ms","700",
  "--confirm-target-id","target-1","--out",filepath.Join(dir,"receipt.json")}
 if err:=runProbe(args);err==nil||
  !strings.Contains(err.Error(),"approved --plan required"){
  t.Fatalf("manual target silently used without explicit downgrade: %v",err)
 }
 // Passing a plan and an explicit manual address cannot select an
 // arbitrary endpoint regardless of how the CLI flags were ordered.
 planPath:=filepath.Join(dir,"plan.json")
 if err:=os.WriteFile(planPath,[]byte("{}"),0600);err!=nil{t.Fatal(err)}
 if err:=runProbe(append(args,"--plan",planPath));err==nil||
  !strings.Contains(err.Error(),"cannot be combined"){
  t.Fatalf("manual address flags combined with plan: %v",err)
 }
 if err:=runProbe(append(args,"--allow-manual-target"));err==nil{
  t.Fatal("malformed challenge passed even with explicit manual downgrade")
 }
}

func TestPrivatePlanAndReceiptRejectSharedFilesAndSymlinks(t *testing.T){
 dir:=t.TempDir()
 privatePlan:=filepath.Join(dir,"approved-plan.json")
 if err:=os.WriteFile(privatePlan,[]byte("{}"),0600);err!=nil{t.Fatal(err)}
 var out map[string]any
 if err:=readPrivateJSON(privatePlan,&out);err!=nil{
  t.Fatalf("private local one-shot evidence file unexpectedly denied: %v",err)
 }
 if runtime.GOOS!="windows"{
  if err:=os.Chmod(privatePlan,0644);err!=nil{t.Fatal(err)}
  if err:=readPrivateJSON(privatePlan,&out);err==nil{
   t.Fatal("world-readable private target plan was accepted")
  }
  if err:=os.Chmod(privatePlan,0600);err!=nil{t.Fatal(err)}
  link:=filepath.Join(dir,"private-plan-link.json")
  if err:=os.Symlink(privatePlan,link);err==nil{
   if err:=readPrivateJSON(link,&out);err==nil{
    t.Fatal("symlinked private probe plan was accepted")
   }
  }
  keyPath:=filepath.Join(dir,"private.key")
  if err:=runKeygen([]string{"--private-key-out",keyPath});err!=nil{
   t.Fatal(err)
  }
  linkKey:=filepath.Join(dir,"linked-key")
  if err:=os.Symlink(keyPath,linkKey);err==nil{
   if _,err:=readPrivateKey(linkKey);err==nil{
    t.Fatal("symlinked Ed25519 private signing key was accepted")
   }
  }
 }
}

func TestInstrumentPrivateInputsRemainStrictlyBounded(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"oversized-plan.json")
 if err:=os.WriteFile(path,bytes.Repeat([]byte("x"),8193),0600);err!=nil{
  t.Fatal(err)
 }
 var v map[string]any
 if err:=readPrivateJSON(path,&v);err==nil{
  t.Fatal("unbounded private network probe-plan input was accepted")
 }
 oversizedKey:=filepath.Join(dir,"oversized-private.key")
 if err:=os.WriteFile(oversizedKey,bytes.Repeat([]byte("A"),257),0600);err!=nil{
  t.Fatal(err)
 }
 if _,err:=readPrivateKey(oversizedKey);err==nil{
  t.Fatal("oversized instrument private key was read into memory")
 }
 notAFile:=filepath.Join(dir,"evidence-directory")
 if err:=os.Mkdir(notAFile,0700);err!=nil{t.Fatal(err)}
 if err:=readPrivateJSON(notAFile,&v);err==nil{
  t.Fatal("directory was admitted as a private instrument receipt")
 }
}

func TestLocalInstrumentOneShotNonceLedgerSurvivesRetryAndUsesKeyPath(t *testing.T){
 keyPath:=filepath.Join(t.TempDir(),"instrument.secret")
 ch:=instrumentprobe.Challenge{SigningPayload:[]byte("distinct human issued nonce bytes")}
 if err:=claimLocalProbeOnce(keyPath,ch);err!=nil{
  t.Fatalf("first authorized local nonce reservation failed: %v",err)
 }
 if err:=claimLocalProbeOnce(keyPath,ch);err==nil{
  t.Fatal("second invocation reused a locally spent nonce")
 }
 // An alternate receipt output name or a restarted CLI still chooses
 // the same ledger key path and cannot reopen the socket.
 sibling:=instrumentprobe.Challenge{SigningPayload:[]byte("a separately issued nonce")}
 if err:=claimLocalProbeOnce(keyPath,sibling);err!=nil{
  t.Fatalf("a new authorized challenge was blocked: %v",err)
 }
 dir:=keyPath+".onepane-probe-claims"
 entries,err:=os.ReadDir(dir)
 if err!=nil||len(entries)!=2{
  t.Fatalf("local immutable nonce claims missing: %v, %d",err,len(entries))
 }
 if runtime.GOOS!="windows"{
  info,err:=os.Stat(dir);if err!=nil{t.Fatal(err)}
  if info.Mode().Perm()!=0700{t.Fatalf("nonce ledger not private: %v",info.Mode())}
  if err=os.Chmod(dir,0755);err!=nil{t.Fatal(err)}
  if err=claimLocalProbeOnce(keyPath,instrumentprobe.Challenge{
   SigningPayload:[]byte("new human challenge after unsafe permission")});err==nil{
   t.Fatal("unsafe world-readable local nonce ledger was accepted")
  }
 }
}
func TestLocalInstrumentOneShotLedgerRejectsSymlink(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"instrument.secret")
 real:=filepath.Join(dir,"another-dir")
 if err:=os.Mkdir(real,0700);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(real,path+".onepane-probe-claims");err!=nil{
  if runtime.GOOS=="windows"{t.Skip("Windows symlink privilege unavailable")}
  t.Fatal(err)
 }
 if err:=claimLocalProbeOnce(path,instrumentprobe.Challenge{
  SigningPayload:[]byte("scope-bound fresh challenge")});err==nil{
  t.Fatal("untrusted symlink masqueraded as private nonce ledger")
 }
}
func TestLocalInstrumentPrivateKeyRejectsIncorrectEmbeddedPublicPart(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"key.txt")
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 tampered:=append([]byte(nil),key...)
 tampered[len(tampered)-1]^=1
 if err=os.WriteFile(path,[]byte(base64.StdEncoding.EncodeToString(tampered)),0600);err!=nil{t.Fatal(err)}
 if _,err=readPrivateKey(path);err==nil{
  t.Fatal("corrupt private instrument file passed self-check before probe")
 }
}
