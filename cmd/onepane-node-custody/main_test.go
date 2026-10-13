package main

import (
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

func writeCanonicalTest(t *testing.T,dir,name string,v any)string{
 t.Helper()
 raw,err:=json.Marshal(v)
 if err!=nil{t.Fatal(err)}
 path:=filepath.Join(dir,name)
 if err=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
 return path
}

func fixtureNodeCustodyCLI(t *testing.T)(string,string,string,
 string,string,string,string){
 t.Helper()
 dir:=t.TempDir()
 dataDir:=filepath.Join(dir,"node")
 ident,err:=nodefederation.EnsureIdentity(dataDir,"peer")
 if err!=nil{t.Fatal(err)}
 certPath:=filepath.Join(dataDir,"keys","federation.crt")
 now:=time.Now().UTC().Truncate(time.Millisecond)
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 ip:="192.168.21.55";port:=8443
 endpointHash:=sha256.Sum256([]byte(fmt.Sprintf(
  "OnePane/target-endpoint/v1|%s:%d",ip,port)))
 ch:=instrumentprobe.Challenge{
  ID:"challenge1",KeyID:"instrument1",
  ProjectID:"project1",GoalID:"goal1",Nonce:make([]byte,32),
  IssuedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),Status:"issued",
 }
 for i:=range ch.Nonce{ch.Nonce[i]=byte(i+1)}
 payload:=struct{
  Domain string `json:"domain"`
  ProjectID string `json:"project_id"`
  GoalID string `json:"goal_id"`
  InstrumentKeyID string `json:"instrument_key_id"`
  ChallengeID string `json:"challenge_id"`
  DeclaredTargetID string `json:"declared_target_id"`
  TLSPinID string `json:"tls_pin_id"`
  PinnedCertificateSHA256 string `json:"pinned_certificate_sha256"`
  PlanHash string `json:"plan_hash"`
  TargetEndpointSHA256 string `json:"target_endpoint_sha256,omitempty"`
  Nonce []byte `json:"nonce"`
  IssuedAt int64 `json:"issued_at"`
  ExpiresAt int64 `json:"expires_at"`
 }{
  Domain:"OnePane/instrument-challenge/ed25519/v2",
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",
  PinnedCertificateSHA256:"sha256:"+strings.Repeat("a",64),
  PlanHash:"sha256:"+strings.Repeat("b",64),
  TargetEndpointSHA256:fmt.Sprintf("sha256:%x",endpointHash[:]),
  Nonce:ch.Nonce,IssuedAt:ch.IssuedAt,ExpiresAt:ch.ExpiresAt,
 }
 ch.SigningPayload,err=json.Marshal(payload)
 if err!=nil{t.Fatal(err)}
 challengeHash:=sha256.Sum256(ch.SigningPayload)
 plan:=instrumentprobe.ProbePlan{
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:ip,TargetPort:port,TimeoutMS:500,
  OriginalChallengeSHA256:fmt.Sprintf("sha256:%x",challengeHash[:]),
  ExpiresAt:ch.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 receipt:=instrumentprobe.LocalProbeReceipt{
  Challenge:ch,Target:instrumentprobe.Target{
   IPv4:ip,Port:port,TimeoutMS:500,
   ConfirmedDeclaredTargetID:"target1",
  },
  Outcome:"not_connected",ObservedAt:now.UnixMilli(),
 }
 signingPayload:=struct{
  Domain string `json:"domain"`
  ChallengePayload []byte `json:"challenge_payload"`
  Target instrumentprobe.Target `json:"target"`
  Outcome string `json:"outcome"`
  ObservedAt int64 `json:"observed_at"`
 }{"OnePane/local-one-shot-probe/ed25519/v1",
  ch.SigningPayload,receipt.Target,receipt.Outcome,receipt.ObservedAt}
 raw,err:=json.Marshal(signingPayload)
 if err!=nil{t.Fatal(err)}
 receipt.LocalReportSignature=ed25519.Sign(priv,raw)
 if err=instrumentprobe.VerifyLocalReceiptAgainstApproval(receipt,pub,ch,plan);
  err!=nil{t.Fatal(err)}
 receiptPath:=writeCanonicalTest(t,dir,"receipt.json",receipt)
 challengePath:=writeCanonicalTest(t,dir,"challenge.json",ch)
 planPath:=writeCanonicalTest(t,dir,"plan.json",plan)
 if ident.Fingerprint==""{t.Fatal("unprovisioned Node identity")}
 return dataDir,receiptPath,challengePath,planPath,certPath,
  ident.Fingerprint,base64.StdEncoding.EncodeToString(pub)
}

func TestManualNodeCustodySignVerifyAndExclusiveEvidence(t *testing.T){
 dataDir,receipt,ch,plan,cert,fp,pub:=fixtureNodeCustodyCLI(t)
 out:=filepath.Join(filepath.Dir(receipt),"witness.json")
 sign:=[]string{"--data-dir",dataDir,"--node-id","peer",
  "--receipt",receipt,"--challenge",ch,"--plan",plan,
  "--instrument-public-key",pub,"--output",out}
 if err:=runSign(sign);err!=nil{t.Fatalf("manual Node custody sign failed: %v",err)}
 verify:=[]string{"--witness",out,"--receipt",receipt,"--challenge",ch,
  "--plan",plan,"--node-certificate",cert,"--node-fingerprint",fp,
  "--instrument-public-key",pub}
 if err:=runVerify(verify);err!=nil{
  t.Fatalf("separate pinned verifier denied legitimate Node report: %v",err)
 }
 if err:=runSign(sign);!os.IsExist(err){
  t.Fatalf("repeat sign overwrote existing evidence: %v",err)
 }
 if runtime.GOOS!="windows"{
  stat,err:=os.Stat(out)
  if err!=nil{t.Fatal(err)}
  if stat.Mode().Perm()!=0600{t.Fatal("Node witness evidence was not owner-only")}
  if err=os.Chmod(plan,0644);err!=nil{t.Fatal(err)}
  if err=runSign(append([]string(nil),sign...));err==nil||
   !strings.Contains(err.Error(),"owner-only"){
   t.Fatalf("shared private network topology was read: %v",err)
  }
  if err=runVerify(verify);err==nil||
   !strings.Contains(err.Error(),"owner-only"){
   t.Fatalf("verifier disclosed shared private topology: %v",err)
  }
 }
}

func TestManualNodeCustodyNeverCreatesUnenrolledNode(t *testing.T){
 dataDir,receipt,ch,plan,_,_,pub:=fixtureNodeCustodyCLI(t)
 missing:=filepath.Join(dataDir,"no-enrollment")
 out:=filepath.Join(filepath.Dir(receipt),"cannot-sign.json")
 err:=runSign([]string{"--data-dir",missing,"--node-id","peer",
  "--receipt",receipt,"--challenge",ch,"--plan",plan,
  "--instrument-public-key",pub,"--output",out})
 if err==nil{t.Fatal("unprovisioned Node was allowed to mint a custody witness")}
 if _,err=os.Stat(filepath.Join(missing,"keys"));!os.IsNotExist(err){
  t.Fatal("Node custody signing created an unapproved Node identity")
 }
 if _,err=os.Stat(out);!os.IsNotExist(err){
  t.Fatal("unprovisioned signing reserved fake evidence")
 }
 if err=runVerify([]string{"--receipt",receipt});err==nil{
  t.Fatal("incomplete independently verified identity arguments accepted")
 }
}

func TestNodeCustodyCLIRejectsAmbiguousJsonAndSymlinks(t *testing.T){
 dir:=t.TempDir()
 ambiguous:=filepath.Join(dir,"ambiguous.json")
 if err:=os.WriteFile(ambiguous,[]byte(
  `{"signing_payload":"", "signing_payload":"", "signature":""}`),0600);err!=nil{t.Fatal(err)}
 var witness nodefederation.NodeInstrumentCustodyWitness
 if err:=readCanonical(ambiguous,&witness,true);err==nil{
  t.Fatal("duplicate JSON member admitted")
 }
 link:=filepath.Join(dir,"linked.json")
 if err:=os.Symlink(ambiguous,link);err==nil{
  if _,err:=readBounded(link,8192,true);err==nil{
   t.Fatal("symlink private evidence admitted")
  }
 }
}
