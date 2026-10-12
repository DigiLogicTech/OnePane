package instrumentprobe

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "fmt"
 "strings"
 "errors"
 "net"
 "testing"
 "time"
 "encoding/json"
)

type recordingDialer struct{
 calls int
 network,address string
 err error
}
func(d *recordingDialer)DialContext(_ context.Context,n,a string)(net.Conn,error){
 d.calls++;d.network=n;d.address=a
 if d.err!=nil{return nil,d.err}
 client,server:=net.Pipe()
 _=server.Close()
 return client,nil
}
func fixtureChallenge(t *testing.T,now time.Time)Challenge{
 t.Helper()
 ch:=Challenge{ID:"challenge1",KeyID:"instrument1",
  ProjectID:"project1",GoalID:"goal1",
  Nonce:make([]byte,32),IssuedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),Status:"issued"}
 for i:=range ch.Nonce{ch.Nonce[i]=byte(i+1)}
 env:=challengeEnvelope{
  Domain:challengeDomain,ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",
  PinnedCertificateSHA256:"sha256:"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  PlanHash:"sha256:"+"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  Nonce:ch.Nonce,IssuedAt:ch.IssuedAt,ExpiresAt:ch.ExpiresAt,
 }
 var err error
 ch.SigningPayload,err=json.Marshal(env)
 if err!=nil{t.Fatal(err)}
 return ch
}
func TestProbeOnceOnePrivateIPv4ConnectionAndLocalSignature(t *testing.T){
 now:=time.Now()
 ch:=fixtureChallenge(t,now)
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 dial:=&recordingDialer{}
 target:=Target{IPv4:"192.168.5.9",Port:443,TimeoutMS:500,
  ConfirmedDeclaredTargetID:"target1"}
 receipt,err:=ProbeOnce(context.Background(),ch,target,key,dial,func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 if dial.calls!=1||dial.network!="tcp4"||dial.address!="192.168.5.9:443"||
  receipt.Outcome!="connected"||len(receipt.LocalReportSignature)!=64{
  t.Fatalf("probe did not open exactly one private TCP endpoint: %+v %+v",dial,receipt)
 }
 raw,err:=localReceiptSigningBytes(receipt)
 if err!=nil{t.Fatal(err)}
 if !ed25519.Verify(key.Public().(ed25519.PublicKey),raw,receipt.LocalReportSignature){
  t.Fatal("local report not signed by instrument key")
 }
 preparedMsg:=measurementEnvelope{
  Domain:measurementDomain,ChallengePayload:ch.SigningPayload,
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,InstrumentKeyID:ch.KeyID,
  ChallengeID:ch.ID,DeclaredTargetID:"target1",TLSPinID:"pin1",
  SourceID:"router",MeasurementType:"endpoint_tcp_connect_claim",
  Outcome:receipt.Outcome,ObservedAt:receipt.ObservedAt,
 }
 preparedBytes,err:=json.Marshal(preparedMsg)
 if err!=nil{t.Fatal(err)}
 prep:=PreparedMeasurement{ChallengeID:ch.ID,MeasurementType:preparedMsg.MeasurementType,
  Outcome:receipt.Outcome,ObservedAt:receipt.ObservedAt,
  SigningPayload:preparedBytes,ExpiresAt:ch.ExpiresAt}
 acceptance,err:=SignPrepared(receipt,prep,key,7,now)
 if err!=nil{t.Fatalf("matching locally executed probe could not sign API payload: %v",err)}
 if acceptance.ExpectedGoalRevision!=7||acceptance.Outcome!="connected"||
  !ed25519.Verify(key.Public().(ed25519.PublicKey),preparedBytes,acceptance.Signature){
  t.Fatalf("signed acceptance cannot verify: %+v",acceptance)
 }
 badPrep:=prep;badPrep.Outcome="not_connected"
 if _,err=SignPrepared(receipt,badPrep,key,7,now);err==nil{
  t.Fatal("client signed a different outcome than its own TCP probe")
 }
 badPrep=prep;badPrep.ObserverVantageVerified=true
 if _,err=SignPrepared(receipt,badPrep,key,7,now);err==nil{
  t.Fatal("client admitted a forged validated hardware vantage flag")
 }
 wrongPin:=preparedMsg
 wrongPin.TLSPinID="different-pinned-endpoint"
 mismatchedPinPayload,err:=json.Marshal(wrongPin)
 if err!=nil{t.Fatal(err)}
 badPrep=prep;badPrep.SigningPayload=mismatchedPinPayload
 if _,err=SignPrepared(receipt,badPrep,key,7,now);err==nil{
  t.Fatal("client signed a different pinned TLS endpoint than its original challenge")
 }
 badPrep=prep;badPrep.SigningPayload=append([]byte(nil),prep.SigningPayload...)
 badPrep.SigningPayload[5]^=1
 if _,err=SignPrepared(receipt,badPrep,key,7,now);err==nil{
  t.Fatal("client signed modified canonical server payload")
 }
 fakeReceipt:=receipt
 fakeReceipt.Outcome="not_connected"
 if _,err=SignPrepared(fakeReceipt,prep,key,7,now);err==nil{
  t.Fatal("tampered local signed probe report accepted")
 }
 _,wrong,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if _,err=SignPrepared(receipt,prep,wrong,7,now);err==nil{
  t.Fatal("wrong local signing key authenticated someone else's report")
 }
 if _,err=SignPrepared(receipt,prep,key,7,now.Add(3*time.Minute));err==nil{
  t.Fatal("expired challenge generated a new signed API import")
 }
}
func TestProbeRefusesUnsafeTargetsAndExpiredChallengeWithoutSocket(t *testing.T){
 now:=time.Now()
 ch:=fixtureChallenge(t,now)
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 for _,ip:=range []string{
  "127.0.0.1","169.254.169.254","8.8.8.8","1.1.1.1",
  "100.64.0.1","0.0.0.0","::1","192.168.1.3:443",
  "router.home.arpa","192.168.1.001",
 }{
  t.Run(ip,func(t *testing.T){
   d:=&recordingDialer{}
   target:=Target{IPv4:ip,Port:443,TimeoutMS:700,
    ConfirmedDeclaredTargetID:"target1"}
   if _,err:=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now});err==nil||d.calls!=0{
    t.Fatalf("unsafe IP %s opened a socket or returned success: %v %+v",ip,err,d)
   }
  })
 }
 d:=&recordingDialer{}
 target:=Target{IPv4:"10.9.8.7",Port:443,TimeoutMS:200,
  ConfirmedDeclaredTargetID:"wrong"}
 if _,err=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now});err==nil||d.calls!=0{
  t.Fatal("wrong human-declared target identity opened TCP connection")
 }
 target.ConfirmedDeclaredTargetID="target1";target.TimeoutMS=5000
 if _,err=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now});err==nil||d.calls!=0{
  t.Fatal("unsafe timeout bypassed bounded operation")
 }
 target.TimeoutMS=500;ch.Status="accepted"
 if _,err=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now});err==nil||d.calls!=0{
  t.Fatal("consumed signing challenge reopened socket")
 }
 ch=fixtureChallenge(t,now)
 if _,err=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now.Add(3*time.Minute)});err==nil||d.calls!=0{
  t.Fatal("expired challenge caused network activity")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=ProbeOnce(ctx,ch,target,key,d,func()time.Time{return now});err==nil||d.calls!=0{
  t.Fatal("cancelled operation caused socket connection")
 }
}
func TestProbeNeverRetriesFailedConnection(t *testing.T){
 now:=time.Now();ch:=fixtureChallenge(t,now)
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 d:=&recordingDialer{err:errors.New("connection refused")}
 target:=Target{IPv4:"172.16.0.23",Port:443,TimeoutMS:500,
  ConfirmedDeclaredTargetID:"target1"}
 r,err:=ProbeOnce(context.Background(),ch,target,key,d,func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 if d.calls!=1||r.Outcome!="not_connected"{
  t.Fatalf("failed TCP handshake caused retry or success: %+v %+v",d,r)
 }
}

func TestApprovedProbePlanBindsCurrentOneShotEndpointAndRejectsTampering(t *testing.T){
 now:=time.Now()
 ch:=fixtureChallenge(t,now)
 digest:=sha256.Sum256(ch.SigningPayload)
 plan:=ProbePlan{
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:"192.168.48.25",TargetPort:8443,TimeoutMS:500,
  OriginalChallengeSHA256:fmt.Sprintf("sha256:%x",digest[:]),
  ExpiresAt:ch.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 dial:=&recordingDialer{}
 r,err:=ProbeApprovedPlan(context.Background(),ch,plan,key,dial,func()time.Time{return now})
 if err!=nil{t.Fatalf("correct approved probe plan was rejected: %v",err)}
 if r.Outcome!="connected"||dial.calls!=1||dial.address!="192.168.48.25:8443"{
  t.Fatalf("plan did not dictate exactly one approved socket: %+v %+v",dial,r)
 }
 cases:=map[string]func(*ProbePlan){
  "different_project":func(p *ProbePlan){p.ProjectID="another"},
  "different_key":func(p *ProbePlan){p.InstrumentKeyID="another"},
  "other_nonce":func(p *ProbePlan){p.ChallengeID="other-challenge"},
  "different_declared_target":func(p *ProbePlan){p.DeclaredTargetID="another"},
  "different_tls_pin":func(p *ProbePlan){p.TLSPinID="other-pin"},
  "different_challenge_digest":func(p *ProbePlan){p.OriginalChallengeSHA256="sha256:"+strings.Repeat("0",64)},
  "extra_expiry":func(p *ProbePlan){p.ExpiresAt+=120000},
  "false_vantage_attestation":func(p *ProbePlan){p.ObserverVantageVerified=true},
  "false_device_identity":func(p *ProbePlan){p.TargetDeviceIdentityVerified=true},
  "ambiguous_provenance":func(p *ProbePlan){p.AttestationClass="hardware_attested"},
  "missing_source":func(p *ProbePlan){p.SourceID=""},
  "public_ip":func(p *ProbePlan){p.TargetIPv4="8.8.8.8"},
  "public_metadata":func(p *ProbePlan){p.TargetIPv4="169.254.169.254"},
  "unbounded_timeout":func(p *ProbePlan){p.TimeoutMS=50000},
 }
 for name,mutate:=range cases{
  t.Run(name,func(t *testing.T){
   candidate:=plan;mutate(&candidate)
   blocked:=&recordingDialer{}
   if _,err=ProbeApprovedPlan(context.Background(),ch,candidate,
    key,blocked,func()time.Time{return now});err==nil||blocked.calls!=0{
    t.Fatalf("untrusted plan mutation opened socket: %v %+v",err,blocked)
   }
  })
 }
}
