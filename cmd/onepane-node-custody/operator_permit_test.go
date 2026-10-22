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
 // A fully signed detached server file can still contain duplicated role
 // keys. The human signing client must refuse to export those as a second
 // independent approval, before creating any signed output file.
 serverPrivate:=serverIdentity.Certificate.PrivateKey.(ed25519.PrivateKey)
 serverPublic:=serverPrivate.Public().(ed25519.PublicKey)
 nodeIdentity,err:=loadExistingNodeIdentity(c.NodeDataDir,c.NodeID)
 if err!=nil{t.Fatal(err)}
 duplicateServerOperatorKeyFile:=filepath.Join(dir,"issuer-reused-as-operator.key")
 if err=os.WriteFile(duplicateServerOperatorKeyFile,
  []byte(base64.StdEncoding.EncodeToString(serverPrivate)),0600);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{
  name string
  issuer nodefederation.Identity
  permit projectworkspace.GoalNodeProbeExecutionPermit
  operatorKeyFile string
  certPath,id,pin string
 }{
  {name:"issuer_is_operator",issuer:serverIdentity,
   permit:func()projectworkspace.GoalNodeProbeExecutionPermit{
    p:=signed.Permit;p.OperatorPublicKeySHA256=operatorKeyFingerprint(serverPublic);return p}(),
   operatorKeyFile:duplicateServerOperatorKeyFile,
   certPath:c.ServerCertificateFile,id:c.ServerNodeID,pin:c.ServerFingerprint},
  {name:"issuer_is_instrument",issuer:serverIdentity,
   permit:func()projectworkspace.GoalNodeProbeExecutionPermit{
    p:=signed.Permit;p.InstrumentPublicKeySHA256=operatorKeyFingerprint(serverPublic);return p}(),
   operatorKeyFile:private,
   certPath:c.ServerCertificateFile,id:c.ServerNodeID,pin:c.ServerFingerprint},
  {name:"issuer_is_peer_node",issuer:nodeIdentity,permit:signed.Permit,
   operatorKeyFile:private,
   certPath:filepath.Join(c.NodeDataDir,"keys","federation.crt"),
   id:c.NodeID,pin:c.NodeFingerprint},
 }{
  t.Run(tc.name,func(t *testing.T){
   forged:=forgeClonedIssuerSeal(t,tc.issuer,tc.permit,time.Now())
   forgedPath:=writeCanonicalTest(t,dir,tc.name+".json",forged)
   clonedArgs:=append([]string(nil),args...)
   target:=filepath.Join(dir,tc.name+"-should-not-exist.json")
   for i:=0;i+1<len(clonedArgs);i++{
    switch clonedArgs[i]{
    case "--server-seal":clonedArgs[i+1]=forgedPath
    case "--server-certificate":clonedArgs[i+1]=tc.certPath
    case "--server-node-id":clonedArgs[i+1]=tc.id
    case "--server-fingerprint":clonedArgs[i+1]=tc.pin
    case "--operator-private-key":clonedArgs[i+1]=tc.operatorKeyFile
    case "--output":clonedArgs[i+1]=target
    }
   }
   if err:=runSignNodeExecutionPermit(clonedArgs);err==nil{
    t.Fatal("operator client exported duplicate role as independent custody")
   }
   if _,err:=os.Stat(target);!os.IsNotExist(err){
    t.Fatalf("invalid role grant left signed output: %v",err)
   }
  })
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
