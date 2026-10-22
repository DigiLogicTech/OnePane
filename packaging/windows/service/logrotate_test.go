//go:build windows

package main

import (
 "bytes"
 "errors"
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

func TestServiceLogProtectsExistingRotatedHistory(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"onepane.log")
 prior:=path+".1"
 if err:=os.WriteFile(prior,[]byte("legacy diagnostic history\n"),0644);err!=nil{t.Fatal(err)}
 broad,err:=windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
 if err!=nil{t.Fatal(err)}
 dacl,_,err:=broad.DACL()
 if err!=nil{t.Fatal(err)}
 if err:=windows.SetNamedSecurityInfo(prior,windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{t.Fatal(err)}
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if err:=w.Close();err!=nil{t.Fatal(err)}
 f,err:=os.Open(prior)
 if err!=nil{t.Fatal(err)}
 if err:=verifyPrivateServiceConfig(f);err!=nil{f.Close();t.Fatal(err)}
 f.Close()
 saved,err:=os.ReadFile(prior)
 if err!=nil||string(saved)!="legacy diagnostic history\n"{
  t.Fatalf("rotated log history was changed: %q %v",saved,err)
 }
}

func TestOversizedSingleServiceLogWriteStaysBounded(t *testing.T){
 path:=filepath.Join(t.TempDir(),"onepane.log")
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 // A single large stream buffer previously bypassed the max-size guard.
 // Construct data with a distinctive suffix to verify the newest bytes.
 data:=bytes.Repeat([]byte("A"),int(2*onePaneLogMaxBytes)+123)
 copy(data[len(data)-7:],[]byte("LATEST!"))
 n,err:=w.Write(data)
 if err!=nil||n!=len(data){t.Fatalf("large append: written=%d expected=%d err=%v",n,len(data),err)}
 if err:=w.Close();err!=nil{t.Fatal(err)}
 var footprint int64
 for _,candidate:=range []string{path,path+".1"}{
  stat,err:=os.Stat(candidate)
  if err!=nil{t.Fatalf("log segment missing: %v",err)}
  if stat.Size()>onePaneLogMaxBytes{
   t.Fatalf("bounded service log segment exceeded limit: %s %d",candidate,stat.Size())
  }
  footprint+=stat.Size()
  f,err:=os.Open(candidate)
  if err!=nil{t.Fatal(err)}
  if err:=verifyPrivateServiceConfig(f);err!=nil{f.Close();t.Fatal(err)}
  f.Close()
 }
 if footprint>2*onePaneLogMaxBytes{t.Fatalf("bounded rotated log consumed %d bytes",footprint)}
 last,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if !bytes.HasSuffix(last,[]byte("LATEST!")){t.Fatal("newest log data missing after multi-rotation append")}
}

func TestServiceLogBoundaryDoesNotRotateOnEmptyWrite(t *testing.T){
 path:=filepath.Join(t.TempDir(),"onepane.log")
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if n,err:=w.Write(nil);err!=nil||n!=0{t.Fatalf("empty write: %d %v",n,err)}
 if _,err:=os.Stat(path+".1");!os.IsNotExist(err){t.Fatalf("empty write unexpectedly rotated: %v",err)}
 if err:=w.Close();err!=nil{t.Fatal(err)}
}

func TestServiceLogRenameFailureCanResumeSafeAppend(t *testing.T){
 path:=filepath.Join(t.TempDir(),"onepane.log")
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if _,err:=w.Write([]byte("before\n"));err!=nil{t.Fatal(err)}
 denied:=errors.New("simulated Windows rename sharing violation")
 rotateErr:=w.rotateWith(os.Remove,func(string,string)error{return denied})
 if !errors.Is(rotateErr,denied){t.Fatalf("rename cause lost: %v",rotateErr)}
 if w.file==nil{t.Fatal("rename failure permanently disabled service log")}
 if _,err:=w.Write([]byte("after\n"));err!=nil{t.Fatalf("append after failed rotation: %v",err)}
 if err:=verifyPrivateServiceConfig(w.file);err!=nil{t.Fatal(err)}
 if err:=w.Close();err!=nil{t.Fatal(err)}
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!="before\nafter\n"{
  t.Fatalf("failed rename overwrote operator diagnostics: %q %v",saved,err)
 }
}

func TestServiceLogNewFileOpenFailureRollsBackOriginalHistory(t *testing.T){
 path:=filepath.Join(t.TempDir(),"onepane.log")
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if _,err:=w.Write([]byte("before replacement error\n"));err!=nil{t.Fatal(err)}
 denied:=errors.New("simulated protected log creation denied")
 rotateErr:=w.rotateWithOpen(os.Remove,os.Rename,func(string)(*os.File,error){
  return nil,denied
 })
 if !errors.Is(rotateErr,denied){t.Fatalf("lost original failure: %v",rotateErr)}
 if w.file==nil{t.Fatal("failed replacement permanently disabled log writer")}
 if _,err:=w.Write([]byte("after recovery\n"));err!=nil{
  t.Fatalf("logging could not recover: %v",err)
 }
 if err:=verifyPrivateServiceConfig(w.file);err!=nil{t.Fatal(err)}
 if err:=w.Close();err!=nil{t.Fatal(err)}
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!="before replacement error\nafter recovery\n"{
  t.Fatalf("recovery changed history: %q %v",saved,err)
 }
 if _,err:=os.Stat(path+".1");!os.IsNotExist(err){
  t.Fatalf("failed rotation left duplicate historical file: %v",err)
 }
}

func TestServiceLogOpenFailureNeverOverwritesUnexpectedReplacement(t *testing.T){
 path:=filepath.Join(t.TempDir(),"onepane.log")
 w,err:=openBoundedLogWriter(path)
 if err!=nil{t.Fatal(err)}
 if _,err:=w.Write([]byte("historic log\n"));err!=nil{t.Fatal(err)}
 injected:=errors.New("simulated concurrent pathname replacement")
 rotateErr:=w.rotateWithOpen(os.Remove,os.Rename,func(path string)(*os.File,error){
  if err:=os.WriteFile(path,[]byte("foreign file\n"),0600);err!=nil{
   return nil,err
  }
  return nil,injected
 })
 if !errors.Is(rotateErr,injected){t.Fatalf("missing error: %v",rotateErr)}
 if w.file!=nil{t.Fatal("unexpected replacement was silently used as service log")}
 foreign,err:=os.ReadFile(path)
 if err!=nil||string(foreign)!="foreign file\n"{
  t.Fatalf("unexpected replacement was overwritten: %q %v",foreign,err)
 }
 prior,err:=os.ReadFile(path+".1")
 if err!=nil||string(prior)!="historic log\n"{
  t.Fatalf("original diagnostic history lost: %q %v",prior,err)
 }
}
