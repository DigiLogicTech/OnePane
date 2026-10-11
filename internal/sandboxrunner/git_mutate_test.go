package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestWorkspaceGitMutationFixedSafeArgumentPlans(t *testing.T){
 for _,tc:=range []struct{action,message,last string}{
  {"init","","--initial-branch=main"},
  {"stage_all","", "."},
  {"commit","world pass 1","world pass 1"},
 }{
  argv,err:=gitMutateCommand(tc.action,tc.message)
  if err!=nil||argv[0]!="git"||argv[len(argv)-1]!=tc.last{
   t.Fatalf("invalid Git mutation plan %s: %v %v",tc.action,argv,err)
  }
  text:=strings.Join(argv," ")
  if strings.Contains(text,"sh -c")||
   !strings.Contains(text,"-C /workspace")||
   !strings.Contains(text,"core.hooksPath=/dev/null")||
   !strings.Contains(text,"commit.gpgsign=false")||
   strings.Contains(text," push "){
   t.Fatalf("Git mutation plan may escape controlled Workspace: %s",text)
  }
 }
 for _,bad:=range []struct{action,message string}{
  {"",""},{"push",""},{"clone",""},{"reset",""},{"amend",""},
  {"init","unexpected"},{"stage_all","unexpected"},
  {"commit",""},{"commit",strings.Repeat("x",513)},
  {"commit","message\nsecond line"},{"commit","null\x00byte"},
 }{
  if plan,err:=gitMutateCommand(bad.action,bad.message);err==nil{
   t.Fatalf("unsafe git mutation request was accepted %q/%q => %q",bad.action,bad.message,plan)
  }
 }
 // A shell-looking message must stay a single Git -m argv value.
 msg:="update world; echo hacked"
 plan,err:=gitMutateCommand("commit",msg)
 if err!=nil||plan[len(plan)-1]!=msg||plan[len(plan)-2]!="-m"{
  t.Fatalf("commit text escaped its argv argument: %v %v",plan,err)
 }
}

func TestGitMutationRequiresVerifiedRegisteredSandbox(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 workspace,ok,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=workspace
 req:=tool.AdapterRequest{ToolID:ToolAppGitMutate,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"git-tool","action":"commit","message":"World assets v1"}`)}
 result,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 var payload struct{
  Succeeded bool `json:"succeeded"`
  Action string `json:"action"`
  Result ExecResult `json:"result"`
 }
 if err:=json.Unmarshal(result.Result,&payload);err!=nil{t.Fatal(err)}
 if !payload.Succeeded||payload.Action!="commit"||
  !strings.Contains(payload.Result.Stdout,"commit --no-verify -m World assets v1")||
  eng.execs!=1{
  t.Fatalf("expected exact controlled Git commit in sandbox: %+v, execs=%d",payload,eng.execs)
 }
 eng.execExitCode=1
 eng.execStderr="nothing to commit"
 result,err=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 if err:=json.Unmarshal(result.Result,&payload);err!=nil{t.Fatal(err)}
 if payload.Succeeded||payload.Result.ExitCode!=1||payload.Result.Stderr!="nothing to commit"{
  t.Fatalf("Git failure falsely claimed success: %+v",payload)
 }
 before:=eng.execs
 for _,raw:=range []json.RawMessage{
  json.RawMessage(`{"runtime_id":"world","application_id":"git-tool","action":"push"}`),
  json.RawMessage(`{"runtime_id":"world","application_id":"git-tool","action":"commit"}`),
  json.RawMessage(`{"runtime_id":"world","application_id":"git-tool","action":"init","command":["sh","-c","touch /host"]}`),
  json.RawMessage(`{"runtime_id":"world","application_id":"git-tool","action":"stage_all","timeout_seconds":121}`),
 }{
  _,err:=adapter.Invoke(context.Background(),tool.AdapterRequest{ToolID:ToolAppGitMutate,Input:raw})
  if !errors.Is(err,ErrInvalidInput){t.Fatalf("Git mutation accepted illegal command %s: %v",raw,err)}
 }
 eng.extraBind=true
 _,err=adapter.Invoke(context.Background(),req)
 if !errors.Is(err,ErrInvalidInput){t.Fatalf("Git write with host bind was accepted: %v",err)}
 if eng.execs!=before{t.Fatalf("forbidden git operation reached container: %d=>%d",before,eng.execs)}
}
