package main

import (
 "bytes"
 "crypto/ed25519"
 "crypto/x509"
 "encoding/json"
 "encoding/pem"
 "errors"
 "flag"
 "fmt"
 "io"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

// VerifyApprovedNodeExecutionEvidence historically validates THREE separate
// key signatures/identities and their common original human-only scope.
// A signed operator permit is not a server signature, and a software Node
// signature is NOT physical hardware/vantage attestation or sensor truth.
func VerifyApprovedNodeExecutionEvidence(
 witness nodefederation.NodeInstrumentCustodyWitness,
 approval SignedNodeExecutionPermit,
 nodeCertificateDER []byte,nodePin string,
 humanPublic,instrumentPublic ed25519.PublicKey,
 receipt instrumentprobe.LocalProbeReceipt,
 challenge instrumentprobe.Challenge,plan instrumentprobe.ProbePlan)error{
 if err:=nodefederation.VerifyLocalInstrumentCustody(witness,
  nodeCertificateDER,nodePin,instrumentPublic,receipt,challenge,plan);
  err!=nil{return err}
 if err:=verifySignedNodeExecutionPermit(approval,humanPublic);err!=nil{return err}
 leaf,err:=x509.ParseCertificate(nodeCertificateDER)
 if err!=nil{return err}
 nodePublic,ok:=leaf.PublicKey.(ed25519.PublicKey)
 if !ok||bytes.Equal(humanPublic,nodePublic)||
  bytes.Equal(humanPublic,instrumentPublic){
  return errors.New("operator signing identity must be distinct from Node and instrument")
 }
 var envelope struct{
  WitnessedAt int64 `json:"witnessed_at"`
 }
 if err=json.Unmarshal(witness.SigningPayload,&envelope);err!=nil{return err}
 if envelope.WitnessedAt<1{
  return errors.New("Node witness timestamp not found")
 }
 // Verify as-of original historical witness time. This does not turn
 // expired or revoked authority into a new permission to execute.
 return validateNodeProbeExecutionPermit(approval.Permit,
  leaf.Subject.CommonName,nodePin,instrumentPublic,
  challenge,plan,time.UnixMilli(envelope.WitnessedAt))
}

func runVerifyExecution(args []string)error{
 fs:=flag.NewFlagSet("verify-execution",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 witnessFile:=fs.String("witness","","private original Node-signed witness file")
 receiptFile:=fs.String("receipt","","private original instrument-signed report file")
 challengeFile:=fs.String("challenge","","original authenticated OnePane challenge file")
 planFile:=fs.String("plan","","original authenticated private TCP4 plan file")
 permitFile:=fs.String("execution-permit","","operator-signed original human permit")
 certFile:=fs.String("node-certificate","","independently retained enrolled Node PEM certificate")
 nodeFingerprint:=fs.String("node-fingerprint","","independently enrolled Node SHA-256 pin")
 instrumentKey:=fs.String("instrument-public-key","","independently enrolled instrument Ed25519 key")
 humanKey:=fs.String("operator-public-key","","independently enrolled human operator Ed25519 key")
 serverSealFile:=fs.String("server-seal","","original private OnePane-signed execution approval")
 serverCertFile:=fs.String("server-certificate","","separately trusted original OnePane server PEM")
 serverID:=fs.String("server-node-id","","independently enrolled OnePane server Node ID")
 serverFingerprint:=fs.String("server-fingerprint","","independently enrolled OnePane server certificate SHA256 pin")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*witnessFile==""||*receiptFile==""||
  *challengeFile==""||*planFile==""||*permitFile==""||
  *certFile==""||*nodeFingerprint==""||*instrumentKey==""||*humanKey==""||
  *serverSealFile==""||*serverCertFile==""||*serverID==""||*serverFingerprint==""{
  return errors.New("verify-execution requires original receipt/witness/challenge/plan, signed execution permit, independent Node certificate/pin, and separately enrolled instrument/operator keys")
 }
 instrumentPublic,err:=parseInstrumentKey(*instrumentKey)
 if err!=nil{return err}
 humanPublic,err:=parseInstrumentKey(*humanKey)
 if err!=nil{return err}
 receipt,ch,plan,err:=readProofInputs(*receiptFile,*challengeFile,*planFile)
 if err!=nil{return err}
 var witness nodefederation.NodeInstrumentCustodyWitness
 if err=readCanonical(*witnessFile,&witness,true);err!=nil{return err}
 var approval SignedNodeExecutionPermit
 if err=readCanonical(*permitFile,&approval,true);err!=nil{return err}
 raw,err:=readBounded(*certFile,65536,false)
 if err!=nil{return err}
 block,remaining:=pem.Decode(raw)
 if block==nil||block.Type!="CERTIFICATE"||len(bytes.TrimSpace(remaining))!=0{
  return errors.New("one independently pinned Node certificate required")
 }
 if err=VerifyApprovedNodeExecutionEvidence(witness,approval,block.Bytes,
  *nodeFingerprint,humanPublic,instrumentPublic,receipt,ch,plan);err!=nil{return err}
 var originalSeal nodefederation.SealedNodeProbeAuthority
 if err=readCanonical(*serverSealFile,&originalSeal,true);err!=nil{return err}
 pinnedPEM,err:=readBounded(*serverCertFile,65536,false)
 if err!=nil{return err}
 serverBlock,serverRest:=pem.Decode(pinnedPEM)
 if serverBlock==nil||serverBlock.Type!="CERTIFICATE"||
  len(bytes.TrimSpace(serverRest))!=0{
  return errors.New("original independently pinned OnePane server certificate required")
 }
 sealedPermit,err:=nodefederation.VerifySealedNodeProbeExecutionAtObservation(
  originalSeal,serverBlock.Bytes,*serverID,*serverFingerprint,receipt.ObservedAt)
 if err!=nil{return err}
 if !sameSealedHumanProbeScope(sealedPermit,approval.Permit){
  return errors.New("human and OnePane server signatures refer to different approvals")
 }
 if err=requireDistinctCustodyRoles(serverBlock.Bytes,block.Bytes,
  humanPublic,instrumentPublic);err!=nil{return err}
 fmt.Println("Original OnePane, human operator, Node and instrument signatures bind one historical approved challenge.")
 fmt.Println("This verifies four software keys and historical metadata, NOT live revocation, physical execution, independent vantage or device health.")
 return nil
}
