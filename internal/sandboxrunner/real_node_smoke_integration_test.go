//go:build integration

package sandboxrunner

import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
 "time"
)

// TestRealRootlessNodeSmoke is intentionally opt-in. It must run on a compatible
// rootless Linux Node with a pre-pulled, digest-pinned BusyBox-compatible OCI
// image. It exercises the actual CLI backend, not fake in-memory execution.
func TestRealRootlessNodeSmoke(t *testing.T){
 image:=strings.TrimSpace(os.Getenv("ONEPANE_SANDBOX_SMOKE_IMAGE"))
 if image==""{
  t.Skip("set ONEPANE_SANDBOX_SMOKE_IMAGE to a pre-pulled sha256-pinned image on a rootless Node")
 }
 if ok,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!ok{
  t.Fatal("ONEPANE_SANDBOX_SMOKE_IMAGE must be pinned by a full OCI sha256 digest")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless{t.Fatalf("real rootless Podman/Docker Node required: %v",err)}
 if _,err:=engine.InspectImage(ctx,image);err!=nil{
  t.Fatalf("image is not present locally; pre-pull explicitly for QA: %v",err)
 }
 runtimeID:=fmt.Sprintf("opqa-%d",time.Now().UnixNano())
 appID:="smoke"
 root:=t.TempDir()
 workspace:=filepath.Join(root,"workspace")
 if err:=os.Mkdir(workspace,0o700);err!=nil{t.Fatal(err)}
 // Avoid deleting anything outside the generated runtime identity.
 t.Cleanup(func(){
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
  defer cancel()
  _,_,_=runCLI(cleanupCtx,profile.Executable,"rm","-f",containerName(runtimeID,appID))
  _,_,_=runCLI(cleanupCtx,profile.Executable,"network","rm",runtimeNetworkName(runtimeID))
 })
 if _,err:=engine.EnsureNetwork(ctx,runtimeID,true);err!=nil{t.Fatal(err)}
 spec:=ContainerSpec{
  RuntimeID:runtimeID,ApplicationID:appID,Image:image,WorkspacePath:workspace,
  Command:[]string{"sh","-c","sleep 120"},WorkingDir:"/workspace",
  Limits:ResourceLimits{CPUMillis:500,MemoryMB:256,PIDs:64},NetworkInternal:true,
 }
 if _,err:=engine.EnsureContainer(ctx,spec);err!=nil{t.Fatalf("real OCI launch failed: %v",err)}
 observed,err:=engine.InspectContainer(ctx,runtimeID,appID)
 if err!=nil{t.Fatalf("OCI independent inspect: %v",err)}
 if observed.Status!="running"||!observed.IsolationVerified||
  observed.RuntimeID!=runtimeID||observed.ApplicationID!=appID||
  observed.SpecHash!=specHash(spec)||!exactWorkspaceMount(observed,workspace) {
  t.Fatalf("running container failed real isolation verification: %+v",observed)
 }
 output,err:=engine.ExecContainer(ctx,runtimeID,appID,[]string{
  "sh","-c","printf 'onepane-verified' > /workspace/probe.txt && cat /workspace/probe.txt",
 })
 if err!=nil||strings.TrimSpace(output.Stdout)!="onepane-verified"{
  t.Fatalf("verified sandbox command failed: output=%+v err=%v",output,err)
 }
 raw,err:=os.ReadFile(filepath.Join(workspace,"probe.txt"))
 if err!=nil||string(raw)!="onepane-verified"{
  t.Fatalf("managed writable root not persisted: %q err=%v",raw,err)
 }
 if _,err:=engine.StopRuntime(ctx,runtimeID);err!=nil{
  t.Fatalf("real runtime stop failed: %v",err)
 }
 after,err:=engine.InspectContainer(ctx,runtimeID,appID)
 if err!=nil||after.Status=="running"||after.Status=="restarting"{
  t.Fatalf("real runtime stop not independently confirmed: %+v err=%v",after,err)
 }
}
