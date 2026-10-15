package nodefederation

import (
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "crypto/x509"
 "encoding/json"
 "fmt"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
)

func custodyTestFixture(t *testing.T)(instrumentprobe.Challenge,
 instrumentprobe.ProbePlan,instrumentprobe.LocalProbeReceipt,
 ed25519.PublicKey,ed25519.PrivateKey,time.Time){
 t.Helper()
 now:=time.Now().UTC().Truncate(time.Millisecond)
 pub,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 targetIP:="192.168.66.20"
 targetPort:=8443
 digest:=sha256.Sum256([]byte(fmt.Sprintf(
  "OnePane/target-endpoint/v1|%s:%d",targetIP,targetPort)))
 ch:=instrumentprobe.Challenge{
  ID:"challenge1",KeyID:"instrument1",ProjectID:"project1",
  GoalID:"goal1",Nonce:make([]byte,32),IssuedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),Status:"issued",
 }
 for i:=range ch.Nonce{ch.Nonce[i]=byte(i+1)}
 env:=struct{
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
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,InstrumentKeyID:ch.KeyID,
  ChallengeID:ch.ID,DeclaredTargetID:"target1",TLSPinID:"pin1",
  PinnedCertificateSHA256:"sha256:"+strings.Repeat("a",64),
  PlanHash:"sha256:"+strings.Repeat("b",64),
  TargetEndpointSHA256:fmt.Sprintf("sha256:%x",digest[:]),
  Nonce:ch.Nonce,IssuedAt:ch.IssuedAt,ExpiresAt:ch.ExpiresAt,
 }
 ch.SigningPayload,err=json.Marshal(env)
 if err!=nil{t.Fatal(err)}
 plan:=instrumentprobe.ProbePlan{
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:targetIP,TargetPort:targetPort,TimeoutMS:500,
  OriginalChallengeSHA256:custodyHash(ch.SigningPayload),
  ExpiresAt:ch.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 receipt:=instrumentprobe.LocalProbeReceipt{
  Challenge:ch,Target:instrumentprobe.Target{
   IPv4:targetIP,Port:targetPort,TimeoutMS:500,
   ConfirmedDeclaredTargetID:"target1",
  },
  Outcome:"connected",ObservedAt:now.UnixMilli(),
 }
 localMsg,err:=json.Marshal(struct{
  Domain string `json:"domain"`
  ChallengePayload []byte `json:"challenge_payload"`
  Target instrumentprobe.Target `json:"target"`
  Outcome string `json:"outcome"`
  ObservedAt int64 `json:"observed_at"`
 }{
  Domain:"OnePane/local-one-shot-probe/ed25519/v1",
  ChallengePayload:ch.SigningPayload,Target:receipt.Target,
  Outcome:receipt.Outcome,ObservedAt:receipt.ObservedAt,
 })
 if err!=nil{t.Fatal(err)}
 receipt.LocalReportSignature=ed25519.Sign(private,localMsg)
 if err=instrumentprobe.VerifyLocalReceiptAgainstApproval(receipt,pub,ch,plan);
  err!=nil{t.Fatalf("fixture does not match real OnePane instrument proof: %v",err)}
 return ch,plan,receipt,pub,private,now
}

func TestNodeCustodyCountersignatureRequiresOriginalApprovalsAndTwoKeys(t *testing.T){
 ch,plan,receipt,instrumentKey,_,now:=custodyTestFixture(t)
 node,err:=EnsureIdentity(t.TempDir(),"paired-node")
 if err!=nil{t.Fatal(err)}
 witnessedAt:=now.Add(time.Second)
 w,err:=SignLocalInstrumentCustody("paired-node",node,receipt,ch,
  plan,instrumentKey,witnessedAt)
 if err!=nil{t.Fatalf("valid private evidence could not be countersigned: %v",err)}
 cert:=node.Certificate.Certificate[0]
 verify:=func(w NodeInstrumentCustodyWitness,nodeDER []byte,pin string,
  key ed25519.PublicKey,r instrumentprobe.LocalProbeReceipt,
  original instrumentprobe.Challenge,p instrumentprobe.ProbePlan)error{
  return VerifyLocalInstrumentCustody(w,nodeDER,pin,key,r,original,p)
 }
 if err=verify(w,cert,node.Fingerprint,instrumentKey,receipt,ch,plan);err!=nil{
  t.Fatalf("valid separately pinned two-key chain rejected: %v",err)
 }
 // Numeric port digits are NOT a valid raw-port leak check: the
 // random key digest or Unix timestamp can independently contain "8443".
 // Instead inspect the typed envelope's exact allowed metadata keys.
 var witnessFields map[string]json.RawMessage
 if err=json.Unmarshal(w.SigningPayload,&witnessFields);err!=nil{t.Fatal(err)}
 for _,name:=range []string{
  "target_ipv4","target_port","ipv4","port","host","address","socket",
 }{
  if _,present:=witnessFields[name];present{
   t.Fatalf("private socket field %s leaked into Node custody envelope",name)
  }
 }
 if strings.Contains(string(w.SigningPayload),plan.TargetIPv4){
  t.Fatal("original private IPv4 literal leaked into Node custody envelope")
 }
 if err=verify(w,cert,"sha256:"+strings.Repeat("0",64),
  instrumentKey,receipt,ch,plan);err==nil{
  t.Fatal("wrong independently enrolled Node pin was accepted")
 }
 other,err:=EnsureIdentity(t.TempDir(),"unrelated-node")
 if err!=nil{t.Fatal(err)}
 if err=verify(w,other.Certificate.Certificate[0],other.Fingerprint,
  instrumentKey,receipt,ch,plan);err==nil{
  t.Fatal("unrelated Node certificate verified another Node signature")
 }
 otherInstrument,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if err=verify(w,cert,node.Fingerprint,otherInstrument,receipt,ch,plan);err==nil{
  t.Fatal("unrelated instrument public key verified original report")
 }
 mutated:=w
 mutated.Signature=append([]byte(nil),w.Signature...)
 mutated.Signature[3]^=1
 if err=verify(mutated,cert,node.Fingerprint,
  instrumentKey,receipt,ch,plan);err==nil{
  t.Fatal("tampered Node signature passed independent verifier")
 }
 mutated=w
 mutated.SigningPayload=append([]byte(nil),w.SigningPayload...)
 mutated.SigningPayload=append(mutated.SigningPayload,' ')
 if err=verify(mutated,cert,node.Fingerprint,
  instrumentKey,receipt,ch,plan);err==nil{
  t.Fatal("noncanonical/witness-injected extra bytes verified")
 }
 changedPlan:=plan
 changedPlan.TargetIPv4="192.168.66.21"
 if err=verify(w,cert,node.Fingerprint,
  instrumentKey,receipt,ch,changedPlan);err==nil{
  t.Fatal("different operator target plan verified")
 }
 changedChallenge:=ch
 changedChallenge.Nonce=append([]byte(nil),ch.Nonce...)
 changedChallenge.Nonce[0]^=1
 if err=verify(w,cert,node.Fingerprint,
  instrumentKey,receipt,changedChallenge,plan);err==nil{
  t.Fatal("unissued nonce substitution passed custody verification")
 }
 changedReceipt:=receipt
 changedReceipt.Outcome="not_connected"
 if err=verify(w,cert,node.Fingerprint,
  instrumentKey,changedReceipt,ch,plan);err==nil{
  t.Fatal("different report outcome passed custody verification")
 }
}

func TestNodeCustodyCannotWitnessStaleUnsignedOrFalseVantageReport(t *testing.T){
 ch,plan,receipt,pub,_,now:=custodyTestFixture(t)
 node,err:=EnsureIdentity(t.TempDir(),"paired-node")
 if err!=nil{t.Fatal(err)}
 checks:=[]struct{
  name string
  change func(*instrumentprobe.Challenge,*instrumentprobe.ProbePlan,
   *instrumentprobe.LocalProbeReceipt,*ed25519.PublicKey,*time.Time)
 }{
  {"past_expiry",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   *at=time.UnixMilli(ch.ExpiresAt)
  }},
  {"over_sixty_seconds",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   *at=time.UnixMilli(r.ObservedAt).Add(61*time.Second)
   ch.ExpiresAt=r.ObservedAt+120000;p.ExpiresAt=ch.ExpiresAt
  }},
  {"not_in_issued_scope",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   p.SourceID=""
  }},
  {"false_vantage",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   p.ObserverVantageVerified=true
  }},
  {"invalid_instrument_signature",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   r.LocalReportSignature[0]^=1
  }},
  {"not_enrolled_node",func(ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan,
   r *instrumentprobe.LocalProbeReceipt,k *ed25519.PublicKey,at *time.Time){
   *k=ed25519.PublicKey(make([]byte,32))
  }},
 }
 for _,tc:=range checks{
  t.Run(tc.name,func(t *testing.T){
   a:=ch;b:=plan;c:=receipt
   c.LocalReportSignature=append([]byte(nil),receipt.LocalReportSignature...)
   key:=append(ed25519.PublicKey(nil),pub...)
   at:=now.Add(time.Second)
   tc.change(&a,&b,&c,&key,&at)
   if _,err:=SignLocalInstrumentCustody("paired-node",node,c,a,b,key,at);err==nil{
    t.Fatal("untrusted claim received Node custody countersignature")
   }
  })
 }
}

func TestNodeCustodyPreflightRejectsWrongSignerOrExpiredIdentity(t *testing.T){
 ident,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 if err=ValidateLocalInstrumentCustodyIdentity("peer",ident,time.Now());err!=nil{
  t.Fatalf("valid enrolled local Node signer rejected before effect: %v",err)
 }
 // A valid pinned identity must be an enrolled signing leaf, not a CA.
 leaf,err:=x509.ParseCertificate(ident.Certificate.Certificate[0])
 if err!=nil{t.Fatal(err)}
 asCA:=*leaf
 asCA.IsCA=true
 if err=validateCustodyCert(&asCA,"peer",ident.Fingerprint,time.Now().UnixMilli());err==nil{
  t.Fatal("CA credential was promoted to a Node leaf identity")
 }
 asCertSigner:=*leaf
 asCertSigner.KeyUsage|=x509.KeyUsageCertSign
 if err=validateCustodyCert(&asCertSigner,"peer",ident.Fingerprint,time.Now().UnixMilli());err==nil{
  t.Fatal("certificate-signing credential was promoted to a Node leaf identity")
 }
 if err=ValidateLocalInstrumentCustodyIdentity("impostor",ident,time.Now());err==nil{
  t.Fatal("wrong Node ID accepted for pre-effect key signing")
 }
 if err=ValidateLocalInstrumentCustodyIdentity("peer",ident,
  time.Now().AddDate(12,0,0));err==nil{
  t.Fatal("expired Node certificate accepted before probe")
 }
 other,err:=EnsureIdentity(t.TempDir(),"other")
 if err!=nil{t.Fatal(err)}
 wrong:=ident
 wrong.Certificate.PrivateKey=other.Certificate.PrivateKey
 if err=ValidateLocalInstrumentCustodyIdentity("peer",wrong,time.Now());err==nil{
  t.Fatal("wrong private signer approved Node execution")
 }
 wrong=ident
 wrong.Fingerprint=other.Fingerprint
 if err=ValidateLocalInstrumentCustodyIdentity("peer",wrong,time.Now());err==nil{
  t.Fatal("different registered Node pin accepted")
 }
}
