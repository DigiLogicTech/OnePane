package main

import (
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "encoding/json"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
 "os"
 "path/filepath"
 "runtime"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestHumanCanSeparatelySignAuthenticatedNodeExecutionPermitOffline(t *testing.T){
 c,_,_:=nodeProbeFixture(t)
 var signed SignedNodeExecutionPermit
 if err:=readCanonical(c.ExecutionPermitFile,&signed,true);err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 signed.Permit.OperatorPublicKeySHA256=operatorKeyFingerprint(pub)
 serverDataDir:=filepath.Dir(filepath.Dir(c.ServerCertificateFile))
 serverIdentity,err:=loadExistingNodeIdentity(serverDataDir,c.ServerNodeID)
 if err!=nil{t.Fatal(err)}
 validServerSeal,err:=nodefederation.SealHumanNodeProbeExecution(
  serverIdentity,signed.Permit,time.Now())
 if err!=nil{t.Fatalf("valid original human authorization not sealed: %v",err)}
 serverSealFile:=writeCanonicalTest(t,dir,"original-server-seal.json",validServerSeal)
 private:=filepath.Join(dir,"operator.key")
 if err=os.WriteFile(private,[]byte(base64.StdEncoding.EncodeToString(priv)),0600);err!=nil{
  t.Fatal(err)
 }
 out:=filepath.Join(dir,"operator-signed.json")
 args:=[]string{"--server-seal",serverSealFile,
  "--server-certificate",c.ServerCertificateFile,
  "--server-node-id",c.ServerNodeID,"--server-fingerprint",c.ServerFingerprint,
  "--operator-private-key",private,
  "--operator-principal-id",signed.Permit.ApprovedBy,"--output",out}
 if err=runSignNodeExecutionPermit(args);err!=nil{
  t.Fatalf("trusted human operator could not sign exact grant: %v",err)
 }
 futureSeal,err:=nodefederation.SealHumanNodeProbeExecution(
  serverIdentity,signed.Permit,time.Now().Add(3*time.Second))
 if err!=nil{t.Fatal(err)}
 futureFile:=writeCanonicalTest(t,dir,"postdated-server-seal.json",futureSeal)
 futureOut:=filepath.Join(dir,"untrusted-future-approval.json")
 futureArgs:=append([]string(nil),args...)
 for i:=0;i+1<len(futureArgs);i++{
  if futureArgs[i]=="--server-seal"{futureArgs[i+1]=futureFile}
  if futureArgs[i]=="--output"{futureArgs[i+1]=futureOut}
 }
 if err=runSignNodeExecutionPermit(futureArgs);err==nil{
  t.Fatal("human signer approved a server seal not yet issued")
 }
 if _,err=os.Stat(futureOut);!os.IsNotExist(err){
  t.Fatalf("future issuer seal created a signed permit: %v",err)
 }
 var result SignedNodeExecutionPermit
 if err=readCanonical(out,&result,true);err!=nil{t.Fatal(err)}
 if err=verifySignedNodeExecutionPermit(result,pub);err!=nil{
  t.Fatalf("operator signature denied by independently pinned key: %v",err)
 }
 if err=runSignNodeExecutionPermit(args);!os.IsExist(err){
  t.Fatalf("repeated human grant export overwrote existing original: %v",err)
 }
 other,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if err=verifySignedNodeExecutionPermit(result,other);err==nil{
  t.Fatal("different human signer key verified original permit")
 }
 changed:=result
 changed.Permit.PeerNodeID="another-Node"
 if err=verifySignedNodeExecutionPermit(changed,pub);err==nil{
  t.Fatal("signed Node authority permitted a forged destination")
 }
 changed=result
 changed.Permit.ExpiresAt+=300000
 if err=verifySignedNodeExecutionPermit(changed,pub);err==nil{
  t.Fatal("human signature failed to bind expiry")
 }
 if runtime.GOOS!="windows"{
  if err=os.Chmod(private,0644);err!=nil{t.Fatal(err)}
  replacement:=filepath.Join(dir,"not-signed.json")
  wrong:=[]string{"--server-seal",serverSealFile,
   "--server-certificate",c.ServerCertificateFile,
   "--server-node-id",c.ServerNodeID,"--server-fingerprint",c.ServerFingerprint,
   "--operator-private-key",private,
   "--operator-principal-id",signed.Permit.ApprovedBy,"--output",replacement}
  if err=runSignNodeExecutionPermit(wrong);err==nil{
   t.Fatal("shared operator private key signed live Node authority")
  }
 }
}

func TestHumanOperatorPermitSigningRefusesUnapprovedMetadata(t *testing.T){
 c,_,_:=nodeProbeFixture(t)
 var transfer SignedNodeExecutionPermit
 if err:=readCanonical(c.ExecutionPermitFile,&transfer,true);err!=nil{t.Fatal(err)}
 pub,key,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 transfer.Permit.OperatorPublicKeySHA256=operatorKeyFingerprint(pub)
 serverDataDir:=filepath.Dir(filepath.Dir(c.ServerCertificateFile))
 originalServer,err:=loadExistingNodeIdentity(serverDataDir,c.ServerNodeID)
 if err!=nil{t.Fatal(err)}
 originalSeal,err:=nodefederation.SealHumanNodeProbeExecution(originalServer,transfer.Permit,time.Now())
 if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 private:=filepath.Join(dir,"human.key")
 if err=os.WriteFile(private,[]byte(base64.StdEncoding.EncodeToString(key)),0600);err!=nil{
  t.Fatal(err)
 }
 for name,alter:=range map[string]func(*projectworkspace.GoalNodeProbeExecutionPermit){
  "revoked":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.Status="revoked"},
  "peer_unavailable":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.EffectiveExecutionState="peer_unavailable"},
  "old_plan_generation":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.PlanGeneration=0},
  "missing_plan_hash":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.PlanSHA256=""},
  "unbound_operator":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.OperatorPublicKeySHA256=""},
  "wrong_actor":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.ApprovedBy="other-human"},
  "future_approval":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.ApprovedAt=time.Now().Add(time.Minute).UnixMilli()},
  "expired":func(p *projectworkspace.GoalNodeProbeExecutionPermit){p.ExpiresAt=time.Now().Add(-time.Minute).UnixMilli()},
 }{
  t.Run(name,func(t *testing.T){
   p:=transfer.Permit
   alter(&p)
   var env struct{
    Domain string `json:"domain"`
    ServerNodeID string `json:"server_node_id"`
    ServerCertificateSHA256 string `json:"server_certificate_sha256"`
    SignedAt int64 `json:"signed_at"`
    Permit projectworkspace.GoalNodeProbeExecutionPermit `json:"permit"`
   }
   if err=json.Unmarshal(originalSeal.SigningPayload,&env);err!=nil{t.Fatal(err)}
   env.Permit=p
   changedPayload,err:=json.Marshal(env)
   if err!=nil{t.Fatal(err)}
   forged:=originalSeal;forged.SigningPayload=changedPayload
   file:=writeCanonicalTest(t,t.TempDir(),"tampered-server-seal.json",forged)
   out:=filepath.Join(t.TempDir(),"signed.json")
   err=runSignNodeExecutionPermit([]string{
    "--server-seal",file,"--server-certificate",c.ServerCertificateFile,
    "--server-node-id",c.ServerNodeID,"--server-fingerprint",c.ServerFingerprint,
    "--operator-private-key",private,"--operator-principal-id","human",
    "--output",out})
   if err==nil{t.Fatal("unapproved operator metadata obtained a signature")}
   if _,err=os.Stat(out);!os.IsNotExist(err){
    t.Fatal("failed human signing left an apparently approved signed file")
   }
  })
 }
 _ = instrumentprobe.Challenge{}
}
