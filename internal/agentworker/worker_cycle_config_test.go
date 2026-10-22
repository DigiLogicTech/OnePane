package agentworker

import (
 "errors"
 "testing"
)

// A control-plane config update must not change Worker routing/protocol or
// context limits midway through an already-running inference/proposal cycle.
func TestWorkerConfigurationCannotChangeDuringExecutionCycle(t *testing.T) {
 worker:=New(nil,nil,nil,"",nil,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 original:=worker.cfg
 updated:=DefaultConfig()
 updated.MaxSteps=32
 updated.ContextMaxBytes=128<<10
 worker.workerCycleMu.Lock()
 changed:=make(chan error,1)
 go func(){changed<-worker.SetConfig(updated)}()
 err:=<-changed
 if !errors.Is(err,ErrWorkerCycleInProgress){
  t.Fatalf("live configuration change was not refused: %v",err)
 }
 if worker.cfg!=original{
  t.Fatalf("configuration was changed during active execution: %+v",worker.cfg)
 }
 worker.workerCycleMu.Unlock()
 if err:=worker.SetConfig(updated);err!=nil{
  t.Fatalf("idle configuration update was denied: %v",err)
 }
 if worker.cfg!=updated{t.Fatalf("approved settings were not applied: %+v",worker.cfg)}
 invalid:=updated
 invalid.MaxSteps=0
 if err:=worker.SetConfig(invalid);err==nil{
  t.Fatalf("invalid configuration accepted")
 }
 if worker.cfg!=updated{
  t.Fatalf("invalid update changed approved configuration: %+v",worker.cfg)
 }
}
