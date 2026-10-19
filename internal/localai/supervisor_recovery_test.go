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
