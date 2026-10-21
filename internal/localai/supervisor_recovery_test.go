package localai

import(
 "os"
 "strings"
 "testing"
)

// Recovery must not retain a hard-coded llama.cpp command-line admission
// while Start, Acquire and Stop enforce backend-aware process identity.
func TestManagedRuntimeRecoveryUsesBackendAwareProcessIdentityBeforeHealth(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 start:=strings.Index(source,"func (s *RuntimeSupervisor) Recover(")
 if start<0{t.Fatal("runtime recovery path unavailable")}
 section:=source[start:]
 if end:=strings.Index(section,"\nfunc ");end>0{section=section[:end]}
 verify:=strings.Index(section,"s.VerifyIdentity(ctx, inst)")
 probe:=strings.Index(section,"s.Health(ctx, inst)")
 if verify<0||probe<0||verify>=probe{
  t.Fatal("recovering runtime health before validated executable/model/port identity")
 }
 if strings.Contains(section,`argsContainPair(ident.Args, "-m"`){
  t.Fatal("Colibri process would be orphaned by llama-only recovery policy")
 }
 if !strings.Contains(section,"inst.PID==x.pid")||
  !strings.Contains(section,"inst.Revision==x.revision"){
  t.Fatal("recovery did not bind read identity to original persisted instance snapshot")
 }
}


func TestStopRechecksManagedPIDIdentityBeforeForceKill(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 start:=strings.Index(source,"func (s *RuntimeSupervisor) stopWithActivityLock(")
 if start<0{t.Fatal("managed stop missing")}
 section:=source[start:]
 if end:=strings.Index(section,"\nfunc ");end>0{section=section[:end]}
 lastVerify:=strings.LastIndex(section,"s.VerifyIdentity(ctx, inst)")
 kill:=strings.Index(section,"s.processes.Signal(inst.PID, syscall.SIGKILL)")
 term:=strings.Index(section,"s.processes.Signal(inst.PID, syscall.SIGTERM)")
 if term<0||lastVerify<=term||kill<=lastVerify{
  t.Fatal("forced kill could target a recycled PID without post-drain identity verification")
 }
 if !strings.Contains(section[term:kill],"markOrphaned("){
  t.Fatal("uncertain PID ownership did not persist conservative orphan state")
 }
}

func TestManagedStopRechecksPIDAfterDrainingPersistenceBeforeSIGTERM(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 from:=strings.Index(source,"func (s *RuntimeSupervisor) stopWithActivityLock(")
 to:=strings.Index(source,"// Recover snapshots candidate rows",from)
 if from<0||to<=from{t.Fatal("managed stop source boundary missing")}
 body:=source[from:to]
 first:=strings.Index(body,"s.VerifyIdentity(ctx, inst)")
 draining:=strings.Index(body,"status='draining'")
 second:=strings.Index(body[draining:],"s.VerifyIdentity(ctx, inst)")
 term:=strings.Index(body,"s.processes.Signal(inst.PID, syscall.SIGTERM)")
 if first<0||draining<=first||second<0||draining+second>=term{
  t.Fatal("SIGTERM can target PID recycled during draining-state SQLite write")
 }
 guard:=body[draining+second:term]
 if !strings.Contains(guard,"markOrphaned("){
  t.Fatal("post-drain PID identity failure must preserve reconciliation evidence")
 }
}
