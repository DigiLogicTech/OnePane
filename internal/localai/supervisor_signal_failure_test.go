package localai

import(
 "os"
 "strings"
 "testing"
)

func TestStopFailedSignalsAlwaysRequireOperatorReconciliation(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 all:=string(raw)
 start:=strings.Index(all,"func (s *RuntimeSupervisor) Stop(")
 if start<0{t.Fatal("managed Stop missing")}
 section:=all[start:]
 if end:=strings.Index(section,"\nfunc ");end>0{section=section[:end]}
 for _,signal:=range []string{"SIGTERM","SIGKILL"}{
  index:=strings.Index(section,"s.processes.Signal(inst.PID, syscall."+signal+")")
  if index<0{t.Fatalf("process %s signal missing",signal)}
  after:=section[index:]
  next:=strings.Index(after,"\n\t\t}")
  if next<0{t.Fatalf("process %s failure branch cannot be identified",signal)}
  branch:=after[:next]
  if !strings.Contains(branch,"markOrphaned(")||
   !strings.Contains(branch,"operator reconciliation"){
   t.Fatalf("failed %s left unverified process in draining state",signal)
  }
 }
}
