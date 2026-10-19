//go:build windows

package main

import (
 "os"
 "path/filepath"
 "testing"

 "golang.org/x/sys/windows"
)

func TestServiceLogSecuresExistingHistoryBeforeAppend(t *testing.T){
 path:=filepath.Join(t.TempDir(),"logs","onepane.log")
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("history\n"),0644);err!=nil{t.Fatal(err)}
 broad,err:=windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
 if err!=nil{t.Fatal(err)}
 dacl,_,err:=broad.DACL()
 if err!=nil{t.Fatal(err)}
 if err:=windows.SetNamedSecurityInfo(path,windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{t.Fatal(err)}
 writer,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(writer.file);err!=nil{t.Fatal(err)}
 if _,err:=writer.Write([]byte("startup\n"));err!=nil{t.Fatal(err)}
 if err:=writer.Close();err!=nil{t.Fatal(err)}
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!="history\nstartup\n"{
  t.Fatalf("existing diagnostics overwritten: %q %v",saved,err)
 }
}

func TestRotatedServiceLogsRemainPrivate(t *testing.T){
 path:=filepath.Join(t.TempDir(),"logs","onepane.log")
 writer,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if _,err:=writer.Write([]byte("first\n"));err!=nil{t.Fatal(err)}
 writer.size=onePaneLogMaxBytes
 if _,err:=writer.Write([]byte("second\n"));err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(writer.file);err!=nil{t.Fatalf("new file not private: %v",err)}
 if err:=writer.Close();err!=nil{t.Fatal(err)}
 for _,target:=range []string{path,path+".1"}{
  f,err:=os.Open(target)
  if err!=nil{t.Fatal(err)}
  if err:=verifyPrivateServiceConfig(f);err!=nil{f.Close();t.Fatal(err)}
  f.Close()
 }
 old,_:=os.ReadFile(path+".1")
 newData,_:=os.ReadFile(path)
 if string(old)!="first\n"||string(newData)!="second\n"{
  t.Fatalf("rotation lost log records: %q %q",old,newData)
 }
}

func TestServiceLogRejectsNonregularTargets(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"onepane.log")
 if err:=os.Mkdir(path,0700);err!=nil{t.Fatal(err)}
 if w,err:=openPrivateServiceLogFile(path);err==nil{
  w.Close();t.Fatal("directory accepted for service log")
 }
}
