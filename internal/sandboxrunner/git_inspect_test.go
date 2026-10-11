package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestWorkspaceGitInspectHasFixedNoShellCommands(t *testing.T){
 for _,action:=range []string{"status","diff","log","tracked_files"}{
  t.Run(action,func(t *testing.T){
   args,err:=gitInspectCommand(action)
   if err!=nil||len(args)<8||args[0]!="git"{t.Fatalf("invalid argv: %q %v",args,err)}
   joined:=strings.Join(args," ")
   if !strings.Contains(joined,"-C /workspace")||
    strings.Contains(joined,"sh -c")||strings.Contains(joined,"--exec-path")||
    strings.Contains(joined,"--git-dir") {
    t.Fatalf("Git inspect escaped fixed Workspace command plan: %q",args)
   }
  })
 }
 for _,bad:=range []string{
  ""," status","status ","status; rm -rf /","push","commit","clone",
  "diff --output=/etc/hosts","--help","status\nmalicious","../status",
 }{
  if args,err:=gitInspectCommand(bad);err==nil {
   t.Fatalf("unapproved Git action allowed: %q => %q",bad,args)
  }
 }
}

func TestGitInspectionRunsOnlyInVerifiedManagedSandbox(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 workspace,ok,err:=managedWorkspacePath(adapter.dataDir,"runtime-world",true)
 if err!=nil||!ok{t.Fatalf("prepare Workspace: %v",err)}
 eng.inspectMount=workspace
 input:=json.RawMessage(`{"runtime_id":"runtime-world","application_id":"devtool","action":"status"}`)
 v,err:=adapter.Invoke(context.Background(),tool.AdapterRequest{
  ToolID:ToolAppGitInspect,Input:input,
 })
 if err!=nil{t.Fatal(err)}
 var response struct{
  Succeeded bool `json:"succeeded"`
  Action string `json:"action"`
  Result ExecResult `json:"result"`
 }
 if err:=json.Unmarshal(v.Result,&response);err!=nil{t.Fatal(err)}
 if !response.Succeeded||response.Action!="status"||
  !strings.Contains(response.Result.Stdout,"-C /workspace status")||eng.execs!=1{
  t.Fatalf("Git status did not execute the closed command plan: %+v execs=%d",response,eng.execs)
 }
 eng.execExitCode=128
 eng.execStderr="fatal: not a git repository"
 v,err=adapter.Invoke(context.Background(),tool.AdapterRequest{
  ToolID:ToolAppGitInspect,Input:input,
 })
 if err!=nil{t.Fatalf("Git missing repo must be returned to agent as diagnostics: %v",err)}
 if err:=json.Unmarshal(v.Result,&response);err!=nil{t.Fatal(err)}
 if response.Succeeded||response.Result.ExitCode!=128||
  response.Result.Stderr!="fatal: not a git repository"{
  t.Fatalf("Git diagnostics were lost: %+v",response)
 }
 before:=eng.execs
 for _,raw:=range []json.RawMessage{
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"devtool","action":"push"}`),
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"devtool","action":"status","command":["rm","-rf","/workspace"]}`),
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"devtool","action":"diff","timeout_seconds":121}`),
 }{
  if _,err=adapter.Invoke(context.Background(),tool.AdapterRequest{
   ToolID:ToolAppGitInspect,Input:raw,
  });!errors.Is(err,ErrInvalidInput){
   t.Fatalf("injected Git command was not denied: %s %v",raw,err)
  }
 }
 if eng.execs!=before{t.Fatal("rejected Git command executed")}
 eng.extraBind=true
 if _,err=adapter.Invoke(context.Background(),tool.AdapterRequest{
  ToolID:ToolAppGitInspect,Input:input,
 });!errors.Is(err,ErrInvalidInput){
  t.Fatalf("extra host mount accepted: %v",err)
 }
 if eng.execs!=before{t.Fatal("host-mounted Git command executed")}
}

func TestGitInspectionRejectsNonRootlessEngine(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"docker",Rootless:false}}
 adapter:=NewAdapter(t.TempDir(),eng)
 _,err:=adapter.Invoke(context.Background(),tool.AdapterRequest{
  ToolID:ToolAppGitInspect,Input:json.RawMessage(`{"runtime_id":"r","application_id":"a","action":"status"}`),
 })
 if !errors.Is(err,ErrRootlessRequired){t.Fatalf("nonrootless Git allowed: %v",err)}
 if eng.execs!=0{t.Fatal("nonrootless Git reached execution")}
}
