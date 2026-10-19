//go:build windows

package main

import (
 "os"
 "os/exec"
 "syscall"
 "testing"
 "time"
)

// Exercise the actual Windows kernel Job Object on a hosted runner, without
// installing a service, launching OnePane, or touching any operator device.
// A helper copy of the current Go test process simulates a long-lived worker.
func TestManagedBackendJobTerminatesChildWhenOwnerCloses(t *testing.T){
 if os.Getenv("ONEPANE_OWNED_JOB_TEST_CHILD")=="1" {
  time.Sleep(20*time.Second)
  return
 }
 job,err:=newOwnedBackendJob()
 if err!=nil{t.Fatal(err)}
 defer job.Close()
 child:=exec.Command(os.Args[0],"-test.run=^TestManagedBackendJobTerminatesChildWhenOwnerCloses$")
 child.Env=append(os.Environ(),"ONEPANE_OWNED_JOB_TEST_CHILD=1")
 child.SysProcAttr=&syscall.SysProcAttr{CreationFlags:0x08000000}
 if err:=child.Start();err!=nil{t.Fatal(err)}
 finished:=make(chan error,1)
 go func(){finished<-child.Wait()}()
 reaped:=false
 defer func(){
  if !reaped{
   _ = child.Process.Kill()
   select{
   case <-finished:
   case <-time.After(5*time.Second):
   }
  }
 }()
 if err:=job.Assign(child.Process.Pid);err!=nil{t.Fatalf("could not enroll test child in owned job: %v",err)}
 if err:=job.Close();err!=nil{t.Fatalf("job close failed: %v",err)}
 select{
 case <-finished:
  // Nonzero child exit is expected when the owned job closes.
  reaped=true
 case <-time.After(5*time.Second):
  t.Fatal("closing owned Windows Job Object did not terminate managed child")
 }
}

func TestManagedBackendJobRejectsInvalidPIDAndClosesIdempotently(t *testing.T){
 job,err:=newOwnedBackendJob()
 if err!=nil{t.Fatal(err)}
 if err:=job.Assign(0);err==nil{t.Fatal("assigned invalid PID")}
 if err:=job.Close();err!=nil{t.Fatal(err)}
 if err:=job.Close();err!=nil{t.Fatal("second close failed:",err)}
}
