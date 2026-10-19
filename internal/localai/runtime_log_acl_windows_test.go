//go:build windows

package localai
import (
 "bytes"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "golang.org/x/sys/windows"
)
func TestWindowsManagedRuntimeLogProtectsExistingBroadACL(t *testing.T){
 path:=filepath.Join(t.TempDir(),"logs","model.log")
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("prior history\n"),0644);err!=nil{t.Fatal(err)}
 broad,err:=windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
 if err!=nil{t.Fatal(err)}
 dacl,_,err:=broad.DACL()
 if err!=nil{t.Fatal(err)}
 if err:=windows.SetNamedSecurityInfo(path,windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{t.Fatal(err)}
 f,err:=openManagedRuntimeLog(path)
 if err!=nil{t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
 sd,err:=windows.GetNamedSecurityInfo(path,windows.SE_FILE_OBJECT,windows.DACL_SECURITY_INFORMATION)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(strings.ToUpper(sd.String()),";;;WD)"){t.Fatal("Everyone still allowed to read log")}
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!="prior history\n"{t.Fatalf("log history changed: %q %v",saved,err)}
}
func TestWindowsManagedRuntimeLogCurrentAndArchivePrivate(t *testing.T){
 path:=filepath.Join(t.TempDir(),"logs","model.log")
 f,err:=openManagedRuntimeLog(path)
 if err!=nil{t.Fatal(err)}
 if _,err:=f.Write(make([]byte,int(maxManagedRuntimeLogBytes)));err!=nil{t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
 next,err:=openManagedRuntimeLog(path)
 if err!=nil{t.Fatal(err)}
 if err:=next.Close();err!=nil{t.Fatal(err)}
 for _,p:=range []string{path,path+".previous"}{
  sd,err:=windows.GetNamedSecurityInfo(p,windows.SE_FILE_OBJECT,
   windows.DACL_SECURITY_INFORMATION)
  if err!=nil{t.Fatal(err)}
  flags,_,err:=sd.Control()
  if err!=nil||flags&windows.SE_DACL_PROTECTED==0{t.Fatalf("runtime log not private: %s %v",p,err)}
 }
}

func TestWindowsLegacyFullRuntimeLogPrivateAfterRotation(t *testing.T){
 path:=filepath.Join(t.TempDir(),"logs","legacy.log")
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 payload:=make([]byte,int(maxManagedRuntimeLogBytes))
 copy(payload,[]byte("legacy model diagnostic bytes"))
 if err:=os.WriteFile(path,payload,0644);err!=nil{t.Fatal(err)}
 broad,err:=windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
 if err!=nil{t.Fatal(err)}
 dacl,_,err:=broad.DACL()
 if err!=nil{t.Fatal(err)}
 if err:=windows.SetNamedSecurityInfo(path,windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{t.Fatal(err)}
 f,err:=openManagedRuntimeLog(path)
 if err!=nil{t.Fatal(err)}
 _=f.Close()
 prior:=path+".previous"
 sd,err:=windows.GetNamedSecurityInfo(prior,windows.SE_FILE_OBJECT,windows.DACL_SECURITY_INFORMATION)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(strings.ToUpper(sd.String()),";;;WD)"){
  t.Fatal("legacy model diagnostic archive retains broad Everyone DACL")
 }
 saved,err:=os.ReadFile(prior)
 if err!=nil||len(saved)!=len(payload)||!bytes.Equal(saved[:len("legacy model diagnostic bytes")],payload[:len("legacy model diagnostic bytes")]){
  t.Fatalf("legacy archived content damaged: %v",err)
 }
}
