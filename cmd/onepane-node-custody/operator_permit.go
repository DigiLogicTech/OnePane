package main

import (
 "bytes"
 "crypto/ed25519"
 "encoding/base64"
 "encoding/json"
 "errors"
 "flag"
 "io"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

const operatorPermitDomain="OnePane/operator-node-execution-permit/ed25519/v1"

type SignedNodeExecutionPermit struct{
 Permit projectworkspace.GoalNodeProbeExecutionPermit `json:"permit"`
 OperatorSignature []byte `json:"operator_signature"`
}

func operatorPermitMessage(p projectworkspace.GoalNodeProbeExecutionPermit)([]byte,error){
 raw,err:=json.Marshal(p)
 if err!=nil{return nil,err}
 if len(raw)>4096{return nil,errors.New("operator permit exceeds scoped size")}
 b:=make([]byte,0,len(operatorPermitDomain)+1+len(raw))
 b=append(b,[]byte(operatorPermitDomain)...)
 b=append(b,0)
 b=append(b,raw...)
 return b,nil
}
func verifySignedNodeExecutionPermit(s SignedNodeExecutionPermit,
 operatorPublic ed25519.PublicKey)error{
 if len(operatorPublic)!=ed25519.PublicKeySize||
  len(s.OperatorSignature)!=ed25519.SignatureSize{
  return errors.New("separately enrolled human operator signature is required")
 }
 payload,err:=operatorPermitMessage(s.Permit)
 if err!=nil{return err}
 if !ed25519.Verify(operatorPublic,payload,s.OperatorSignature){
  return errors.New("human operator execution signature mismatch")
 }
 return nil
}

// sign-execution-permit is run explicitly by the human operator, not by a
// Node or background process. Sign ONLY the independently retrieved, live
// OnePane human-granted permit. The operator's signing key MUST be separately
// enrolled and kept private from both Node and instrument hosts.
func runSignNodeExecutionPermit(args []string)error{
 fs:=flag.NewFlagSet("sign-execution-permit",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 permitFile:=fs.String("permit","","original canonical owner-only OnePane human permit JSON")
 operatorKeyFile:=fs.String("operator-private-key","","separate owner-only operator Ed25519 private key file")
 operatorPrincipal:=fs.String("operator-principal-id","","operator identity matching original human approver")
 output:=fs.String("output","","new owner-only signed permit transfer file")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*permitFile==""||*operatorKeyFile==""||
  *operatorPrincipal==""||*output==""{
  return errors.New("sign-execution-permit requires --permit, --operator-private-key, --operator-principal-id and --output")
 }
 var p projectworkspace.GoalNodeProbeExecutionPermit
 if err:=readCanonical(*permitFile,&p,true);err!=nil{return err}
 now:=time.Now().UnixMilli()
 if p.ID==""||p.ApprovedBy!=*operatorPrincipal||
  p.Status!="approved"||p.Revision!=1||
  (p.EffectiveExecutionState!=""&&p.EffectiveExecutionState!="active")||
  p.ApprovedAt>now||p.ExpiresAt<=now||
  p.OriginalChallengeSHA256==""||p.InstrumentPublicKeySHA256==""{
  return errors.New("operator permit is not an approved live original human record")
 }
 raw,err:=readBounded(*operatorKeyFile,256,true)
 if err!=nil{return err}
 decoded,err:=base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
 if err!=nil||len(decoded)!=ed25519.PrivateKeySize{
  return errors.New("expected protected 64-byte Ed25519 operator signing key")
 }
 key:=ed25519.PrivateKey(decoded)
 pub:=key.Public().(ed25519.PublicKey)
 payload,err:=operatorPermitMessage(p)
 if err!=nil{return err}
 result:=SignedNodeExecutionPermit{
  Permit:p,OperatorSignature:ed25519.Sign(key,payload),
 }
 if err=verifySignedNodeExecutionPermit(result,pub);err!=nil{return err}
 return exclusivePrivateJSON(*output,result)
}
