//go:build windows

package main

import (
 "errors"
 "os"
 "path/filepath"
 "testing"

 "golang.org/x/sys/windows"
)

func TestInitialServiceConfigPreservesExistingAndUsesProtectedDACL(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"config.yaml")
 first:=[]byte("onepane-secret: first\n")
 if err:=persistInitialServiceConfig(path,first);err!=nil{t.Fatal(err)}
 f,err:=os.Open(path)
 if err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(f);err!=nil{f.Close();t.Fatal(err)}
 f.Close()
 if err:=persistInitialServiceConfig(path,[]byte("onepane-secret: overwritten\n"));err!=nil{
  t.Fatal(err)
 }
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!=string(first){
  t.Fatalf("existing service configuration changed: %q %v",saved,err)
 }
}

func TestPrivateServiceConfigRejectsDirectoryAndSymlink(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"config.yaml")
 if err:=os.Mkdir(path,0700);err!=nil{t.Fatal(err)}
 if err:=persistInitialServiceConfig(path,[]byte("never"));err==nil{
  t.Fatal("directory was accepted as an existing service config")
 }
 if err:=os.Remove(path);err!=nil{t.Fatal(err)}
 target:=filepath.Join(root,"outside.yaml")
 if err:=os.WriteFile(target,[]byte("untouched"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(target,path);err==nil{
  if err:=persistInitialServiceConfig(path,[]byte("unsafe"));err==nil{
   t.Fatal("linked external service config was accepted")
  }
  got,err:=os.ReadFile(target)
  if err!=nil||string(got)!="untouched"{t.Fatalf("linked target modified: %q %v",got,err)}
 }else if !errors.Is(err,windows.ERROR_PRIVILEGE_NOT_HELD){
  // Windows runner policy may deny symlink creation. Do not pretend the
  // linked-file negative test ran when privilege is unavailable.
  t.Logf("symlink creation unavailable on this Windows account: %v",err)
 }
}

func TestServiceConfigACLValidationRefusesBroadEveryone(t *testing.T){
 path:=filepath.Join(t.TempDir(),"config.yaml")
 f,err:=os.OpenFile(path,os.O_RDWR|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{t.Fatal(err)}
 defer f.Close()
 broad,err:=windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
 if err!=nil{t.Fatal(err)}
 dacl,_,err:=broad.DACL()
 if err!=nil{t.Fatal(err)}
 if err:=windows.SetNamedSecurityInfo(f.Name(),windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(f);err==nil{
  t.Fatal("Everyone-access configuration incorrectly accepted as private")
 }
 if err:=secureNewServiceConfig(f);err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(f);err!=nil{t.Fatal(err)}
}
