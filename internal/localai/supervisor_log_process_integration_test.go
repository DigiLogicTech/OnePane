package localai

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestOSProcessBackendCapsRealLongRunningRuntimeLog(t *testing.T){
 const helperEnv="ONEPANE_RUNTIME_LOG_TEST_HELPER"
 if os.Getenv(helperEnv)=="enabled" {
  // The child must survive producing much more output than the permitted
  // disk budget. Go pumps stdout/stderr into the bounded runtime log sink.
  chunk:=[]byte(strings.Repeat("L",1<<20))
  for n:=0;n<12;n++{
   if _,err:=os.Stdout.Write(chunk);err!=nil{t.Fatal(err)}
   if _,err:=os.Stderr.Write(chunk);err!=nil{t.Fatal(err)}
  }
  return
 }
 executable,err:=os.Executable()
 if err!=nil{t.Fatal(err)}
 dest:=filepath.Join(t.TempDir(),"private","runtime.log")
 backend:=NewOSProcessBackend()
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
 defer cancel()
 pid,err:=backend.Start(ctx,LaunchSpec{
  Executable:executable,
  Args:[]string{"-test.run=^TestOSProcessBackendCapsRealLongRunningRuntimeLog$"},
  Env:append(os.Environ(),helperEnv+"=enabled"),
  LogPath:dest,
 })
 if err!=nil{t.Fatal(err)}
 if pid<=1{t.Fatalf("invalid helper pid %d",pid)}
 // Poll for the child to complete. The old implementation passed an
 // os.File directly to the child and could fill storage indefinitely.
 for backend.Alive(pid)&&ctx.Err()==nil{
  time.Sleep(20*time.Millisecond)
 }
 if ctx.Err()!=nil{t.Fatal("managed log helper did not exit")}
 // Allow Wait's bounded stdout/stderr pump to flush before asserting.
 deadline:=time.Now().Add(3*time.Second)
 for time.Now().Before(deadline){
  stat,err:=os.Stat(dest)
  if err==nil&&stat.Size()==maxManagedRuntimeLogBytes{return}
  time.Sleep(20*time.Millisecond)
 }
 stat,err:=os.Stat(dest)
 if err!=nil{t.Fatal(err)}
 t.Fatalf("managed process log not bounded/drained: %d bytes",stat.Size())
}
