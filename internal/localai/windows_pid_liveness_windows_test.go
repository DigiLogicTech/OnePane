//go:build windows

package localai

import (
 "errors"
 "os"
 "os/exec"
 "testing"
 "time"

 "golang.org/x/sys/windows"
)

func TestWindowsUntrackedPIDProbeIsConservative(t *testing.T) {
 if windowsUntrackedPIDAlive(0)||windowsUntrackedPIDAlive(1){
  t.Fatal("invalid PID was considered alive")
 }
 if windowsUntrackedProcessErrorIsOccupied(windows.ERROR_INVALID_PARAMETER){
  t.Fatal("nonexistent process was treated as an occupant")
 }
 if !windowsUntrackedProcessErrorIsOccupied(windows.ERROR_ACCESS_DENIED){
  t.Fatal("uninspectable process was incorrectly declared dead")
 }
 if !windowsUntrackedProcessErrorIsOccupied(errors.New("simulated handle/query failure")){
  t.Fatal("unexpected Windows process error was treated as proof of exit")
 }
}

func TestWindowsUntrackedPIDLifecycle(t *testing.T) {
 if os.Getenv("ONEPANE_UNTRACKED_PID_TEST_CHILD")=="1"{
  time.Sleep(20*time.Second)
  return
 }
 // The child process is deliberately NOT registered in OSProcessBackend.
 // It models a persisted PID after OnePane itself has restarted.
 child:=exec.Command(os.Args[0],"-test.run=^TestWindowsUntrackedPIDLifecycle$")
 child.Env=append(os.Environ(),"ONEPANE_UNTRACKED_PID_TEST_CHILD=1")
 if err:=child.Start();err!=nil{t.Fatal(err)}
 exited:=false
 defer func(){
  if !exited{
   _=child.Process.Kill()
   _=child.Wait()
  }
 }()
 backend:=NewOSProcessBackend()
 pid:=child.Process.Pid
 if !backend.Alive(pid){
  t.Fatal("actual running Windows child was hidden because it was not tracked in memory")
 }
 if _,err:=backend.Identity(pid);err==nil{
  t.Fatal("untracked PID liveness incorrectly authorised CLI/process ownership")
 }
 // A live foreign/untracked child is a valid liveness occupant, NOT a
 // process OnePane can send to taskkill /T /F. This process belongs to
 // this test and will be terminated only through its original os.Process.
 if err:=backend.Signal(pid,os.Kill);err==nil{
  t.Fatal("Windows model backend accepted a tree signal for an untracked PID")
 }
 if !backend.Alive(pid){
  t.Fatal("untracked test-owned process was terminated by unsafe taskkill")
 }
 if err:=child.Process.Kill();err!=nil{t.Fatal(err)}
 if err:=child.Wait();err==nil{
  t.Fatal("killed helper unexpectedly exited cleanly")
 }
 exited=true
 deadline:=time.Now().Add(2*time.Second)
 for backend.Alive(pid)&&time.Now().Before(deadline){
  time.Sleep(20*time.Millisecond)
 }
 if backend.Alive(pid){
  t.Fatal("reaped Windows test process still reported alive")
 }
}
