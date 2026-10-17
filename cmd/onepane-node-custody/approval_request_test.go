package main

import (
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "os"
 "path/filepath"
 "runtime"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestHumanNodeOperatorProofIsNotTransferableToOtherScopes(t *testing.T){
 public,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 c:=projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand{
  ProjectID:"p",GoalID:"g",InstrumentKeyID:"key",
  ChallengeID:"nonce",PeerNodeID:"peer",ActorPrincipalID:"human",
  ExpectedGoalRevision:7,OperatorPublicKey:public,
 }
 body,err:=makeHumanNodeApprovalRequest(c,private)
 if err!=nil{t.Fatal(err)}
 original,err:=projectworkspace.NodeOperatorExecutionApprovalSigningBytes(c)
 if err!=nil||!ed25519.Verify(public,original,body.OperatorProofSignature){
  t.Fatalf("human proof not valid for exact request: %v",err)
 }
 for label,mutate:=range map[string]func(*projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){
  "node":func(x *projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){x.PeerNodeID="other"},
  "actor":func(x *projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){x.ActorPrincipalID="robot"},
  "goal":func(x *projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){x.GoalID="other"},
  "challenge":func(x *projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){x.ChallengeID="other"},
  "revision":func(x *projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand){x.ExpectedGoalRevision++},
 }{
  t.Run(label,func(t *testing.T){
   alt:=c;mutate(&alt)
   raw,err:=projectworkspace.NodeOperatorExecutionApprovalSigningBytes(alt)
   if err!=nil{t.Fatal(err)}
   if ed25519.Verify(public,raw,body.OperatorProofSignature){
    t.Fatal("one human signature authorised a different Node effect")
   }
  })
 }
 _,other,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if _,err=makeHumanNodeApprovalRequest(c,other);err==nil{
  t.Fatal("operator private/public signer mismatch accepted")
 }
}

func TestHumanNodeApprovalCLIPrivateKeyAndExclusiveOriginal(t *testing.T){
 _,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 keyPath:=filepath.Join(dir,"human.key")
 if err=os.WriteFile(keyPath,[]byte(base64.StdEncoding.EncodeToString(private)),0600);
  err!=nil{t.Fatal(err)}
 out:=filepath.Join(dir,"request.json")
 args:=[]string{"--project-id","p","--goal-id","g",
  "--instrument-key-id","key","--challenge-id","nonce",
  "--peer-node-id","peer","--operator-principal-id","human",
  "--goal-revision","7","--operator-private-key",keyPath,"--output",out}
 if err:=runPrepareHumanApproval(args);err!=nil{t.Fatal(err)}
 var body HumanNodeApprovalRequest
 if err:=readCanonical(out,&body,true);err!=nil{t.Fatal(err)}
 scope:=projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand{
  ProjectID:"p",GoalID:"g",InstrumentKeyID:"key",
  ChallengeID:"nonce",PeerNodeID:"peer",ActorPrincipalID:"human",
  ExpectedGoalRevision:7,OperatorPublicKey:body.OperatorPublicKey,
 }
 raw,err:=projectworkspace.NodeOperatorExecutionApprovalSigningBytes(scope)
 if err!=nil||!ed25519.Verify(body.OperatorPublicKey,raw,body.OperatorProofSignature){
  t.Fatalf("offline operator request does not prove original private key: %v",err)
 }
 if err:=runPrepareHumanApproval(args);!os.IsExist(err){
  t.Fatalf("repeat wrote over previously signed human request: %v",err)
 }
 if runtime.GOOS!="windows"{
  if err=os.Chmod(keyPath,0644);err!=nil{t.Fatal(err)}
  args[len(args)-1]=filepath.Join(dir,"other.json")
  if err=runPrepareHumanApproval(args);err==nil{
   t.Fatal("group-readable private human operator key was accepted")
  }
 }
}
