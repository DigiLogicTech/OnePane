package main

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "errors"
 "net"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)
type recordingNodeDialer struct{
 calls int
 network,address string
 err error
}
func(d *recordingNodeDialer)DialContext(_ context.Context,network,address string)(net.Conn,error){
 d.calls++
 d.network=network;d.address=address
 if d.err!=nil{return nil,d.err}
 client,server:=net.Pipe()
 _=server.Close()
 return client,nil
}
func nodeProbeFixture(t *testing.T)(ExplicitNodeProbeCommand,
 instrumentprobe.Challenge,instrumentprobe.ProbePlan){
 t.Helper()
 dataDir,_,chFile,planFile,_,fp,_:=fixtureNodeCustodyCLI(t)
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 keyFile:=filepath.Join(filepath.Dir(chFile),"instrument.key")
 if err=os.WriteFile(keyFile,[]byte(base64.StdEncoding.EncodeToString(priv)),0600);
  err!=nil{t.Fatal(err)}
 var ch instrumentprobe.Challenge
 var plan instrumentprobe.ProbePlan
 if err=readCanonical(chFile,&ch,true);err!=nil{t.Fatal(err)}
 if err=readCanonical(planFile,&plan,true);err!=nil{t.Fatal(err)}
 c:=ExplicitNodeProbeCommand{
  NodeDataDir:dataDir,NodeID:"peer",NodeFingerprint:fp,
  InstrumentPrivateKeyFile:keyFile,
  InstrumentPublicKey:base64.StdEncoding.EncodeToString(pub),
  ChallengeFile:chFile,PlanFile:planFile,
  ReceiptOutput:filepath.Join(filepath.Dir(chFile),"node-exec-receipt.json"),
  WitnessOutput:filepath.Join(filepath.Dir(chFile),"node-exec-witness.json"),
 }
 return c,ch,plan
}
func TestNodeLocalProbeOneSocketTwoKeysAndSharedReplayLedger(t *testing.T){
 cmd,ch,plan:=nodeProbeFixture(t)
 dial:=&recordingNodeDialer{}
 if err:=ExecuteExplicitNodeProbe(context.Background(),cmd,dial,time.Now);
  err!=nil{t.Fatalf("approved Node one-shot failed: %v",err)}
 if dial.calls!=1||dial.network!="tcp4"||dial.address!="192.168.21.55:8443"{
  t.Fatalf("Node ran non-approved network effect: %+v",dial)
 }
 var receipt instrumentprobe.LocalProbeReceipt
 var witness nodefederation.NodeInstrumentCustodyWitness
 if err:=readCanonical(cmd.ReceiptOutput,&receipt,true);err!=nil{t.Fatal(err)}
 if err:=readCanonical(cmd.WitnessOutput,&witness,true);err!=nil{t.Fatal(err)}
 if receipt.Outcome!="connected"||receipt.Target.IPv4!=plan.TargetIPv4{
  t.Fatalf("unexpected Node self-report: %+v",receipt)
 }
 ident,err:=loadExistingNodeIdentity(cmd.NodeDataDir,cmd.NodeID);if err!=nil{t.Fatal(err)}
 pub,err:=parseInstrumentKey(cmd.InstrumentPublicKey);if err!=nil{t.Fatal(err)}
 if err=nodefederation.VerifyLocalInstrumentCustody(witness,
  ident.Certificate.Certificate[0],cmd.NodeFingerprint,pub,
  receipt,ch,plan);err!=nil{
  t.Fatalf("paired original two-key proof failed: %v",err)
 }
 if err=instrumentprobe.ClaimLocalProbeOnce(cmd.InstrumentPrivateKeyFile,ch);err==nil{
  t.Fatal("original instrument command could reuse spent Node nonce")
 }
 if err=os.Remove(cmd.ReceiptOutput);err!=nil{t.Fatal(err)}
 if err=os.Remove(cmd.WitnessOutput);err!=nil{t.Fatal(err)}
 if err=ExecuteExplicitNodeProbe(context.Background(),cmd,dial,time.Now);
  err==nil||dial.calls!=1{
  t.Fatalf("Node restart repeated protected socket: %v %+v",err,dial)
 }
}
func TestNodeLocalProbeRejectsBadAuthorityBeforeDial(t *testing.T){
 cases:=[]struct{name string;change func(*testing.T,*ExplicitNodeProbeCommand,
   *instrumentprobe.Challenge,*instrumentprobe.ProbePlan)}{
  {"wrong_node_pin",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   c.NodeFingerprint="sha256:"+strings.Repeat("0",64)
  }},
  {"wrong_instrument_key",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   pub,_,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
   c.InstrumentPublicKey=base64.StdEncoding.EncodeToString(pub)
  }},
  {"unprovisioned_node",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   c.NodeDataDir=filepath.Join(t.TempDir(),"unknown-node")
  }},
  {"changed_approved_port",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   p.TargetPort=8444;c.PlanFile=writeCanonicalTest(t,t.TempDir(),"different-plan",p)
  }},
  {"unsafe_public_host",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   p.TargetIPv4="8.8.8.8";c.PlanFile=writeCanonicalTest(t,t.TempDir(),"public-plan",p)
  }},
  {"false_hardware_vantage",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   p.ObserverVantageVerified=true
   c.PlanFile=writeCanonicalTest(t,t.TempDir(),"false-plan",p)
  }},
  {"changed_challenge_nonce",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   ch.Nonce=append([]byte(nil),ch.Nonce...);ch.Nonce[0]^=1
   c.ChallengeFile=writeCanonicalTest(t,t.TempDir(),"changed-challenge",ch)
  }},
  {"existing_witness_output",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   if err:=os.WriteFile(c.WitnessOutput,[]byte("reserved"),0600);err!=nil{t.Fatal(err)}
  }},
  {"overlapping_output",func(t *testing.T,c *ExplicitNodeProbeCommand,
   ch *instrumentprobe.Challenge,p *instrumentprobe.ProbePlan){
   c.WitnessOutput=c.ReceiptOutput
  }},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  c,ch,p:=nodeProbeFixture(t)
  tc.change(t,&c,&ch,&p)
  d:=&recordingNodeDialer{}
  if err:=ExecuteExplicitNodeProbe(context.Background(),c,d,time.Now);err==nil||d.calls!=0{
   t.Fatalf("unapproved Node operation dialled: %v %+v",err,d)
  }
  if err:=instrumentprobe.ClaimLocalProbeOnce(c.InstrumentPrivateKeyFile,ch);err!=nil{
   t.Fatalf("failed validation spent original one-shot nonce: %v",err)
  }
 })}
}
func TestNodeLocalProbeConnectionFailureSpendsNonceAndSignsOutcome(t *testing.T){
 c,ch,_:=nodeProbeFixture(t)
 d:=&recordingNodeDialer{err:errors.New("connection refused")}
 if err:=ExecuteExplicitNodeProbe(context.Background(),c,d,time.Now);err!=nil{
  t.Fatalf("bounded failed TCP connect did not produce signed report: %v",err)
 }
 if d.calls!=1{t.Fatal("failed TCP dial was retried")}
 var receipt instrumentprobe.LocalProbeReceipt
 if err:=readCanonical(c.ReceiptOutput,&receipt,true);err!=nil{t.Fatal(err)}
 if receipt.Outcome!="not_connected"{t.Fatalf("failed TCP produced false success: %v",receipt.Outcome)}
 if err:=instrumentprobe.ClaimLocalProbeOnce(c.InstrumentPrivateKeyFile,ch);err==nil{
  t.Fatal("failed network operation reopened nonce claim")
 }
}
func TestNodeLocalProbeCancelledBeforeEffects(t *testing.T){
 c,ch,_:=nodeProbeFixture(t)
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 d:=&recordingNodeDialer{}
 if err:=ExecuteExplicitNodeProbe(ctx,c,d,time.Now);!errors.Is(err,context.Canceled)||d.calls!=0{
  t.Fatalf("cancelled operation ran socket: %v %+v",err,d)
 }
 if err:=instrumentprobe.ClaimLocalProbeOnce(c.InstrumentPrivateKeyFile,ch);err!=nil{
  t.Fatalf("preflight cancellation spent nonce: %v",err)
 }
}
