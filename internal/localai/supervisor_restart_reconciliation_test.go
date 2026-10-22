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
  {"failed with unverified NULL PID",RuntimeFailed,0,false,true},
  {"stopped with unverified NULL PID",RuntimeStopped,0,false,true},
  {"invalid PID",RuntimeFailed,1,true,true},
  {"healthy missing PID",RuntimeHealthy,0,false,true},
  {"busy missing PID",RuntimeBusy,0,false,true},
  {"starting missing PID",RuntimeStarting,0,false,true},
  {"healthy parent gone",RuntimeHealthy,44,false,true},
  {"busy parent gone",RuntimeBusy,44,false,true},
  {"starting parent gone",RuntimeStarting,44,false,true},
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

// An exact durable pre-spawn failure is independently retryable; a generic
// failed status after a crash cannot prove no engine was created.
func TestManagedRestartNoChildFailureProofIsRequired(t *testing.T){
 reason:=managedNoChildFailureReason
 valid:=RuntimeInstance{Status:RuntimeFailed,PID:0,FailureReason:&reason}
 if !confirmedUnlaunchedModelFailure(valid) || requiresOperatorReconciliationBeforeRestart(valid,false){
  t.Fatal("safe pre-spawn failure became permanently blocked")
 }
 for _,inst:=range []RuntimeInstance{
  {Status:RuntimeFailed,PID:0},
  {Status:RuntimeFailed,PID:1,FailureReason:&reason},
  {Status:RuntimeFailed,PID:0,pidRecorded:true,FailureReason:&reason},
  {Status:RuntimeFailed,PID:0,FailureReason:&reason,StartedAt:func() *int64 {v:=int64(42);return &v}()},
 }{
  if confirmedUnlaunchedModelFailure(inst) || !requiresOperatorReconciliationBeforeRestart(inst,false){
   t.Fatalf("unproven failed PID became retryable: %+v",inst)
  }
 }
}

func TestManagedStartupChecksUnknownBusyBeforeProcessReuse(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 code:=string(raw)
 start:=strings.Index(code,"func (s *RuntimeSupervisor) startLocked(")
 if start<0{t.Fatal("managed startup unavailable")}
 section:=code[start:]
 if end:=strings.Index(section,"\nfunc ");end>0{section=section[:end]}
 busy:=strings.Index(section,"if old.Status==RuntimeBusy {")
 lock:=strings.Index(section[busy:],"s.activityMu.Lock()")
 evidence:=strings.Index(section[busy:],"s.activeRequests[deploymentID]>0")
 denial:=strings.Index(section[busy:],"unknown persisted busy inference outcome")
 verify:=strings.Index(section,"s.VerifyIdentity(ctx,old)")
 if busy<0||lock<0||evidence<lock||denial<evidence||verify<=busy+denial{
  t.Fatal("persisted busy inference can be reused before verifying active request lease")
 }
 if !strings.Contains(section[busy:verify],"s.activityMu.Unlock()"){
  t.Fatal("startLocked must release activityMu before VerifyIdentity")
 }
}

func TestManagedStoppedNoChildProofIsRequiredBeforeRestart(t *testing.T){
 reason:=managedNoChildFailureReason
 for _,tc:=range []struct{
  name string;inst RuntimeInstance;deny bool
 }{
  {"unknown stopped",RuntimeInstance{Status:RuntimeStopped},true},
  {"unrelated failure",RuntimeInstance{Status:RuntimeStopped,FailureReason:func()*string{v:="unrelated";return &v}()},true},
  {"recorded zero",RuntimeInstance{Status:RuntimeStopped,PID:0,pidRecorded:true,FailureReason:&reason},true},
  {"started model",RuntimeInstance{Status:RuntimeStopped,FailureReason:&reason,StartedAt:func()*int64{n:=int64(123);return &n}()},true},
  {"proven no-child stopped",RuntimeInstance{Status:RuntimeStopped,FailureReason:&reason},false},
 }{
  t.Run(tc.name,func(t *testing.T){
   if got:=requiresOperatorReconciliationBeforeRestart(tc.inst,false);got!=tc.deny{
    t.Fatalf("stopped evidence %+v: deny=%t want=%t",tc.inst,got,tc.deny)
   }
  })
 }
}
