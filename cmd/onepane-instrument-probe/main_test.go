package main

import(
 "bytes"
 "crypto/ed25519"
 "encoding/base64"
 "os"
 "path/filepath"
 "runtime"
 "testing"
)
func TestLocalInstrumentKeygenIsExclusiveAndPrivate(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"instrument.secret")
 if err:=runKeygen([]string{"--private-key-out",path});err!=nil{
  t.Fatalf("local key generation rejected: %v",err)
 }
 key,err:=readPrivateKey(path)
 if err!=nil||len(key)!=ed25519.PrivateKeySize{
  t.Fatalf("generated local private key not usable: %v",err)
 }
 pub:=key.Public().(ed25519.PublicKey)
 if len(pub)!=ed25519.PublicKeySize{
  t.Fatal("generated instrument Ed25519 key is invalid")
 }
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if !bytes.Equal(bytes.TrimSpace(raw),[]byte(base64.StdEncoding.EncodeToString(key))){
  t.Fatal("private key file was not properly generated")
 }
 if err=runKeygen([]string{"--private-key-out",path});err==nil{
  t.Fatal("instrument key generation overwrote an existing private key")
 }
 info,err:=os.Stat(path)
 if err!=nil{t.Fatal(err)}
 if runtime.GOOS!="windows"{
  if info.Mode().Perm()!=0600{
   t.Fatalf("private file permissions not owner-only: %v",info.Mode().Perm())
  }
  if err=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
  if _,err=readPrivateKey(path);err==nil{
   t.Fatal("insecure world-readable instrument key was loaded")
  }
 }
}
func TestInstrumentProbeCLIRefusesMalformedKey(t *testing.T){
 path:=filepath.Join(t.TempDir(),"bad-key")
 if err:=os.WriteFile(path,[]byte("definitely-not-a-secret-key"),0600);err!=nil{t.Fatal(err)}
 if _,err:=readPrivateKey(path);err==nil{
  t.Fatal("invalid key file was accepted")
 }
 if err:=runProbe([]string{"--private-key",path});err==nil{
  t.Fatal("one-shot execution proceeded without signed human challenge")
 }
 if err:=runSign([]string{"--private-key",path});err==nil{
  t.Fatal("offline admission signed without private key or evidence")
 }
}
