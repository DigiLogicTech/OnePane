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

func TestManagedTrackedProcessCleanupIsGenerationFenced(t *testing.T) {
 b:=NewOSProcessBackend()
 older:=&trackedManagedCommand{exited:make(chan struct{})}
 newer:=&trackedManagedCommand{exited:make(chan struct{})}
 const reusedPID=4321
 b.mu.Lock()
 b.cmds[reusedPID]=newer
 b.mu.Unlock()
 // The delayed cleanup for the previous launch must not drop a newly
 // registered launch whose OS process reused the same numeric PID.
 b.forgetTrackedProcess(reusedPID,older)
 b.mu.Lock()
 current:=b.cmds[reusedPID]
 b.mu.Unlock()
 if current!=newer{t.Fatal("stale managed subprocess Wait deleted a newer PID generation")}
 if !b.Alive(reusedPID){t.Fatal("new managed PID was treated as exited")}
 b.forgetTrackedProcess(reusedPID,newer)
 b.mu.Lock()
 _,found:=b.cmds[reusedPID]
 b.mu.Unlock()
 if found{t.Fatal("actual process owner cleanup did not release its own mapping")}
 // Repeated cleanup must be idempotent and must not revoke a later
 // owner even when the original launch's channel has closed.
 close(older.exited)
 b.mu.Lock()
 b.cmds[reusedPID]=newer
 b.mu.Unlock()
 b.forgetTrackedProcess(reusedPID,older)
 b.mu.Lock()
 current=b.cmds[reusedPID]
 b.mu.Unlock()
 if current!=newer{t.Fatal("completed old launch reclaimed a newer PID")}
}
