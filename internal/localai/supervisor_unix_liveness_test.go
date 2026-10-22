package localai

import (
 "errors"
 "os"
 "syscall"
 "testing"
)

func TestUnixPIDProbeDeniedSignalDoesNotProveFreedMemory(t *testing.T){
 for _,tc:=range []struct{name string;err error;occupied bool}{
  {"signal succeeded",nil,true},
  {"permission denied",syscall.EPERM,true},
  {"unexpected signal failure",errors.New("could not inspect PID"),true},
  {"no such process",syscall.ESRCH,false},
  {"process already done",os.ErrProcessDone,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   if got:=unixPIDProbeMeansOccupied(tc.err);got!=tc.occupied{
    t.Fatalf("probe %v occupied=%v, want %v",tc.err,got,tc.occupied)
   }
  })
 }
}
