package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "reflect"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestGodotToolFixedCommandsAndRejectsCallerArguments(t *testing.T){
 for _,tc:=range []struct{action string;want []string}{
  {"import",[]string{"godot","--headless","--path","/workspace","--editor","--import"}},
  {"run",[]string{"godot","--headless","--path","/workspace","--quit-after","60"}},
 }{
  argv,err:=godotBuildCommand(tc.action)
  if err!=nil||!reflect.DeepEqual(argv,tc.want){t.Fatalf("unsafe %s command: %q %v",tc.action,argv,err)}
 }
 for _,bad:=range []string{"", "export","compile","run;rm -rf /","../host","test"," RUN","run\n","run --script /host"}{
  if args,err:=godotBuildCommand(bad);!errors.Is(err,ErrInvalidInput)||len(args)!=0{
   t.Fatalf("unapproved Godot command accepted: %q args=%q err=%v",bad,args,err)
  }
 }
 for _,bad:=range []string{
  `{"runtime_id":"world","application_id":"engine","action":"run","command":["sh","-c","id"]}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","image":"latest"}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","environment_bindings":{"HOME":"/host"}}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","workspace_id":"other"}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","path":"/host"}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","timeout_seconds":1,"other":"injection"}`,
  `{"runtime_id":"world","application_id":"engine"}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","timeout_seconds":3,"foo":null}`,
 }{
  if validGodotEnvelope(json.RawMessage(bad)){t.Fatalf("accepted unapproved Godot JSON schema: %s",bad)}
 }
}

func TestGodotToolUsesOnlyVerifiedOwnedRootlessWorkspace(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 root,ok,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=root
 req:=tool.AdapterRequest{ToolID:ToolAppGodotBuild,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"engine","action":"import","timeout_seconds":120}`)}
 out,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 var got struct {
  Action string `json:"action"`
  Succeeded bool `json:"succeeded"`
  ArtifactVerified bool `json:"artifact_verified"`
  Result ExecResult `json:"result"`
 }
 if err=json.Unmarshal(out.Result,&got);err!=nil{t.Fatal(err)}
 if !got.Succeeded||got.Action!="import"||got.ArtifactVerified||got.Result.ExitCode!=0||
  got.Result.Stdout!="godot --headless --path /workspace --editor --import"||eng.execs!=1{
  t.Fatalf("Godot command/result was not narrowly scoped: %+v execs=%d",got,eng.execs)
 }
 eng.execExitCode=2
 eng.execStderr="project.godot import error"
 req.Input=json.RawMessage(`{"runtime_id":"world","application_id":"engine","action":"run"}`)
 out,err=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 if err=json.Unmarshal(out.Result,&got);err!=nil{t.Fatal(err)}
 if got.Succeeded||got.ArtifactVerified||got.Result.ExitCode!=2||
  !strings.Contains(got.Result.Stdout,"--quit-after 60"){
  t.Fatalf("failed Godot scene incorrectly claimed verified success: %+v",got)
 }
 before:=eng.execs
 for _,bad:=range []string{
  `{"runtime_id":"world","application_id":"engine","action":"export"}`,
  `{"runtime_id":"world","application_id":"engine","action":"import","command":["sh","-c","id"]}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","image":"host"}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","timeout_seconds":3601}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","timeout_seconds":-1}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","environment_bindings":{"token":{"literal":"secret"}}}`,
  `{"runtime_id":"world","application_id":"engine","action":"run","resource_limits":{"pids":0}}`,
 }{
  if _,err:=adapter.Invoke(context.Background(),tool.AdapterRequest{ToolID:ToolAppGodotBuild,Input:json.RawMessage(bad)});!errors.Is(err,ErrInvalidInput){
   t.Fatalf("untrusted Godot action was executed or returned wrong error: %s %v",bad,err)
  }
 }
 if eng.execs!=before{t.Fatal("rejected Godot tool unexpectedly entered OCI exec")}
 eng.extraBind=true
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||eng.execs!=before{
  t.Fatalf("extra host bind not denied: %v",err)
 }
 eng.extraBind=false
 eng.inspectMount="/tmp/forged-host-mount"
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||eng.execs!=before{
  t.Fatalf("forged Workspace mount not denied: %v",err)
 }
 eng.inspectMount=root
 eng.profile.Rootless=false
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrRootlessRequired)||eng.execs!=before{
  t.Fatalf("rootful container was allowed to run Godot: %v",err)
 }
}

func TestGodotToolTimeoutReturnsObservableCancellation(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true},blockExec:true}
 adapter:=NewAdapter(t.TempDir(),eng)
 root,ok,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=root
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Millisecond)
 defer cancel()
 req:=tool.AdapterRequest{ToolID:ToolAppGodotBuild,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"engine","action":"run"}`)}
 _,err=adapter.Invoke(ctx,req)
 if !errors.Is(err,context.DeadlineExceeded){t.Fatalf("Godot timeout misreported as successful build: %v",err)}
}
