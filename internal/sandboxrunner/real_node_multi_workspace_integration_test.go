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

// TestRealRootlessWorkspaceIsolation extends the single-runtime smoke to
// prove two independent OCI Workspaces can run side by side, persist distinct
// files, and survive an independent stop. Requires the same explicitly
// approved, locally cached, immutable BusyBox-compatible image.
func TestRealRootlessWorkspaceIsolation(t *testing.T) {
 image:=strings.TrimSpace(os.Getenv("ONEPANE_SANDBOX_SMOKE_IMAGE"))
 if image=="" {t.Skip("physical Node test requires approved pre-pulled image")}
 if ok,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!ok {
  t.Fatal("image must use a full immutable OCI digest")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),4*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless{t.Fatalf("rootless OCI Node required: %v",err)}
 if _,err:=engine.InspectImage(ctx,image);err!=nil{t.Fatalf("approved image missing: %v",err)}
 runID:=fmt.Sprintf("opqa-pair-%d",time.Now().UnixNano())
 ids:=[]string{runID+"-world",runID+"-story"}
 roots:=[]string{filepath.Join(t.TempDir(),"world"),filepath.Join(t.TempDir(),"story")}
 const appID="builder"
 containerIDs:=make([]string,len(ids))
 for i:=range ids {
  if err:=os.MkdirAll(roots[i],0o700);err!=nil{t.Fatal(err)}
 }
 // Cleanup both exact generated names even if startup or an assertion fails.
 // Never prune the shared Node or delete any existing images/host directories.
 t.Cleanup(func(){
  cleanupCtx,stop:=context.WithTimeout(context.Background(),40*time.Second)
  defer stop()
  for _,id:=range ids {
   _,_,_=runCLI(cleanupCtx,profile.Executable,"rm","-f",containerName(id,appID))
   _,_,_=runCLI(cleanupCtx,profile.Executable,"network","rm",runtimeNetworkName(id))
  }
 })
 for i,id:=range ids {
  if _,err:=engine.EnsureNetwork(ctx,id,true);err!=nil{t.Fatalf("network %d: %v",i,err)}
  spec:=ContainerSpec{
   RuntimeID:id,ApplicationID:appID,Image:image,
   WorkspacePath:roots[i],Command:[]string{"sh","-c","sleep 210"},
   WorkingDir:"/workspace",NetworkInternal:true,
   Limits:ResourceLimits{CPUMillis:500,MemoryMB:256,PIDs:64},
  }
  if _,err:=engine.EnsureContainer(ctx,spec);err!=nil{t.Fatalf("container %d: %v",i,err)}
  state,err:=engine.InspectContainer(ctx,id,appID)
  if err!=nil||state.Status!="running"||!state.IsolationVerified||
   state.SpecHash!=specHash(spec)||state.ID==""||!exactWorkspaceMount(state,roots[i]) {
   t.Fatalf("Workspace %d lacks verified dedicated OCI mount: %+v %v",i,state,err)
  }
  containerIDs[i]=state.ID
 }
 for i,id:=range ids {
  value:=[]string{"world-only","story-only"}[i]
  command:=[]string{"sh","-c","printf "+value+" > /workspace/identity.txt && cat /workspace/identity.txt"}
  output,err:=engine.ExecContainer(ctx,id,appID,command)
  if err!=nil||output.ExitCode!=0||strings.TrimSpace(output.Stdout)!=value {
   t.Fatalf("Workspace %d command failed: %+v %v",i,output,err)
  }
  data,err:=os.ReadFile(filepath.Join(roots[i],"identity.txt"))
  if err!=nil||string(data)!=value {t.Fatalf("Workspace %d file mismatch: %q %v",i,data,err)}
 }
 // A second Workspace receives neither the first one's writable mount nor
 // its file. Independent networks must also have different engine identities.
 a,err:=engine.InspectNetwork(ctx,ids[0]);if err!=nil{t.Fatal(err)}
 b,err:=engine.InspectNetwork(ctx,ids[1]);if err!=nil{t.Fatal(err)}
 if a.Name==b.Name||(a.ID!=""&&b.ID!=""&&a.ID==b.ID)||!a.Internal||!b.Internal||
  a.RuntimeID!=ids[0]||b.RuntimeID!=ids[1]{
  t.Fatalf("Workspaces share OCI network identity: world=%+v story=%+v",a,b)
 }
 if _,err:=engine.StopRuntime(ctx,ids[0]);err!=nil{t.Fatalf("stopping World: %v",err)}
 stopped,err:=engine.InspectContainer(ctx,ids[0],appID)
 if err!=nil||stopped.Status=="running"||stopped.Status=="restarting"{
  t.Fatalf("World stop not independently confirmed: %+v %v",stopped,err)
 }
 alive,err:=engine.InspectContainer(ctx,ids[1],appID)
 if err!=nil||alive.Status!="running"||!alive.IsolationVerified||
  !exactWorkspaceMount(alive,roots[1]){
  t.Fatalf("stopping World disrupted Story: %+v %v",alive,err)
 }
 observed,err:=engine.ExecContainer(ctx,ids[1],appID,[]string{"sh","-c","cat /workspace/identity.txt"})
 if err!=nil||observed.ExitCode!=0||strings.TrimSpace(observed.Stdout)!="story-only"{
  t.Fatalf("Story not executable after World stopped: %+v %v",observed,err)
 }
 // Restart the exact approved stopped World container and verify that its
 // original OCI identity, dedicated mount and durable files are retained.
 worldSpec:=ContainerSpec{
  RuntimeID:ids[0],ApplicationID:appID,Image:image,WorkspacePath:roots[0],
  Command:[]string{"sh","-c","sleep 210"},WorkingDir:"/workspace",
  NetworkInternal:true,Limits:ResourceLimits{CPUMillis:500,MemoryMB:256,PIDs:64},
 }
 restarted,err:=engine.EnsureContainer(ctx,worldSpec)
 if err!=nil||restarted.Status!="running"||!restarted.IsolationVerified||
  restarted.ID!=containerIDs[0]||!exactWorkspaceMount(restarted,roots[0])||
  restarted.SpecHash!=specHash(worldSpec){
  t.Fatalf("stopped World failed verified same-spec restart: %+v %v",restarted,err)
 }
 resumed,err:=engine.ExecContainer(ctx,ids[0],appID,[]string{"sh","-c","cat /workspace/identity.txt"})
 if err!=nil||resumed.ExitCode!=0||strings.TrimSpace(resumed.Stdout)!="world-only"{
  t.Fatalf("World persistent data missing after restart: %+v %v",resumed,err)
 }
 // A versioned toolchain rebuild changes only World's OCI process/identity.
 // An owned verified container can be replaced without deleting World's
 // writable root or evicting Story. No registry pulls or host-wide pruning.
 if _,err:=engine.StopRuntime(ctx,ids[0]);err!=nil{t.Fatalf("re-stop World before rebuild: %v",err)}
 worldSpec.Command=[]string{"sh","-c","sleep 180"}
 rebuilt,err:=engine.EnsureContainer(ctx,worldSpec)
 if err!=nil||rebuilt.Status!="running"||!rebuilt.IsolationVerified||
  (rebuilt.ID==""||rebuilt.ID==containerIDs[0])||!exactWorkspaceMount(rebuilt,roots[0])||
  rebuilt.SpecHash!=specHash(worldSpec){
  t.Fatalf("World OCI rebuild did not replace exactly its owned container: %+v %v",rebuilt,err)
 }
 afterRebuild,err:=engine.ExecContainer(ctx,ids[0],appID,[]string{"sh","-c","cat /workspace/identity.txt"})
 if err!=nil||afterRebuild.ExitCode!=0||strings.TrimSpace(afterRebuild.Stdout)!="world-only"{
  t.Fatalf("World persistence failed across OCI rebuild: %+v %v",afterRebuild,err)
 }
 untouched,err:=engine.InspectContainer(ctx,ids[1],appID)
 if err!=nil||untouched.ID!=containerIDs[1]||untouched.Status!="running"||
  !untouched.IsolationVerified||!exactWorkspaceMount(untouched,roots[1]){
  t.Fatalf("World restart/rebuild disrupted Story OCI identity/isolation: %+v %v",untouched,err)
 }
 storyReadback,err:=engine.ExecContainer(ctx,ids[1],appID,[]string{"sh","-c","cat /workspace/identity.txt"})
 if err!=nil||storyReadback.ExitCode!=0||strings.TrimSpace(storyReadback.Stdout)!="story-only"{
  t.Fatalf("Story file changed after World rebuild: %+v %v",storyReadback,err)
 }
 refreshedStoryNetwork,err:=engine.InspectNetwork(ctx,ids[1])
 if err!=nil||refreshedStoryNetwork.Name!=b.Name||
  (b.ID!=""&&refreshedStoryNetwork.ID!=b.ID)||
  !refreshedStoryNetwork.Internal||refreshedStoryNetwork.RuntimeID!=ids[1]{
  t.Fatalf("World OCI rebuild disrupted Story private network: %+v %v",refreshedStoryNetwork,err)
 }
 if _,err:=engine.StopRuntime(ctx,ids[0]);err!=nil{t.Fatal(err)}
 if _,err:=engine.StopRuntime(ctx,ids[1]);err!=nil{t.Fatal(err)}
 for i:=range ids {
  data,err:=os.ReadFile(filepath.Join(roots[i],"identity.txt"))
  expected:=[]string{"world-only","story-only"}[i]
  if err!=nil||string(data)!=expected{t.Fatalf("Workspace %d lost independent persistence: %q %v",i,data,err)}
 }
 t.Log("Verified: independent networks and writable roots; World stop/restart and versioned OCI rebuild preserve World files and leave Story executing")
}
