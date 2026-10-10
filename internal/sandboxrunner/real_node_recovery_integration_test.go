//go:build integration

package sandboxrunner

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
 "time"
)

const recoveryChildExitCode=86

func recoverySmokeSpec(runtimeID,workspace,image string)ContainerSpec{
 return ContainerSpec{
  RuntimeID:runtimeID,ApplicationID:"incarnation",Image:image,
  WorkspacePath:workspace,Command:[]string{"sh","-c","sleep 240"},
  WorkingDir:"/workspace",NetworkInternal:true,
  Limits:ResourceLimits{CPUMillis:500,MemoryMB:256,PIDs:64},
 }
}

// Runs as a *separate process*, not a helper goroutine in the parent process.
// The forced exit intentionally leaves a detached, isolated OCI container
// alive and has no time to run deferred shutdown handlers or cleanup.
func TestRealRootlessRecoveryIncarnationChild(t *testing.T){
 if os.Getenv("ONEPANE_RECOVERY_CHILD")!="1"{t.Skip("dedicated physical recovery subprocess only")}
 runtimeID:=os.Getenv("ONEPANE_RECOVERY_RUNTIME")
 workspace:=os.Getenv("ONEPANE_RECOVERY_WORKSPACE")
 image:=os.Getenv("ONEPANE_SANDBOX_SMOKE_IMAGE")
 if runtimeID==""||workspace==""||image==""{t.Fatal("missing explicit approved physical recovery fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 engine:=NewCLIEngine()
 if _,err:=engine.EnsureNetwork(ctx,runtimeID,true);err!=nil{t.Fatal(err)}
 spec:=recoverySmokeSpec(runtimeID,workspace,image)
 created,err:=engine.EnsureContainer(ctx,spec)
 if err!=nil||!created.IsolationVerified||created.Status!="running"{
  t.Fatalf("real isolated sandbox launch failed: %v",err)
 }
 outcome,err:=engine.ExecContainer(ctx,runtimeID,spec.ApplicationID,[]string{
  "sh","-c","printf 'once-only' > /workspace/recovered.txt",
 })
 if err!=nil||outcome.ExitCode!=0{t.Fatalf("original physical effect failed: %v",err)}
 // This must not be a normal test completion: it models an abruptly killed
 // OnePane control plane after the external command was dispatched.
 os.Exit(recoveryChildExitCode)
}

func TestRealRootlessProcessCrashAndOwnedOCIRecovery(t *testing.T){
 image:=strings.TrimSpace(os.Getenv("ONEPANE_SANDBOX_SMOKE_IMAGE"))
 if image==""{t.Skip("physical test is opt-in with approved pre-pulled digest")}
 if ok,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!ok{
  t.Fatal("only an explicitly approved immutable OCI image may be used")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),4*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless{t.Fatal("trusted rootless OCI Node required")}
 if _,err:=engine.InspectImage(ctx,image);err!=nil{t.Fatal("approved digest is not pre-pulled")}
 runtimeID:=fmt.Sprintf("opqa-recovery-%d",time.Now().UnixNano())
 workspace:=filepath.Join(t.TempDir(),"workspace")
 if err:=os.Mkdir(workspace,0o700);err!=nil{t.Fatal(err)}
 spec:=recoverySmokeSpec(runtimeID,workspace,image)
 t.Cleanup(func(){
  closeCtx,stop:=context.WithTimeout(context.Background(),35*time.Second)
  defer stop()
  _,_,_=runCLI(closeCtx,profile.Executable,"rm","-f",containerName(runtimeID,spec.ApplicationID))
  _,_,_=runCLI(closeCtx,profile.Executable,"network","rm",runtimeNetworkName(runtimeID))
 })
 process:=exec.CommandContext(ctx,os.Args[0],"-test.run=^TestRealRootlessRecoveryIncarnationChild$","-test.count=1")
 process.Env=append(os.Environ(),"ONEPANE_RECOVERY_CHILD=1",
  "ONEPANE_RECOVERY_RUNTIME="+runtimeID,"ONEPANE_RECOVERY_WORKSPACE="+workspace)
 if _,err=process.CombinedOutput();err==nil{
  t.Fatal("control-plane recovery probe did not simulate a hard process exit")
 }
 var exit *exec.ExitError
 if !errors.As(err,&exit)||exit.ExitCode()!=recoveryChildExitCode{
  t.Fatal("physical control-plane child exited unexpectedly before crash checkpoint")
 }
 persisted,err:=os.ReadFile(filepath.Join(workspace,"recovered.txt"))
 if err!=nil||string(persisted)!="once-only"{
  t.Fatalf("side effect was not persisted before control-plane exit: %v",err)
 }
 // A freshly constructed CLI control plane can observe and safely adopt
 // the *existing* owned OCI identity, without running a second command.
 recoveredEngine:=NewCLIEngine()
 observed,err:=recoveredEngine.InspectContainer(ctx,runtimeID,spec.ApplicationID)
 if err!=nil||observed.Status!="running"||!observed.IsolationVerified||
  observed.ID==""||observed.SpecHash!=specHash(spec)||
  !exactWorkspaceMount(observed,workspace){
  t.Fatalf("crashed process lost exact isolated OCI identity: %v",err)
 }
 adopted,err:=recoveredEngine.EnsureContainer(ctx,spec)
 if err!=nil||adopted.ID!=observed.ID||adopted.Status!="running"||
  !adopted.IsolationVerified{
  t.Fatalf("fresh control plane replaced or lost original runtime: %v",err)
 }
 persisted,err=os.ReadFile(filepath.Join(workspace,"recovered.txt"))
 if err!=nil||string(persisted)!="once-only"{
  t.Fatalf("external command replayed or workspace data lost: %v",err)
 }
 stopped,err:=recoveredEngine.StopRuntime(ctx,runtimeID)
 if err!=nil||len(stopped)!=1||stopped[0].ID!=observed.ID||
  stopped[0].Status=="running"{
  t.Fatalf("crash-recovered runtime was not safely stopped by immutable ID: %v",err)
 }
 after,err:=recoveredEngine.InspectContainer(ctx,runtimeID,spec.ApplicationID)
 if err!=nil||after.ID!=observed.ID||after.Status=="running"{
  t.Fatalf("independent stopped-postcondition missing: %v",err)
 }
 t.Log("Verified: hard control-plane process exit preserves rootless OCI identity, Workspace file and once-only command; fresh process adopts and stops only its owned container")
}
