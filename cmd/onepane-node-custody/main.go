// onepane-node-custody is an explicitly invoked offline evidence-custody
// utility. sign/verify open no network socket. The separately opted-in
// probe-and-witness operation performs at most one approved private TCP4
// dial. None create a Node identity, contact another Node or update health.
package main

import (
 "bytes"
 "crypto/ed25519"
 "crypto/tls"
 "crypto/x509"
 "encoding/base64"
 "encoding/json"
 "encoding/pem"
 "errors"
 "flag"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

// Read from a stable opened file descriptor after lstat. Plans, receipts,
// witness files and the existing Node private key require owner-only POSIX
// permissions. Windows NTFS ACL review is still an operator responsibility.
func readBounded(path string,limit int64,private bool)([]byte,error){
 if path==""{return nil,errors.New("file path required")}
 before,err:=os.Lstat(path)
 if err!=nil{return nil,err}
 if !before.Mode().IsRegular()||before.Size()>limit{
  return nil,errors.New("expected bounded ordinary non-symlink file")
 }
 if private&&runtime.GOOS!="windows"&&before.Mode().Perm()&0077!=0{
  return nil,errors.New("private Node custody files require owner-only mode")
 }
 f,err:=os.Open(path)
 if err!=nil{return nil,err}
 defer f.Close()
 stat,err:=f.Stat()
 if err!=nil{return nil,err}
 if !stat.Mode().IsRegular()||stat.Size()>limit||!os.SameFile(before,stat){
  return nil,errors.New("custody input changed identity before read")
 }
 if private&&runtime.GOOS!="windows"&&stat.Mode().Perm()&0077!=0{
  return nil,errors.New("private Node custody files require owner-only mode")
 }
 raw,err:=io.ReadAll(io.LimitReader(f,limit+1))
 if err!=nil{return nil,err}
 if int64(len(raw))>limit{return nil,errors.New("custody input too large")}
 return raw,nil
}

func readCanonical(path string,out any,private bool)error{
 raw,err:=readBounded(path,8192,private)
 if err!=nil{return err}
 if err=json.Unmarshal(raw,out);err!=nil{return err}
 canonical,err:=json.Marshal(out)
 if err!=nil{return err}
 if !bytes.Equal(bytes.TrimSpace(raw),canonical){
  return errors.New("custody input JSON must have canonical OnePane encoding")
 }
 return nil
}

func readProofInputs(receiptFile,challengeFile,planFile string)(
 instrumentprobe.LocalProbeReceipt,instrumentprobe.Challenge,
 instrumentprobe.ProbePlan,error){
 var r instrumentprobe.LocalProbeReceipt
 var c instrumentprobe.Challenge
 var p instrumentprobe.ProbePlan
 if err:=readCanonical(receiptFile,&r,true);err!=nil{
  return r,c,p,err
 }
 if err:=readCanonical(challengeFile,&c,true);err!=nil{
  return r,c,p,err
 }
 if err:=readCanonical(planFile,&p,true);err!=nil{
  return r,c,p,err
 }
 return r,c,p,nil
}

func parseInstrumentKey(base64Key string)(ed25519.PublicKey,error){
 key,err:=base64.StdEncoding.DecodeString(base64Key)
 if err!=nil||len(key)!=ed25519.PublicKeySize{
  return nil,errors.New("independent enrolled 32-byte Ed25519 public key required")
 }
 return ed25519.PublicKey(key),nil
}

func loadExistingNodeIdentity(dataDir,nodeID string)(nodefederation.Identity,error){
 if dataDir==""||nodeID==""||strings.TrimSpace(nodeID)!=nodeID{
  return nodefederation.Identity{},errors.New("existing Node data directory and Node ID required")
 }
 // Never invoke EnsureIdentity: it may create new keys on an unprovisioned
 // directory, which would be misleading evidence of prior Node enrollment.
 certPath:=filepath.Join(dataDir,"keys","federation.crt")
 keyPath:=filepath.Join(dataDir,"keys","federation.key")
 certPEM,err:=readBounded(certPath,65536,false)
 if err!=nil{return nodefederation.Identity{},err}
 keyPEM,err:=readBounded(keyPath,65536,true)
 if err!=nil{return nodefederation.Identity{},err}
 pair,err:=tls.X509KeyPair(certPEM,keyPEM)
 if err!=nil{return nodefederation.Identity{},err}
 if len(pair.Certificate)!=1{
  return nodefederation.Identity{},errors.New("expected one pre-enrolled Node certificate")
 }
 leaf,err:=x509.ParseCertificate(pair.Certificate[0])
 if err!=nil{return nodefederation.Identity{},err}
 if leaf.Subject.CommonName!=nodeID{
  return nodefederation.Identity{},errors.New("existing Node identity differs from selected Node ID")
 }
 return nodefederation.Identity{Certificate:pair,CertPEM:certPEM,
  Fingerprint:nodefederation.CertificateFingerprint(pair.Certificate[0])},nil
}

func exclusivePrivateJSON(path string,record any)error{
 if path==""{return errors.New("explicit output path required")}
 data,err:=json.Marshal(record)
 if err!=nil{return err}
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return err}
 defer f.Close()
 if _,err=f.Write(append(data,'\n'));err!=nil{return err}
 if err=f.Sync();err!=nil{return err}
 if runtime.GOOS!="windows"{
  dir,err:=os.Open(filepath.Dir(path))
  if err!=nil{return err}
  defer dir.Close()
  if err=dir.Sync();err!=nil{return err}
 }
 return nil
}

func runSign(args []string)error{
 fs:=flag.NewFlagSet("sign",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 dataDir:=fs.String("data-dir","","existing local Node data directory")
 nodeID:=fs.String("node-id","","independently enrolled Node ID")
 receiptFile:=fs.String("receipt","","private canonical signed local receipt")
 challengeFile:=fs.String("challenge","","original private OnePane challenge")
 planFile:=fs.String("plan","","original private approved probe plan")
 instrumentKeyArg:=fs.String("instrument-public-key","","independently enrolled base64 Ed25519 instrument public key")
 outFile:=fs.String("output","","new exclusive owner-only Node witness file")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*dataDir==""||*nodeID==""||*receiptFile==""||
  *challengeFile==""||*planFile==""||*instrumentKeyArg==""||*outFile==""{
  return errors.New("sign needs --data-dir, --node-id, --receipt, --challenge, --plan, --instrument-public-key and --output")
 }
 key,err:=parseInstrumentKey(*instrumentKeyArg)
 if err!=nil{return err}
 receipt,ch,plan,err:=readProofInputs(*receiptFile,*challengeFile,*planFile)
 if err!=nil{return err}
 ident,err:=loadExistingNodeIdentity(*dataDir,*nodeID)
 if err!=nil{return err}
 witness,err:=nodefederation.SignLocalInstrumentCustody(*nodeID,ident,
  receipt,ch,plan,key,time.Now())
 if err!=nil{return err}
 if err=exclusivePrivateJSON(*outFile,witness);err!=nil{return err}
 fmt.Println("Private Node key custody witness created; no physical execution or sensor attestation.")
 return nil
}

func runVerify(args []string)error{
 fs:=flag.NewFlagSet("verify",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 witnessFile:=fs.String("witness","","private Node custody witness")
 receiptFile:=fs.String("receipt","","original private instrument report")
 challengeFile:=fs.String("challenge","","original private challenge")
 planFile:=fs.String("plan","","original private approved endpoint plan")
 nodeCertFile:=fs.String("node-certificate","","independently retained Node certificate PEM")
 nodePin:=fs.String("node-fingerprint","","independently enrolled Node TLS SHA256 fingerprint")
 instrumentKeyArg:=fs.String("instrument-public-key","","independently enrolled instrument Ed25519 public key")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*witnessFile==""||*receiptFile==""||*challengeFile==""||
  *planFile==""||*nodeCertFile==""||*nodePin==""||*instrumentKeyArg==""{
  return errors.New("verify requires --witness, --receipt, --challenge, --plan, --node-certificate, --node-fingerprint and --instrument-public-key")
 }
 key,err:=parseInstrumentKey(*instrumentKeyArg)
 if err!=nil{return err}
 receipt,ch,plan,err:=readProofInputs(*receiptFile,*challengeFile,*planFile)
 if err!=nil{return err}
 var witness nodefederation.NodeInstrumentCustodyWitness
 if err=readCanonical(*witnessFile,&witness,true);err!=nil{return err}
 raw,err:=readBounded(*nodeCertFile,65536,false)
 if err!=nil{return err}
 certBlock,rest:=pem.Decode(raw)
 if certBlock==nil||certBlock.Type!="CERTIFICATE"||len(bytes.TrimSpace(rest))!=0{
  return errors.New("one independently retained Node PEM certificate required")
 }
 if err=nodefederation.VerifyLocalInstrumentCustody(witness,certBlock.Bytes,
  *nodePin,key,receipt,ch,plan);err!=nil{return err}
 fmt.Println("Two-key Node custody signatures match separately supplied approvals. Physical execution remains unverified.")
 return nil
}

func main(){
 if len(os.Args)<2{
  fmt.Fprintln(os.Stderr,"usage: onepane-node-custody sign|verify|probe-and-witness [flags]")
  os.Exit(2)
 }
 var err error
 switch os.Args[1]{
 case "sign":err=runSign(os.Args[2:])
 case "verify":err=runVerify(os.Args[2:])
 case "probe-and-witness":err=runProbeAndWitness(os.Args[2:])
 default:err=errors.New("expected sign or verify")
 }
 if err!=nil{
  fmt.Fprintln(os.Stderr,"Node custody action declined:",err)
  os.Exit(2)
 }
}
