package main

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

func TestHistoricalHumanNodeInstrumentChainRequiresThreeSeparateKeys(t *testing.T){
 c,ch,plan:=nodeProbeFixture(t)
 d:=&recordingNodeDialer{}
 if err:=ExecuteExplicitNodeProbe(context.Background(),c,d,time.Now);
  err!=nil{t.Fatalf("original human-approved one-shot execution failed: %v",err)}
 var receipt instrumentprobe.LocalProbeReceipt
 var witness nodefederation.NodeInstrumentCustodyWitness
 var approval SignedNodeExecutionPermit
 if err:=readCanonical(c.ReceiptOutput,&receipt,true);err!=nil{t.Fatal(err)}
 if err:=readCanonical(c.WitnessOutput,&witness,true);err!=nil{t.Fatal(err)}
 if err:=readCanonical(c.ExecutionPermitFile,&approval,true);err!=nil{t.Fatal(err)}
 node,err:=loadExistingNodeIdentity(c.NodeDataDir,c.NodeID);if err!=nil{t.Fatal(err)}
 instPub,err:=parseInstrumentKey(c.InstrumentPublicKey);if err!=nil{t.Fatal(err)}
 humanPub,err:=parseInstrumentKey(c.OperatorPublicKey);if err!=nil{t.Fatal(err)}
 cert:=node.Certificate.Certificate[0]
 verify:=func(w nodefederation.NodeInstrumentCustodyWitness,
  a SignedNodeExecutionPermit,nodeDER []byte,operator ed25519.PublicKey)error{
  return VerifyApprovedNodeExecutionEvidence(w,a,nodeDER,c.NodeFingerprint,
   operator,instPub,receipt,ch,plan)
 }
 if err=verify(witness,approval,cert,humanPub);err!=nil{
  t.Fatalf("original three independent software-key chain rejected: %v",err)
 }
 certPath:=filepath.Join(c.NodeDataDir,"keys","federation.crt")
 args:=[]string{
  "--witness",c.WitnessOutput,"--receipt",c.ReceiptOutput,
  "--challenge",c.ChallengeFile,"--plan",c.PlanFile,
  "--execution-permit",c.ExecutionPermitFile,
  "--node-certificate",certPath,"--node-fingerprint",c.NodeFingerprint,
  "--instrument-public-key",c.InstrumentPublicKey,
  "--operator-public-key",c.OperatorPublicKey,
  "--server-seal",c.ServerSealFile,
  "--server-certificate",c.ServerCertificateFile,
  "--server-node-id",c.ServerNodeID,
  "--server-fingerprint",c.ServerFingerprint,
 }
 if err=runVerifyExecution(args);err!=nil{
  t.Fatalf("offline operator verification command failed: %v",err)
 }
 // A historical chain signed by the paired Node AGAIN in the role of
 // OnePane issuer can have valid signatures but only three unique keys.
 // The offline verifier must reject that misleading four-role claim.
 clonedIssuerSeal,err:=nodefederation.SealHumanNodeProbeExecution(node,approval.Permit,time.Now())
 if err!=nil{t.Fatal(err)}
 clonedSealFile:=writeCanonicalTest(t,t.TempDir(),"cloned-historical-issuer.json",clonedIssuerSeal)
 clonedArgs:=append([]string(nil),args...)
 for i:=0;i+1<len(clonedArgs);i++{
  switch clonedArgs[i]{
  case "--server-seal":clonedArgs[i+1]=clonedSealFile
  case "--server-certificate":clonedArgs[i+1]=certPath
  case "--server-node-id":clonedArgs[i+1]=c.NodeID
  case "--server-fingerprint":clonedArgs[i+1]=c.NodeFingerprint
  }
 }
 if err=runVerifyExecution(clonedArgs);err==nil{
  t.Fatal("historical verification misreported cloned server/Node key as independent")
 }
 // A server seal generated AFTER the instrument had already observed
 // the endpoint must not retroactively authorise that earlier receipt,
 // even though its server signature, scope and operator signatures match.
 serverDataDir:=filepath.Dir(filepath.Dir(c.ServerCertificateFile))
 issuer,err:=nodefederation.EnsureIdentity(serverDataDir,c.ServerNodeID)
 if err!=nil{t.Fatal(err)}
 lateAt:=time.UnixMilli(receipt.ObservedAt+1000)
 if lateAt.UnixMilli()>=approval.Permit.ExpiresAt{
  t.Fatal("fixture is too close to original challenge expiration")
 }
 postdatedSeal,err:=nodefederation.SealHumanNodeProbeExecution(
  issuer,approval.Permit,lateAt)
 if err!=nil{t.Fatalf("test needs a valid but postdated signature: %v",err)}
 postdatedFile:=writeCanonicalTest(t,t.TempDir(),"postdated-server-seal.json",postdatedSeal)
 lateArgs:=append([]string(nil),args...)
 for i:=0;i+1<len(lateArgs);i++{
  if lateArgs[i]=="--server-seal"{lateArgs[i+1]=postdatedFile}
 }
 if err=runVerifyExecution(lateArgs);err==nil{
  t.Fatal("historical receipt was retroactively authorised with a newer issuer seal")
 }
 wrongPub,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if err=verify(witness,approval,cert,wrongPub);err==nil{
  t.Fatal("unregistered operator key accepted original human consent")
 }
 if err=verify(witness,approval,cert,instPub);err==nil{
  t.Fatal("instrument signer was misrepresented as independent human")
 }
 bad:=approval
 bad.Permit.ChallengeID="different"
 if err=verify(witness,bad,cert,humanPub);err==nil{
  t.Fatal("operator execution scope changed after original signature")
 }
 bad=approval
 bad.OperatorSignature=append([]byte(nil),approval.OperatorSignature...)
 bad.OperatorSignature[0]^=1
 if err=verify(witness,bad,cert,humanPub);err==nil{
  t.Fatal("altered human signature passed independent evidence review")
 }
 otherNode,err:=nodefederation.EnsureIdentity(t.TempDir(),"other")
 if err!=nil{t.Fatal(err)}
 if err=verify(witness,approval,otherNode.Certificate.Certificate[0],humanPub);err==nil{
  t.Fatal("unrelated Node signed somebody else's probe")
 }
 changed:=witness
 changed.Signature=append([]byte(nil),witness.Signature...)
 changed.Signature[0]^=1
 if err=verify(changed,approval,cert,humanPub);err==nil{
  t.Fatal("mutated Node custody signature was admitted")
 }
 if err=runVerifyExecution([]string{"--receipt",c.ReceiptOutput});err==nil{
  t.Fatal("historical evidence review silently skipped human authority files")
 }
 _ = base64.StdEncoding
}
