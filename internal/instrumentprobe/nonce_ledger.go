package instrumentprobe

import (
 "crypto/sha256"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "runtime"
)

// ClaimLocalProbeOnce is the crash-persistent local execution fence used by
// both instrument and Node one-shot CLIs. Reserve the original challenge
// bytes before any socket effect; failed dial or crash spends the nonce.
// Deleting files or duplicating an instrument key defeats this local fence.
// This is not hardware attestation or global replay defence.
func ClaimLocalProbeOnce(keyFile string,ch Challenge)error{
 if len(ch.SigningPayload)==0{return errors.New("instrument challenge missing")}
 dirname,err:=probeClaimsDir(keyFile)
 if err!=nil{return err}
 mkdirErr:=os.Mkdir(dirname,0700)
 if mkdirErr!=nil&&!os.IsExist(mkdirErr){return mkdirErr}
 info,err:=os.Lstat(dirname)
 if err!=nil{return err}
 if !info.IsDir()||info.Mode()&os.ModeSymlink!=0{
  return errors.New("nonce ledger must be a real directory, not a symlink")
 }
 if runtime.GOOS!="windows"&&info.Mode().Perm()&0077!=0{
  return errors.New("nonce ledger directory must be owner-only (chmod 700)")
 }
 if mkdirErr==nil{
  if err=syncLedgerDir(filepath.Dir(dirname));err!=nil{
   return fmt.Errorf("cannot durably create nonce ledger: %w",err)
  }
 }
 digest:=sha256.Sum256(ch.SigningPayload)
 claimFile:=filepath.Join(dirname,fmt.Sprintf("%x.claim",digest[:]))
 file,err:=os.OpenFile(claimFile,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return fmt.Errorf("one-shot nonce already claimed or ledger unavailable: %w",err)}
 if _,err=file.Write([]byte("attempt-reserved\n"));err!=nil{
  _=file.Close();return err
 }
 if err=file.Sync();err!=nil{_ =file.Close();return err}
 if err=file.Close();err!=nil{return err}
 if err=syncLedgerDir(dirname);err!=nil{
  return fmt.Errorf("cannot durably reserve nonce: %w",err)
 }
 return nil
}
func probeClaimsDir(keyFile string)(string,error){
 if keyFile==""{return "",errors.New("instrument key path required")}
 abs,err:=filepath.Abs(keyFile)
 if err!=nil{return "",err}
 parent,err:=filepath.EvalSymlinks(filepath.Dir(abs))
 if err!=nil{return "",fmt.Errorf("resolve instrument key parent: %w",err)}
 return filepath.Join(parent,filepath.Base(abs))+".onepane-probe-claims",nil
}
func syncLedgerDir(path string)error{
 if runtime.GOOS=="windows"{return nil}
 f,err:=os.Open(path)
 if err!=nil{return err}
 defer f.Close()
 return f.Sync()
}
