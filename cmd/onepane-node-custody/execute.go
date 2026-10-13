package main

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/x509"
 "crypto/sha256"
 "encoding/hex"
 "encoding/base64"
 "encoding/json"
 "encoding/pem"
 "errors"
 "flag"
 "fmt"
 "io"
 "net"
 "os"
 "path/filepath"
 "runtime"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// ExplicitNodeProbeCommand describes one manually invoked, scope-bound,
// private IPv4 TCP socket. An authenticated operator must independently
// supply the original server-issued challenge/plan and both enrolled pins.
type ExplicitNodeProbeCommand struct {
 NodeDataDir, NodeID, NodeFingerprint, OperatorPublicKey string
 ServerSealFile, ServerCertificateFile, ServerNodeID, ServerFingerprint string
 InstrumentPrivateKeyFile, InstrumentPublicKey string
 ChallengeFile, PlanFile, ExecutionPermitFile, ReceiptOutput, WitnessOutput string
}

// This checks the scope of a SEPARATELY operator-authenticated OnePane
// permit. The permit is not server-signed and a file supplied by an
// untrusted Node cannot prove human approval. It must arrive over an
// independent authenticated operator channel before any network effect.
// Offline revocation cannot be observed after export; expiry <=2 minutes
// and the live OnePane nonce ledger remain additional separate gates.
func validateNodeProbeExecutionPermit(p projectworkspace.GoalNodeProbeExecutionPermit,
 nodeID,nodePin string,pub ed25519.PublicKey,
 ch instrumentprobe.Challenge,plan instrumentprobe.ProbePlan,
 now time.Time)error{
 hash:=func(raw []byte)string{
  digest:=sha256.Sum256(raw)
  return "sha256:"+hex.EncodeToString(digest[:])
 }
 ts:=now.UnixMilli()
 var originalPlan struct{PlanHash string `json:"plan_hash"`}
 if err:=json.Unmarshal(ch.SigningPayload,&originalPlan);err!=nil{return err}
 if p.ID==""||p.ProjectWorkspaceID==""||p.ApprovedBy==""||
  p.PlanGeneration<1||p.PlanSHA256==""||
  p.PlanSHA256!=originalPlan.PlanHash||
  p.Status!="approved"||p.Revision!=1||
  (p.EffectiveExecutionState!=""&&p.EffectiveExecutionState!="active")||
  p.RevokedAt!=0||
  p.PeerNodeID!=nodeID||p.PeerTLSFingerprint!=nodePin||
  p.ProjectID!=ch.ProjectID||p.GoalID!=ch.GoalID||
  p.InstrumentKeyID!=ch.KeyID||p.ChallengeID!=ch.ID||
  p.SourceID!=plan.SourceID||
  p.DeclaredTargetID!=plan.DeclaredTargetID||
  p.TLSPinID!=plan.TLSPinID||
  p.OriginalChallengeSHA256!=hash(ch.SigningPayload)||
  p.InstrumentPublicKeySHA256!=hash(pub)||
  p.OperatorPublicKeySHA256==""||
  p.ApprovedAt<ch.IssuedAt||p.ApprovedAt>ts||
  p.ExpiresAt!=ch.ExpiresAt||p.ExpiresAt<=ts{
  return errors.New("separately authorised Node execution permit does not match exact live challenge/Node")
 }
 return nil
}

// reservePrivateOutput creates a durable exclusive receipt slot BEFORE
// opening any network connection or spending the one-shot nonce. Failed
// execution intentionally leaves both slots and claim in place.
func reservePrivateOutput(path string)(*os.File,error){
 if path==""{return nil,errors.New("explicit private output required")}
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return nil,err}
 if err=syncExecutionDir(filepath.Dir(path));err!=nil{
  _=f.Close();return nil,err
 }
 return f,nil
}
func syncExecutionDir(path string)error{
 if runtime.GOOS=="windows"{return nil}
 dir,err:=os.Open(path)
 if err!=nil{return err}
 defer dir.Close()
 return dir.Sync()
}
func saveReservedJSON(out *os.File,record any)error{
 if out==nil{return errors.New("private output not reserved")}
 content,err:=json.Marshal(record)
 if err!=nil{return err}
 if _,err=out.Write(append(content,'\n'));err!=nil{return err}
 return out.Sync()
}

// ExecuteExplicitNodeProbe is invoked only by a human-facing CLI command.
// It shares the instrument CLI's durable per-key challenge-claim ledger.
// Validate all inputs and prepare both exclusive output slots before
// consuming the nonce. The probe calls instrumentprobe.ProbeApprovedPlan
// which opens at most one private IPv4 TCP4 connect and writes no data.
// A Node software key countersigns only AFTER that local function returns.
// This cannot attest the physical host, honest binary or physical target.
func ExecuteExplicitNodeProbe(ctx context.Context,c ExplicitNodeProbeCommand,
 dialer instrumentprobe.Dialer,now func()time.Time)error{
 if now==nil{now=time.Now}
 if ctx==nil||dialer==nil||
  c.NodeDataDir==""||c.NodeID==""||
  c.NodeFingerprint==""||c.OperatorPublicKey==""||c.InstrumentPrivateKeyFile==""||
  c.ServerSealFile==""||c.ServerCertificateFile==""||
  c.ServerNodeID==""||c.ServerFingerprint==""||
  c.InstrumentPublicKey==""||c.ChallengeFile==""||c.PlanFile==""||
  c.ExecutionPermitFile==""||
  c.ReceiptOutput==""||c.WitnessOutput==""||
  c.ReceiptOutput==c.WitnessOutput{
  return errors.New("explicit Node, enrolled keys, approved challenge/plan and two distinct private outputs required")
 }
 if err:=ctx.Err();err!=nil{return err}
 node,err:=loadExistingNodeIdentity(c.NodeDataDir,c.NodeID)
 if err!=nil{return err}
 if node.Fingerprint!=c.NodeFingerprint{
  return errors.New("current Node key does not match independently enrolled fingerprint")
 }
 if err=nodefederation.ValidateLocalInstrumentCustodyIdentity(
  c.NodeID,node,now());err!=nil{return err}
 instrumentPub,err:=parseInstrumentKey(c.InstrumentPublicKey)
 if err!=nil{return err}
 raw,err:=readBounded(c.InstrumentPrivateKeyFile,256,true)
 if err!=nil{return err}
 keyBytes,err:=base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
 if err!=nil||len(keyBytes)!=ed25519.PrivateKeySize{
  return errors.New("private instrument key is not a base64 Ed25519 key")
 }
 instrumentKey:=ed25519.PrivateKey(keyBytes)
 test:=[]byte("OnePane/probe-key-integrity/v1")
 if !ed25519.Verify(instrumentPub,test,ed25519.Sign(instrumentKey,test))||
  !bytes.Equal(instrumentKey.Public().(ed25519.PublicKey),instrumentPub){
  return errors.New("private instrument key does not match separately enrolled public key")
 }
 var ch instrumentprobe.Challenge
 var plan instrumentprobe.ProbePlan
 if err=readCanonical(c.ChallengeFile,&ch,true);err!=nil{return err}
 if err=readCanonical(c.PlanFile,&plan,true);err!=nil{return err}
 if _,err=instrumentprobe.ValidateProbePlan(ch,plan,now());err!=nil{return err}
 var signed SignedNodeExecutionPermit
 if err=readCanonical(c.ExecutionPermitFile,&signed,true);err!=nil{return err}
 operatorPublic,err:=parseInstrumentKey(c.OperatorPublicKey)
 if err!=nil{return err}
 if bytes.Equal(operatorPublic,instrumentPub){
  return errors.New("human operator must hold a separate key from instrument")
 }
 nodeLeaf,err:=x509.ParseCertificate(node.Certificate.Certificate[0])
 if err!=nil{return err}
 if nodePub,ok:=nodeLeaf.PublicKey.(ed25519.PublicKey);ok&&
  bytes.Equal(operatorPublic,nodePub){
  return errors.New("human operator must hold a separate key from Node")
 }
 if err=verifySignedNodeExecutionPermit(signed,operatorPublic);err!=nil{return err}
 if err=validateNodeProbeExecutionPermit(signed.Permit,c.NodeID,
  node.Fingerprint,instrumentPub,ch,plan,now());err!=nil{return err}
 var serverSeal nodefederation.SealedNodeProbeAuthority
 if err=readCanonical(c.ServerSealFile,&serverSeal,true);err!=nil{return err}
 serverPEM,err:=readBounded(c.ServerCertificateFile,65536,false)
 if err!=nil{return err}
 block,rest:=pem.Decode(serverPEM)
 if block==nil||block.Type!="CERTIFICATE"||len(bytes.TrimSpace(rest))!=0{
  return errors.New("one independent original OnePane server certificate required")
 }
 originalPermit,err:=nodefederation.VerifySealedNodeProbeExecution(
  serverSeal,block.Bytes,c.ServerNodeID,c.ServerFingerprint)
 if err!=nil{return err}
 if originalPermit!=signed.Permit{
  return errors.New("human signature and original OnePane server seal concern different approvals")
 }
 // Prevent writes to existing plans, instrument keys or evidence, even
 // with aliased filenames. O_EXCL also refuses an existing symlink.
 receiptOut,err:=reservePrivateOutput(c.ReceiptOutput)
 if err!=nil{return err}
 defer receiptOut.Close()
 witnessOut,err:=reservePrivateOutput(c.WitnessOutput)
 if err!=nil{return err}
 defer witnessOut.Close()
 if err=instrumentprobe.ClaimLocalProbeOnce(c.InstrumentPrivateKeyFile,ch);err!=nil{
  return err
 }
 receipt,err:=instrumentprobe.ProbeApprovedPlan(ctx,ch,plan,
  instrumentKey,dialer,now)
 if err!=nil{return err}
 if err=saveReservedJSON(receiptOut,receipt);err!=nil{return err}
 witness,err:=nodefederation.SignLocalInstrumentCustody(c.NodeID,node,
  receipt,ch,plan,instrumentPub,now())
 if err!=nil{return err}
 if err=saveReservedJSON(witnessOut,witness);err!=nil{return err}
 return nil
}

func runProbeAndWitness(args []string)error{
 fs:=flag.NewFlagSet("probe-and-witness",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 dataDir:=fs.String("data-dir","","existing Node data directory (no provisioning)")
 nodeID:=fs.String("node-id","","existing independently enrolled Node ID")
 nodePin:=fs.String("node-fingerprint","","independently enrolled pinned Node TLS SHA256 fingerprint")
 operatorPub:=fs.String("operator-public-key","","independently enrolled human operator Ed25519 base64 public key")
 serverSeal:=fs.String("server-seal","","owner-only original OnePane server signed permit")
 serverCert:=fs.String("server-certificate","","independently retained original OnePane certificate PEM")
 serverID:=fs.String("server-node-id","","independently enrolled original OnePane server Node ID")
 serverPin:=fs.String("server-fingerprint","","independently enrolled original OnePane certificate SHA256 pin")
 privateKey:=fs.String("instrument-private-key","","owner-only 64-byte base64 Ed25519 instrument key file")
 instrumentPin:=fs.String("instrument-public-key","","independently enrolled instrument Ed25519 base64 public key")
 challenge:=fs.String("challenge","","original owner-only OnePane-issued challenge")
 plan:=fs.String("plan","","original owner-only human-approved exact private endpoint plan")
 permit:=fs.String("execution-permit","","owner-only, separately authenticated human Node execution permit")
 receipt:=fs.String("receipt-output","","new owner-only signed instrument receipt")
 witness:=fs.String("witness-output","","new owner-only paired-Node custody witness")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected positional arguments")}
 err:=ExecuteExplicitNodeProbe(context.Background(),ExplicitNodeProbeCommand{
  NodeDataDir:*dataDir,NodeID:*nodeID,NodeFingerprint:*nodePin,
  OperatorPublicKey:*operatorPub,
  ServerSealFile:*serverSeal,ServerCertificateFile:*serverCert,
  ServerNodeID:*serverID,ServerFingerprint:*serverPin,
  InstrumentPrivateKeyFile:*privateKey,InstrumentPublicKey:*instrumentPin,
  ChallengeFile:*challenge,PlanFile:*plan,ExecutionPermitFile:*permit,
  ReceiptOutput:*receipt,
  WitnessOutput:*witness,
 },&net.Dialer{},time.Now)
 if err!=nil{return err}
 fmt.Println("One explicitly approved local TCP4 connect attempted; independent instrument and Node signatures saved.")
 fmt.Println("Node execution is software-mediated, NOT hardware/physical vantage attestation or device-health verification.")
 return nil
}
