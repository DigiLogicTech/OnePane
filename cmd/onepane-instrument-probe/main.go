// OnePane one-shot instrument probe. Explicit, human-initiated operation
// only. This program does NOT attest the host's identity or replace OnePane
// human approval, and does not send an HTTP request by itself.
//
// Protocol:
// 1. Human obtains a one-use challenge using authenticated OnePane API.
// 2. probe: one private IPv4 TCP connect, sign private local receipt.
// 3. Human submits the receipt's outcome/observed_at through the existing
//    instrument measurement/prepare endpoint.
// 4. sign: verify the exact prepared bytes against the locally signed receipt,
//    output a detached signature request for human API /measurement/accept.
package main

import(
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "net"
 "os"
 "runtime"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
)

// Read the SAME opened regular file that passed the permission and inode
// checks. The previous Lstat -> ReadFile path allowed a symlink swap
// between checking a private file's mode and following its path.
func readBoundedFile(path string,maxBytes int64,private bool)([]byte,error){
 if path==""{return nil,errors.New("input file required")}
 before,err:=os.Lstat(path)
 if err!=nil{return nil,err}
 if !before.Mode().IsRegular()||before.Mode()&os.ModeSymlink!=0||
  before.Size()>maxBytes{
  return nil,errors.New("input must be an ordinary bounded non-symlink file")
 }
 if private&&runtime.GOOS!="windows"&&before.Mode().Perm()&0077!=0{
  return nil,errors.New("private file must be owner-only (chmod 600)")
 }
 file,err:=os.Open(path)
 if err!=nil{return nil,err}
 defer file.Close()
 after,err:=file.Stat()
 if err!=nil{return nil,err}
 if !after.Mode().IsRegular()||after.Size()>maxBytes||
  !os.SameFile(before,after){
  return nil,errors.New("input file identity changed before read")
 }
 if private&&runtime.GOOS!="windows"&&after.Mode().Perm()&0077!=0{
  return nil,errors.New("private file must be owner-only (chmod 600)")
 }
 contents,err:=io.ReadAll(io.LimitReader(file,maxBytes+1))
 if err!=nil{return nil,err}
 if int64(len(contents))>maxBytes{
  return nil,errors.New("input exceeded configured byte cap")
 }
 return contents,nil
}

func readPrivateKey(path string)(ed25519.PrivateKey,error){
 contents,err:=readBoundedFile(path,256,true)
 if err!=nil{return nil,err}
 // 64-byte Go Ed25519 key in a local, owner-only base64 file.
 // Never upload this file or pass its contents on a command line.
 bytes,err:=base64.StdEncoding.DecodeString(string(trimSpace(contents)))
 if err!=nil||len(bytes)!=ed25519.PrivateKeySize{
  return nil,errors.New("expected base64 Ed25519 64-byte private key")
 }
 return ed25519.PrivateKey(bytes),nil
}
func trimSpace(s []byte)[]byte{
 begin,end:=0,len(s)
 for begin<end&&(s[begin]=='\n'||s[begin]=='\r'||s[begin]=='\t'||s[begin]==' '){begin++}
 for end>begin&&(s[end-1]=='\n'||s[end-1]=='\r'||s[end-1]=='\t'||s[end-1]==' '){end--}
 return s[begin:end]
}
func decodeInputJSON(path string,v any,private bool)error{
 bytes,err:=readBoundedFile(path,8192,private)
 if err!=nil{return err}
 // Canonical files produced by OnePane or this CLI. Duplicate, unknown
 // or inconsistent fields cannot rewrite the signed object silently.
 if err=json.Unmarshal(bytes,v);err!=nil{return err}
 canonical,err:=json.Marshal(v)
 if err!=nil{return err}
 if string(trimSpace(bytes))!=string(canonical){
  return errors.New("input JSON must use canonical OnePane encoding")
 }
 return nil
}
func readJSON(path string,v any)error{
 return decodeInputJSON(path,v,false)
}
// A plan contains private network topology and a local receipt has a
// detached signature. Require a private, non-symlink regular file.
func readPrivateJSON(path string,v any)error{
 return decodeInputJSON(path,v,true)
}

func writeExclusive(path string,record any)error{
 if path==""{return errors.New("explicit output file required")}
 b,err:=json.Marshal(record)
 if err!=nil{return err}
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return err}
 defer f.Close()
 if _,err=f.Write(append(b,'\n'));err!=nil{return err}
 return f.Sync()
}
// Key generation happens exclusively on the instrument host. The private
// key stays in a new local file (never printed, overwritten or uploaded).
func runKeygen(args []string)error{
 fs:=flag.NewFlagSet("keygen",flag.ContinueOnError)
 dest:=fs.String("private-key-out","","new, exclusive private key path")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*dest==""{return errors.New("keygen needs --private-key-out")}
 pub,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{return err}
 fd,err:=os.OpenFile(*dest,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return err}
 // Keep the key on disk in base64 form for local reads. No cloud sync,
 // command-line argument, clipboard or standard output carries it.
 _,writeErr:=fd.Write([]byte(base64.StdEncoding.EncodeToString(private)+"\n"))
 if writeErr==nil{writeErr=fd.Sync()}
 closeErr:=fd.Close()
 if writeErr!=nil{return writeErr}
 if closeErr!=nil{return closeErr}
 digest:=sha256.Sum256(pub)
 fmt.Println("Ed25519 public key (base64, for human OnePane enrollment):")
 fmt.Println(base64.StdEncoding.EncodeToString(pub))
 fmt.Printf("Public key SHA256 fingerprint: sha256:%x\n",digest[:])
 if runtime.GOOS=="windows"{
  fmt.Println("Windows: restrict the private key's NTFS ACL to the intended instrument account.")
 }
 return nil
}

func runProbe(args []string)error{
 fs:=flag.NewFlagSet("probe",flag.ContinueOnError)
 challenge:=fs.String("challenge","","human-issued JSON challenge file")
 planFile:=fs.String("plan","","human-only current OnePane approved probe-plan JSON")
 allowManual:=fs.Bool("allow-manual-target",false,
  "explicit less-protected fallback; operator must independently verify the approved IP/port")
 keyFile:=fs.String("private-key","","base64 Ed25519 private key 0600 file")
 addr:=fs.String("ipv4","","operator-verified exact private IPv4 literal")
 port:=fs.Int("port",0,"operator-approved TCP port")
 timeout:=fs.Int("timeout-ms",0,"original permit timeout, 100-1500 ms")
 declared:=fs.String("confirm-target-id","","human-approved declared target ID")
 output:=fs.String("out","","new 0600 local probe receipt JSON path")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected positional arguments")}
 var ch instrumentprobe.Challenge
 if err:=readJSON(*challenge,&ch);err!=nil{return err}
 pk,err:=readPrivateKey(*keyFile)
 if err!=nil{return err}
 var receipt instrumentprobe.LocalProbeReceipt
 if *planFile!=""{
  // Fail closed: a plan must be the sole source of the endpoint; mixing
  // legacy manually selected flags could silently change approved scope.
  if *allowManual||*addr!=""||*port!=0||*timeout!=0||*declared!=""{
   return errors.New("approved --plan cannot be combined with manual endpoint flags")
  }
  var plan instrumentprobe.ProbePlan
  if err=readPrivateJSON(*planFile,&plan);err!=nil{return err}
  receipt,err=instrumentprobe.ProbeApprovedPlan(context.Background(),ch,plan,
   pk,&net.Dialer{},time.Now)
 }else{
  // Do not silently downgrade a missing or misplaced authenticated
  // plan to a manually chosen endpoint.
  if !*allowManual{
   return errors.New("approved --plan required; explicit --allow-manual-target is a less-protected fallback")
  }
  // Explicit legacy manual operator workflow. OnePane cannot establish
  // that these manually selected bytes match the server's original permit.
  target:=instrumentprobe.Target{IPv4:*addr,Port:*port,TimeoutMS:*timeout,
   ConfirmedDeclaredTargetID:*declared}
  receipt,err=instrumentprobe.ProbeOnce(context.Background(),ch,target,
   pk,&net.Dialer{},time.Now)
 }
 if err!=nil{return err}
 if err=writeExclusive(*output,receipt);err!=nil{return err}
 fmt.Println("One explicit private-IPv4 TCP probe completed; local signed receipt saved.")
 fmt.Println("Outcome is an instrument self-report, NOT physical device attestation.")
 return nil
}
func runSign(args []string)error{
 fs:=flag.NewFlagSet("sign",flag.ContinueOnError)
 report:=fs.String("receipt","","signed private local probe report file")
 prepared:=fs.String("prepared","","human API /measurement/prepare response file")
 keyFile:=fs.String("private-key","","same locally held private signing key")
 revision:=fs.Int64("goal-revision",0,"current Goal revision expected by OnePane")
 output:=fs.String("out","","new JSON body file for human API /measurement/accept")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected positional arguments")}
 pk,err:=readPrivateKey(*keyFile)
 if err!=nil{return err}
 var local instrumentprobe.LocalProbeReceipt
 if err=readPrivateJSON(*report,&local);err!=nil{return err}
 var preparedDoc instrumentprobe.PreparedMeasurement
 if err=readJSON(*prepared,&preparedDoc);err!=nil{return err}
 body,err:=instrumentprobe.SignPrepared(local,preparedDoc,pk,*revision,time.Now())
 if err!=nil{return err}
 if err=writeExclusive(*output,body);err!=nil{return err}
 fmt.Println("Signed admission body saved for human-authorised OnePane import.")
 fmt.Println("No background monitoring, scan or host modification was performed.")
 return nil
}
func main(){
 var err error
 if len(os.Args)<2{
  err=errors.New("usage: onepane-instrument-probe keygen|probe|sign [flags]")
 }else{
  switch os.Args[1]{
  case "keygen":err=runKeygen(os.Args[2:])
  case "probe":err=runProbe(os.Args[2:])
  case "sign":err=runSign(os.Args[2:])
  default:err=fmt.Errorf("unsupported instrument operation: %s",os.Args[1])
  }
 }
 if err!=nil{
  fmt.Fprintln(os.Stderr,"onepane-instrument-probe:",err)
  os.Exit(2)
 }
}
