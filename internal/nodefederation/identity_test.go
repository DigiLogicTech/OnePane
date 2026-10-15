package nodefederation

import (
	"bytes"
 "crypto/x509"
 "os"
 "path/filepath"
 "runtime"
 "strings"
	"testing"
)

func TestEnsureIdentityStableAndBoundToNode(t *testing.T) {
	dir := t.TempDir()
	a, err := EnsureIdentity(dir, "node-test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EnsureIdentity(dir, "node-test")
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("fingerprint changed: %s != %s", a.Fingerprint, b.Fingerprint)
	}
	leaf, err := x509.ParseCertificate(a.Certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "node-test" {
		t.Fatalf("CN=%q", leaf.Subject.CommonName)
	}
}

func TestEnsureIdentityRejectsNodeIdentityChange(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureIdentity(dir, "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIdentity(dir, "node-b"); err == nil {
		t.Fatal("certificate silently rebound to another node")
	}
}

func TestEnsureIdentityPreservesIncompleteCredentialsInsteadOfRotating(t *testing.T){
 for _,missing:=range []string{"certificate","private_key"}{
  t.Run(missing,func(t *testing.T){
   dir:=t.TempDir()
   original,err:=EnsureIdentity(dir,"node-original")
   if err!=nil{t.Fatal(err)}
   certPath:=filepath.Join(dir,"keys","federation.crt")
   keyPath:=filepath.Join(dir,"keys","federation.key")
   keep,remove:=certPath,keyPath
   if missing=="certificate"{keep,remove=keyPath,certPath}
   before,err:=os.ReadFile(keep)
   if err!=nil{t.Fatal(err)}
   if err=os.Remove(remove);err!=nil{t.Fatal(err)}
   if _,err=EnsureIdentity(dir,"node-original");err==nil{
    t.Fatal("incomplete Node identity regenerated without operator approval")
   }
   after,err:=os.ReadFile(keep)
   if err!=nil||!bytes.Equal(before,after){
    t.Fatalf("existing Node credential changed: %v",err)
   }
   if original.Fingerprint==""{t.Fatal("missing original identity fingerprint")}
  })
 }
}

func TestEnsureIdentityRejectsLinkedOrInsecureCredentialFiles(t *testing.T){
 if runtime.GOOS=="windows"{t.Skip("Unix symlink and mode semantics; Windows ACLs need installed QA")}
 dir:=t.TempDir()
 if _,err:=EnsureIdentity(dir,"node");err!=nil{t.Fatal(err)}
 keyPath:=filepath.Join(dir,"keys","federation.key")
 before,err:=os.ReadFile(keyPath)
 if err!=nil{t.Fatal(err)}
 if err=os.Chmod(keyPath,0o664);err!=nil{t.Fatal(err)}
 if _,err=EnsureIdentity(dir,"node");err==nil{
  t.Fatal("group/world-readable private Node key was admitted")
 }
 if err=os.Chmod(keyPath,0o600);err!=nil{t.Fatal(err)}
 external:=filepath.Join(t.TempDir(),"external.key")
 if err=os.WriteFile(external,before,0o600);err!=nil{t.Fatal(err)}
 if err=os.Remove(keyPath);err!=nil{t.Fatal(err)}
 if err=os.Symlink(external,keyPath);err!=nil{t.Fatal(err)}
 if _,err=EnsureIdentity(dir,"node");err==nil{
  t.Fatal("linked key identity was followed outside the Node's key directory")
 }
 after,err:=os.ReadFile(external)
 if err!=nil||!bytes.Equal(before,after){
  t.Fatalf("out-of-directory target changed: %v",err)
 }
}

func TestEnsureIdentityRejectsUnusableNodeIDsBeforeDiskWrites(t *testing.T){
 for _,nodeID:=range []string{
  "", " node","node ","node\nother","node\x00other",strings.Repeat("x",129),
 }{
  dir:=t.TempDir()
  if _,err:=EnsureIdentity(dir,nodeID);err==nil{
   t.Fatalf("invalid Node id %q minted a permanent certificate",nodeID)
  }
  if _,err:=os.Stat(filepath.Join(dir,"keys"));!os.IsNotExist(err){
   t.Fatalf("invalid Node id %q created credential files: %v",nodeID,err)
  }
 }
}
