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
 return fixtureChallengeForEndpoint(t,now,"192.168.5.9",443)
}
func fixtureChallengeForEndpoint(t *testing.T,now time.Time,
 ip string,port int)Challenge{
 t.Helper()
 ch:=Challenge{ID:"challenge1",KeyID:"instrument1",
  ProjectID:"project1",GoalID:"goal1",
  Nonce:make([]byte,32),IssuedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),Status:"issued"}
 for i:=range ch.Nonce{ch.Nonce[i]=byte(i+1)}
 env:=challengeEnvelope{
  Domain:challengeDomainV2,ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",
  PinnedCertificateSHA256:"sha256:"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  PlanHash:"sha256:"+"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  TargetEndpointSHA256:endpointDigest(ip,port),
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
func TestV2ManualProbeRejectsDifferentPrivateIPv4OrPortBeforeAnySocket(t *testing.T){
 now:=time.Now()
 ch:=fixtureChallengeForEndpoint(t,now,"192.168.5.9",443)
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 target:=Target{IPv4:"192.168.5.10",Port:443,TimeoutMS:500,
  ConfirmedDeclaredTargetID:"target1"}
 for _,change:=range []struct{
  label string
  ip string
  port int
 }{
  {"different-private-host","192.168.5.10",443},
  {"different-port","192.168.5.9",8443},
 }{
  t.Run(change.label,func(t *testing.T){
   target.IPv4=change.ip;target.Port=change.port
   dial:=&recordingDialer{}
   if _,err=ProbeOnce(context.Background(),ch,target,key,dial,
    func()time.Time{return now});err==nil||dial.calls!=0{
    t.Fatalf("v2 manual fallback dialled an unapproved private endpoint: %v %+v",err,dial)
   }
  })
 }
 // A caller with the enrolled private key can re-sign a local receipt,
 // but a v2 mismatch must still be refused on the later import path.
 approved:=Target{IPv4:"192.168.5.9",Port:443,TimeoutMS:500,
  ConfirmedDeclaredTargetID:"target1"}
 dial:=&recordingDialer{}
 receipt,err:=ProbeOnce(context.Background(),ch,approved,key,dial,
  func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 altered:=receipt
 altered.Target.IPv4="192.168.5.10"
 original,err:=localReceiptSigningBytes(altered)
 if err!=nil{t.Fatal(err)}
 altered.LocalReportSignature=ed25519.Sign(key,original)
 payload,err:=json.Marshal(measurementEnvelope{
  Domain:measurementDomain,ChallengePayload:ch.SigningPayload,
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,DeclaredTargetID:"target1",
  TLSPinID:"pin1",SourceID:"router",
  MeasurementType:"endpoint_tcp_connect_claim",
  Outcome:altered.Outcome,ObservedAt:altered.ObservedAt,
 })
 if err!=nil{t.Fatal(err)}
 prepared:=PreparedMeasurement{
  ChallengeID:ch.ID,MeasurementType:"endpoint_tcp_connect_claim",
  Outcome:altered.Outcome,ObservedAt:altered.ObservedAt,
  SigningPayload:payload,ExpiresAt:ch.ExpiresAt,
 }
 if _,err=SignPrepared(altered,prepared,key,1,now);err==nil{
  t.Fatal("resigned off-target local result gained admission under original v2 target")
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
 now:=time.Now();ch:=fixtureChallengeForEndpoint(t,now,"172.16.0.23",443)
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
 ch:=fixtureChallengeForEndpoint(t,now,"192.168.48.25",8443)
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
  "other_private_ip":func(p *ProbePlan){p.TargetIPv4="192.168.48.26"},
  "other_approved_port":func(p *ProbePlan){p.TargetPort=443},
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

type exactMappedDialer struct{
 target string
 listenerAddress string
 called int
}
func(d *exactMappedDialer)DialContext(ctx context.Context,network,address string)(net.Conn,error){
 d.called++
 if network!="tcp4"||address!=d.target{
  return nil,errors.New("client attempted an unapproved destination")
 }
 return (&net.Dialer{}).DialContext(ctx,"tcp4",d.listenerAddress)
}

func TestApprovedPlanUsesRealTCPConnectWithoutScanning(t *testing.T){
 l,err:=net.Listen("tcp4","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer l.Close()
 accepted:=make(chan struct{},1)
 go func(){
  conn,err:=l.Accept()
  if err==nil{_ = conn.Close();accepted<-struct{}{}}
 }()
 now:=time.Now()
 ch:=fixtureChallengeForEndpoint(t,now,"10.16.5.19",443)
 keyPublic,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(ch.SigningPayload)
 plan:=ProbePlan{
  ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:"target1",TLSPinID:"pin1",SourceID:"router",
  TargetIPv4:"10.16.5.19",TargetPort:443,TimeoutMS:900,
  OriginalChallengeSHA256:fmt.Sprintf("sha256:%x",sum[:]),
  ExpiresAt:ch.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 dialer:=&exactMappedDialer{
  target:"10.16.5.19:443",listenerAddress:l.Addr().String(),
 }
 receipt,err:=ProbeApprovedPlan(context.Background(),ch,plan,key,dialer,
  func()time.Time{return now})
 if err!=nil{t.Fatalf("approved single TCP handshake failed: %v",err)}
 if dialer.called!=1||receipt.Outcome!="connected"||
  receipt.Target.IPv4!="10.16.5.19"||
  receipt.Target.Port!=443{
  t.Fatalf("one-shot actual TCP connect not bound to approved endpoint: %+v",receipt)
 }
 select{
 case <-accepted:
 case <-time.After(2*time.Second):
  t.Fatal("real local test server did not receive the bounded TCP connect")
 }
 signed,err:=localReceiptSigningBytes(receipt)
 if err!=nil{t.Fatal(err)}
 if !ed25519.Verify(keyPublic,signed,receipt.LocalReportSignature){
  t.Fatal("real TCP handshake receipt lacks independently valid signature")
 }
}

func TestHistoricalV1ChallengeOnlyAllowedAsExplicitManualFallback(t *testing.T){
 now:=time.Now()
 ch:=fixtureChallengeForEndpoint(t,now,"192.168.7.12",443)
 var env challengeEnvelope
 if err:=json.Unmarshal(ch.SigningPayload,&env);err!=nil{t.Fatal(err)}
 env.Domain=challengeDomainV1
 env.TargetEndpointSHA256=""
 var err error
 ch.SigningPayload,err=json.Marshal(env)
 if err!=nil{t.Fatal(err)}
 if _,err=ParseChallenge(ch,now);err!=nil{
  t.Fatalf("historical signed v1 challenge wrongly unreadable: %v",err)
 }
 sum:=sha256.Sum256(ch.SigningPayload)
 plan:=ProbePlan{ProjectID:ch.ProjectID,GoalID:ch.GoalID,
  InstrumentKeyID:ch.KeyID,ChallengeID:ch.ID,
  DeclaredTargetID:env.DeclaredTargetID,TLSPinID:env.TLSPinID,
  SourceID:"router",TargetIPv4:"192.168.7.12",TargetPort:443,
  TimeoutMS:500,OriginalChallengeSHA256:fmt.Sprintf("sha256:%x",sum[:]),
  ExpiresAt:ch.ExpiresAt,
  AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
 }
 _,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 dial:=&recordingDialer{}
 if _,err=ProbeApprovedPlan(context.Background(),ch,plan,key,dial,
  func()time.Time{return now});err==nil||dial.calls!=0{
  t.Fatal("legacy v1 nonce lacking endpoint hash gained authenticated plan authority")
 }
}
