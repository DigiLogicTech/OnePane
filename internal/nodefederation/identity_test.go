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


// Reading through the checked descriptor, rather than a later by-path open,
// closes a symlink replacement window and bounds hostile credential inputs.
// The unchanged valid identity must still load under its original fingerprint.
func TestNodeIdentityBoundedCheckedFileRead(t *testing.T){
 dir:=t.TempDir()
 first,err:=EnsureIdentity(dir,"node-stable")
 if err!=nil{t.Fatal(err)}
 certPath:=filepath.Join(dir,"keys","federation.crt")
 keyPath:=filepath.Join(dir,"keys","federation.key")
 for _,p:=range []string{certPath,keyPath}{
  contents,err:=readIdentityFile(p,p==keyPath)
  if err!=nil||len(contents)==0{t.Fatalf("legitimate file %q: %v",p,err)}
 }
 second,err:=EnsureIdentity(dir,"node-stable")
 if err!=nil||second.Fingerprint!=first.Fingerprint{
  t.Fatalf("checked credential reader rotated a valid identity: %v",err)
 }
 for _,name:=range []string{"certificate","private_key"}{
  t.Run(name,func(t *testing.T){
   target:=certPath
   private:=false
   if name=="private_key"{target=keyPath;private=true}
   original,err:=os.ReadFile(target)
   if err!=nil{t.Fatal(err)}
   t.Cleanup(func(){_ =os.WriteFile(target,original,0o600)})
   oversized:=bytes.Repeat([]byte("x"),16*1024+1)
   if err=os.WriteFile(target,oversized,0o600);err!=nil{t.Fatal(err)}
   if _,err=readIdentityFile(target,private);err==nil{
    t.Fatal("oversized Node credential was read")
   }
   if _,err=EnsureIdentity(dir,"node-stable");err==nil{
    t.Fatal("oversized persistent Node credential was accepted")
   }
   if err=os.WriteFile(target,original,0o600);err!=nil{t.Fatal(err)}
   third,err:=EnsureIdentity(dir,"node-stable")
   if err!=nil||third.Fingerprint!=first.Fingerprint{
    t.Fatalf("valid file restoration was not preserved: %v",err)
   }
  })
 }
 if runtime.GOOS!="windows"{
  target:=filepath.Join(t.TempDir(),"outside.crt")
  raw,err:=os.ReadFile(certPath)
  if err!=nil{t.Fatal(err)}
  if err=os.WriteFile(target,raw,0o600);err!=nil{t.Fatal(err)}
  link:=filepath.Join(t.TempDir(),"linked.crt")
  if err=os.Symlink(target,link);err!=nil{t.Fatal(err)}
  if _,err=readIdentityFile(link,false);err==nil{
   t.Fatal("linked Node certificate read despite file identity check")
  }
 }
}
