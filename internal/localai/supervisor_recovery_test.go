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
