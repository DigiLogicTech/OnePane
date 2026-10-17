package localai

import (
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

func TestManagedRuntimeLogIsBoundedEvenWhenProcessNeverRestarts(t *testing.T){
 path:=filepath.Join(t.TempDir(),"runtime.log")
 f,err:=os.OpenFile(path,os.O_RDWR|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{t.Fatal(err)}
 defer f.Close()
 if _,err:=f.WriteString("existing protected log\n");err!=nil{t.Fatal(err)}
 writer,err:=newBoundedRuntimeLogWriter(f)
 if err!=nil{t.Fatal(err)}
 // stdout and stderr are independent pumps in os/exec, both writing to
 // the same bounded sink. Ensure neither stream can expand the cap.
 chunk:=[]byte(strings.Repeat("X",1<<20))
 var wg sync.WaitGroup
 for i:=0;i<12;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   n,e:=writer.Write(chunk)
   if e!=nil||n!=len(chunk){t.Errorf("runtime output was not drained: %d %v",n,e)}
  }()
 }
 wg.Wait()
 stat,err:=f.Stat()
 if err!=nil{t.Fatal(err)}
 if stat.Size()!=maxManagedRuntimeLogBytes{
  t.Fatalf("noisy model filled storage beyond limit: size=%d",stat.Size())
 }
 n,err:=writer.Write(chunk)
 if err!=nil||n!=len(chunk){t.Fatalf("discarded bytes interrupted process: %d %v",n,err)}
 stat,_=f.Stat()
 if stat.Size()!=maxManagedRuntimeLogBytes{t.Fatal("runtime log grew after cap")}
 if _,err:=f.Seek(0,0);err!=nil{t.Fatal(err)}
 first:=make([]byte,9)
 if _,err:=f.Read(first);err!=nil{t.Fatal(err)}
 if string(first)!="existing p"{t.Fatalf("old diagnostic bytes were replaced: %q",first)}
}
