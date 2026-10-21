package localai

import (
 "os"
 "strings"
 "testing"
)

func TestManagedRuntimeRestartRequiresReconciliationForLiveNonRunningProcess(t *testing.T) {
 cases := []struct {
  name string
  status RuntimeInstanceStatus
  pid int
  alive, want bool
 }{
  {"orphaned process still alive",RuntimeOrphaned,44,true,true},
  {"failed launch left process alive",RuntimeFailed,44,true,true},
  {"stopped record but PID alive",RuntimeStopped,44,true,true},
  {"drain incomplete",RuntimeDraining,44,true,true},
  {"unknown status",RuntimeInstanceStatus("unknown"),44,true,true},
  {"orphan confirmed exited",RuntimeOrphaned,44,false,false},
  {"orphaned with unknown PID",RuntimeOrphaned,0,true,true},
  {"orphaned with unknown PID after restart",RuntimeOrphaned,0,false,true},
  {"draining without recorded PID",RuntimeDraining,0,false,true},
  {"failed confirmed unlaunched without PID",RuntimeFailed,0,false,false},
  {"invalid PID",RuntimeFailed,1,true,false},
  {"healthy verified separately",RuntimeHealthy,44,true,false},
  {"busy verified separately",RuntimeBusy,44,true,false},
  {"starting verified separately",RuntimeStarting,44,true,false},
 }
 for _,tc := range cases {
  t.Run(tc.name,func(t *testing.T) {
   inst:=RuntimeInstance{PID:tc.pid,Status:tc.status}
   got:=requiresOperatorReconciliationBeforeRestart(inst,tc.alive)
   if got!=tc.want {t.Fatalf("state=%s pid=%d alive=%v: blocked=%v want=%v",
    tc.status,tc.pid,tc.alive,got,tc.want)}
  })
 }
}

// Guard must precede any predecessor eviction, Colibri swap or process start.
// This is a source-order contract in addition to the behavioural state table.
func TestManagedRuntimeRestartGuardPrecedesCapacityAndProcessLaunch(t *testing.T) {
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 text:=string(raw)
 start:=strings.Index(text,"func (s *RuntimeSupervisor) startLocked(")
 if start<0{t.Fatal("startLocked unavailable")}
 scope:=text[start:]
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 check:=strings.Index(scope,"requiresOperatorReconciliationBeforeRestart(old, alive)")
 swap:=strings.Index(scope,"s.prepareColibriSwapLocked(")
 capacity:=strings.Index(scope,"s.ensureCapacity(")
 launch:=strings.Index(scope,"s.processes.Start(")
 if check<0||swap<=check||capacity<=check||launch<=capacity {
  t.Fatal("live orphan/failed process was not denied before swap, eviction and launch")
 }
}
