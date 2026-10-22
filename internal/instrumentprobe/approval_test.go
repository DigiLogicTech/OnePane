package instrumentprobe

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "fmt"
 "strings"
 "testing"
 "time"
)

func TestOfflineEvidenceMustMatchIndependentOriginalApproval(t *testing.T){
 now:=time.Now()
 original:=fixtureChallengeForEndpoint(t,now,"192.168.44.19",8443)
 hash:=sha256.Sum256(original.SigningPayload)
 plan:=ProbePlan{
  ProjectID:original.ProjectID,GoalID:original.GoalID,
  InstrumentKeyID:original.KeyID,ChallengeID:original.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:"192.168.44.19",TargetPort:8443,TimeoutMS:500,
  OriginalChallengeSHA256:fmt.Sprintf("sha256:%x",hash[:]),
  ExpiresAt:original.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 public,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 receipt,err:=ProbeApprovedPlan(context.Background(),original,plan,private,
  &recordingDialer{},func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 if err=VerifyLocalReceiptAgainstApproval(receipt,public,original,plan);err!=nil{
  t.Fatalf("valid independent approvals rejected: %v",err)
 }
 changed:=original
 changed.Nonce=append([]byte(nil),original.Nonce...)
 changed.Nonce[0]^=1
 if err=VerifyLocalReceiptAgainstApproval(receipt,public,changed,plan);err==nil{
  t.Fatal("different server nonce validated signed receipt")
 }
 changed=original;changed.ID="different"
 if err=VerifyLocalReceiptAgainstApproval(receipt,public,changed,plan);err==nil{
  t.Fatal("different issued challenge ID validated signed receipt")
 }
 for name,alter:=range map[string]func(*ProbePlan){
  "host":func(p *ProbePlan){p.TargetIPv4="192.168.44.20"},
  "port":func(p *ProbePlan){p.TargetPort=443},
  "source":func(p *ProbePlan){p.SourceID=""},
  "pin":func(p *ProbePlan){p.TLSPinID="another"},
  "hash":func(p *ProbePlan){p.OriginalChallengeSHA256="sha256:"+strings.Repeat("a",64)},
  "expiry":func(p *ProbePlan){p.ExpiresAt++},
  "vantage":func(p *ProbePlan){p.ObserverVantageVerified=true},
 }{
  t.Run(name,func(t *testing.T){
   altered:=plan;alter(&altered)
   if err:=VerifyLocalReceiptAgainstApproval(receipt,public,original,altered);err==nil{
    t.Fatal("mismatched independently retained approval was accepted")
   }
  })
 }
 // Even with the correct signing key, a report can select a different
 // timeout. Only the independent server plan proves that is not approved.
 alternate:=receipt
 alternate.Target.TimeoutMS=900
 signingBytes,err:=localReceiptSigningBytes(alternate)
 if err!=nil{t.Fatal(err)}
 alternate.LocalReportSignature=ed25519.Sign(private,signingBytes)
 if err=VerifyLocalReceipt(alternate,public);err!=nil{
  t.Fatalf("valid self-signed alternative receipt unexpectedly rejected: %v",err)
 }
 if err=VerifyLocalReceiptAgainstApproval(alternate,public,original,plan);err==nil{
  t.Fatal("self-signed timeout change bypassed independently approved plan")
 }
}
