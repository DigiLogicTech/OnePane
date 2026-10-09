//go:build integration

package sandboxrunner

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
 "time"
)

// TestRealRootlessGodotWorldBuild is a separately approved, opt-in execution
// gate. A pre-pulled Godot 4 OCI image must contain sh and a `godot` binary.
// Godot imports and executes a tiny real scene entirely inside the bounded
// rootless Workspace, then produces a hash-verified output in its writable
// managed directory. This does not claim export-template packaging or a GUI.
func TestRealRootlessGodotWorldBuild(t *testing.T) {
 image:=strings.TrimSpace(os.Getenv("ONEPANE_GODOT_SMOKE_IMAGE"))
 if image=="" {t.Skip("set ONEPANE_GODOT_SMOKE_IMAGE for the approved rootless Godot smoke")}
 if ok,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!ok {
  t.Fatal("Godot image must have a full immutable sha256 reference")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless{t.Fatalf("real rootless engine required: %v",err)}
 if _,err:=engine.InspectImage(ctx,image);err!=nil {
  t.Fatalf("Godot image not pre-pulled under Node runner user: %v",err)
 }
 runID:=fmt.Sprintf("opqa-godot-%d",time.Now().UnixNano())
 const appID="godot"
 workspace:=filepath.Join(t.TempDir(),"workspace")
 if err:=os.MkdirAll(workspace,0o700);err!=nil{t.Fatal(err)}
 // A Godot 4 scene with a real GDScript. Output bytes are deterministic so
 // the host independently verifies that the engine executed the project.
 sources:=map[string]string{
  "project.godot":`config_version=5
[application]
config/name="OnePane Rootless QA"
run/main_scene="res://world.tscn"
[rendering]
renderer/rendering_method="gl_compatibility"
renderer/rendering_method.mobile="gl_compatibility"
`,
  "world.tscn":`[gd_scene load_steps=2 format=3]
[ext_resource type="Script" path="res://world.gd" id="1_script"]
[node name="World" type="Node2D"]
script = ExtResource("1_script")
`,
  "world.gd":`extends Node2D

func _ready() -> void:
    var output := FileAccess.open("res://world-build.txt", FileAccess.WRITE)
    if output == null:
        push_error("Unable to publish World build output")
        get_tree().quit(2)
        return
    output.store_string("OnePane Godot World build v1\n")
    output.close()
    print("ONEPANE_GODOT_WORLD_BUILD_OK")
    get_tree().quit(0)
`,
 }
 for name,content:=range sources {
  if err:=os.WriteFile(filepath.Join(workspace,name),[]byte(content),0o600);err!=nil{t.Fatal(err)}
 }
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),30*time.Second)
  defer done()
  _,_,_=runCLI(cleanupCtx,profile.Executable,"rm","-f",containerName(runID,appID))
  _,_,_=runCLI(cleanupCtx,profile.Executable,"network","rm",runtimeNetworkName(runID))
 })
 spec:=ContainerSpec{
  RuntimeID:runID,ApplicationID:appID,Image:image,WorkspacePath:workspace,
  Command:[]string{"sh","-c","sleep 270"},WorkingDir:"/workspace",
  NetworkInternal:true,Environment:map[string]string{"HOME":"/tmp"},
  Limits:ResourceLimits{CPUMillis:1000,MemoryMB:1024,PIDs:128},
 }
 if _,err:=engine.EnsureNetwork(ctx,runID,true);err!=nil{t.Fatal(err)}
 if _,err:=engine.EnsureContainer(ctx,spec);err!=nil{t.Fatalf("Godot container launch: %v",err)}
 state,err:=engine.InspectContainer(ctx,runID,appID)
 if err!=nil||state.Status!="running"||!state.IsolationVerified||
  state.SpecHash!=specHash(spec)||!exactWorkspaceMount(state,workspace) {
  t.Fatalf("Godot OCI isolation not verified: %+v %v",state,err)
 }
 // Import real scene/resources (no external registry access), then execute.
 for _,command:=range [][]string{
  {"sh","-c","godot --headless --path /workspace --editor --import"},
  {"sh","-c","godot --headless --path /workspace --quit-after 30"},
 } {
  observed,err:=engine.ExecContainer(ctx,runID,appID,command)
  if err!=nil||observed.ExitCode!=0{
   t.Fatalf("Godot import/build command failed: command=%v stdout=%q stderr=%q exit=%d err=%v",
    command,observed.Stdout,observed.Stderr,observed.ExitCode,err)
  }
 }
 const expected="OnePane Godot World build v1\n"
 data,err:=os.ReadFile(filepath.Join(workspace,"world-build.txt"))
 if err!=nil||string(data)!=expected{
  t.Fatalf("Godot did not produce expected Workspace build output: %q %v",data,err)
 }
 sum:=sha256.Sum256(data)
 digest:=hex.EncodeToString(sum[:])
 expectedSHA:=sha256.Sum256([]byte(expected))
 if digest!=hex.EncodeToString(expectedSHA[:]){
  t.Fatal("on-disk Godot build artifact SHA-256 mismatch")
 }
 if _,err:=engine.StopRuntime(ctx,runID);err!=nil{t.Fatal(err)}
 stopped,err:=engine.InspectContainer(ctx,runID,appID)
 if err!=nil||stopped.Status=="running"||stopped.Status=="restarting"{
  t.Fatalf("Godot runtime stop unverified: %+v %v",stopped,err)
 }
 t.Logf("Godot 4 scene executed in isolated rootless Workspace; verified artifact SHA-256: %s",digest)
}
