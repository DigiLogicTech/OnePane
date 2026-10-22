package localai

import (
 "context"
 "errors"
 "os"
 "strings"
 "sync"
 "testing"
 "time"
)

// This launches only the Go test executable as a transient, self-owned
// process. No model weights, GPU, network request or production process is
// touched. Native Windows and Linux -race must exercise the exact wait/Alive
// boundary with concurrent inspector calls.
func TestManagedTrackedProcessExitIsRaceFree(t *testing.T){
 if os.Getenv("ONEPANE_TEST_TRACKED_PROCESS_CHILD")=="1"{
  os.Exit(0)
 }
 executable,err:=os.Executable()
 if err!=nil{t.Fatal(err)}
 b:=NewOSProcessBackend()
 for trial:=0;trial<6;trial++{
  pid,err:=b.Start(context.Background(),LaunchSpec{
   Executable:executable,
   Args:[]string{"-test.run=^TestManagedTrackedProcessExitIsRaceFree$"},
   Env:[]string{"ONEPANE_TEST_TRACKED_PROCESS_CHILD=1"},
  })
  if err!=nil{t.Fatalf("start owned test process trial %d: %v",trial,err)}
  const observers=6
  var wg sync.WaitGroup
  deadline:=time.Now().Add(8*time.Second)
  for worker:=0;worker<observers;worker++{
   wg.Add(1)
   go func(){
    defer wg.Done()
    for time.Now().Before(deadline){
     if !b.Alive(pid){return}
     _,identityErr:=b.Identity(pid)
     if identityErr!=nil&&!strings.Contains(identityErr.Error(),"exited")&&
       !errors.Is(identityErr,os.ErrNotExist){
      // After the command has been reaped, an untracked /proc read can
      // legitimately fail (especially on native Windows). The safety
      // property is no stale cached identity or data race, not an OS error.
      _=identityErr
     }
    }
   }()
  }
  wg.Wait()
  if b.Alive(pid){t.Fatalf("owned subprocess %d still reported alive after bounded observation",trial)}
 }
}
