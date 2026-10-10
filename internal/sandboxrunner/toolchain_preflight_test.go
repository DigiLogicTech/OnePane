package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestToolchainPreflightStrictRequirementsAndOrder(t *testing.T){
 cases:=[]struct{json string;valid bool}{
  {`{"runtime_id":"world","application_id":"build","required":["my-unknown-compiler","go","python3"]}`,true},
  {`{"runtime_id":"world","application_id":"build","required":["go"]}`,true},
  {`{"runtime_id":"world","application_id":"build","required":[]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":null}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go","go"]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["../../../host"]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["sh; cat /etc/passwd"]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["."]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":[".."]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"command":["id"]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"image":"mutable:latest"}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"environment":{"TOKEN":"secret"}}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"workspace_id":"story"}`,false},
  {`{"runtime_id":"../host","application_id":"build","required":["go"]}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"timeout_seconds":1000}`,false},
  {`{"runtime_id":"world","application_id":"build","required":["go"],"action":"install"}`,false},
  {`{"runtime_id":"world","application_id":"build"}`,false},
 }
 for _,tt:=range cases{
  parsed,err:=decodeToolchainRequirements(json.RawMessage(tt.json))
  if (err==nil)!=tt.valid{t.Fatalf("unsafe admission for %s: %+v %v",tt.json,parsed,err)}
 }
 a,err:=decodeToolchainRequirements(json.RawMessage(`{"runtime_id":"world","application_id":"build","required":["python3","go"]}`))
 if err!=nil{t.Fatal(err)}
 b,err:=decodeToolchainRequirements(json.RawMessage(`{"application_id":"build","required":["go","python3"],"runtime_id":"world"}`))
 if err!=nil{t.Fatal(err)}
 if fingerprintToolchainRequirements(a.Required)!=fingerprintToolchainRequirements(b.Required){
  t.Fatal("same prereqs in different input orders yield different cache/evidence identity")
 }
 if strings.Join(a.Required,",")!="go,python3"{t.Fatalf("preflight requirement list not canonical: %q",a.Required)}
 if command:=workspaceToolchainPreflightCommand(a);len(command)!=6||command[0]!="sh"||
  command[1]!="-c"||command[3]!="onepane-preflight"||
  strings.Join(command[4:],",")!="go,python3"||
  strings.Contains(command[2],"eval")||strings.Contains(command[2],"curl")||
  strings.Contains(command[2],"wget")||strings.Contains(command[2],"which"){
  t.Fatalf("preflight passes untrusted shell source: %q",command)
 }
 over:=make([]string,maxToolchainRequirements+1)
 for i:=range over{over[i]="tool"+string(rune('A'+i))}
 raw,_:=json.Marshal(map[string]any{"runtime_id":"world","application_id":"build","required":over})
 if _,err:=decodeToolchainRequirements(raw);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("unbounded preflight accepted: %v",err)
 }
}

func TestToolchainPreflightReportsMissingWithoutRunningBinaries(t *testing.T){
 sh,err:=exec.LookPath("sh")
 if err!=nil{t.Skip("sh not installed on test host")}
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0o700);err!=nil{t.Fatal(err)}
 marker:=filepath.Join(root,"must-not-exist")
 toolPath:=filepath.Join(bin,"my-own-builder")
 // Deliberately executable shell script which would create a marker. A
 // correct preflight checks existence/mode WITHOUT executing this content.
 executable:="#!/bin/sh\nprintf 'UNSAFE' > "+marker+"\n"
 if err:=os.WriteFile(toolPath,[]byte(executable),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(bin,"not-executable"),[]byte("x"),0o600);err!=nil{t.Fatal(err)}
 req,err:=decodeToolchainRequirements(json.RawMessage(`{"runtime_id":"world","application_id":"build","required":["not-executable","my-own-builder","unknown-cli"]}`))
 if err!=nil{t.Fatal(err)}
 cmd:=exec.Command(sh,append([]string{"-c",workspaceToolchainPreflightScript,"onepane-preflight"},req.Required...)...)
 cmd.Env=[]string{"PATH="+bin}
 stdout,err:=cmd.Output()
 if err!=nil{t.Fatalf("fixed tool checker failed: %v",err)}
 r,err:=parseToolchainPreflight(strings.TrimSpace(string(stdout)),req,"spec-hash")
 if err!=nil{t.Fatalf("read-only preflight output was rejected: %q %v",stdout,err)}
 if r.Status!="missing"||!r.Observed||len(r.Available)!=1||r.Available[0]!="my-own-builder"||
  strings.Join(r.Missing,",")!="not-executable,unknown-cli"||r.ExecutionGranted||r.InstallationGranted||
  r.RuntimeSpecRef!="spec-hash"||len(r.RequirementsRef)!=64{
  t.Fatalf("reported unsupported or privileged toolchain state: %+v",r)
 }
 if _,err=os.Stat(marker);!errors.Is(err,os.ErrNotExist){
  t.Fatal("preflight unexpectedly executed an installed binary")
 }
}
func TestToolchainPreflightFailClosedOnCorruptObservedEvidence(t *testing.T){
 req,_:=decodeToolchainRequirements(json.RawMessage(`{"runtime_id":"world","application_id":"build","required":["go","python3"]}`))
 good,err:=parseToolchainPreflight("go\t1\npython3\t1",req,"spec")
 if err!=nil||good.Status!="ready"||len(good.Missing)!=0||!good.Observed{
  t.Fatalf("valid observed prerequisites rejected: %+v %v",good,err)
 }
 for _,bad:=range []string{
  "","go\t1","go\t1\npython3\tmaybe",
  "python3\t1\ngo\t1","go\t1\npython3\t1\nmalicious\t1",
  "go\t1\npython3\t1\n","go\t1\npython3\t1\n\n",
  "go\t1\npython3\t1\nSECRET=123",
  "go\t1\npython3\t1\n/path/to/secret",
  strings.Repeat("x",maxToolchainPreflightStdout+1),
 }{
  _,err:=parseToolchainPreflight(bad,req,"spec")
  if !errors.Is(err,ErrInvalidInput){t.Fatalf("malformed OCI output accepted %q: %v",bad[:min(len(bad),50)],err)}
 }
 unavailable:=toolchainPreflightUnavailable(req,"image-version")
 if unavailable.Status!="unavailable"||unavailable.Observed||len(unavailable.Missing)!=0{
  t.Fatalf("unavailable shell incorrectly recorded as missing prereqs: %+v",unavailable)
 }
}
func TestToolchainPreflightRequiresExactOwnedVerifiedOCI(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 a:=NewAdapter(t.TempDir(),eng)
 root,exists,err:=managedWorkspacePath(a.dataDir,"world",true)
 if err!=nil||!exists{t.Fatal(err)}
 eng.inspectMount=root
 eng.execStdout="go\t1\npython3\t0"
 request:=tool.AdapterRequest{ToolID:ToolAppToolchainPreflight,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"builder","required":["python3","go"]}`)}
 out,err:=a.Invoke(context.Background(),request)
 if err!=nil{t.Fatal(err)}
 var row struct{
  Preflight ToolchainPreflightResult `json:"preflight"`
  SandboxVerified bool `json:"sandbox_verified"`
 }
 if err:=json.Unmarshal(out.Result,&row);err!=nil{t.Fatal(err)}
 if !row.SandboxVerified||row.Preflight.Status!="missing"||
  strings.Join(row.Preflight.Missing,",")!="python3"||
  len(row.Preflight.Available)!=1||row.Preflight.Available[0]!="go"||
  row.Preflight.ExecutionGranted||row.Preflight.InstallationGranted{
  t.Fatalf("unsafe output or wrong prerequisite validation: %+v",row)
 }
 eng.execExitCode=127
 out,err=a.Invoke(context.Background(),request)
 if err!=nil{t.Fatalf("image without POSIX scanner should be explicit unavailable: %v",err)}
 if err=json.Unmarshal(out.Result,&row);err!=nil||row.Preflight.Status!="unavailable"||
  row.Preflight.Observed||len(row.Preflight.Missing)!=0{
  t.Fatalf("missing shell was reported as successful or missing: %+v %v",row,err)
 }
 eng.execExitCode=0
 calls:=eng.execs
 for _,unsafe:=range []string{
  `{"runtime_id":"world","application_id":"builder","required":["go"],"command":["sh","-c","id"]}`,
  `{"runtime_id":"world","application_id":"builder","required":["/usr/bin/go"]}`,
  `{"runtime_id":"world","application_id":"builder","required":["sh;cat /etc/passwd"]}`,
  `{"runtime_id":"world","application_id":"builder","required":["go"],"image":"unapproved"}`,
 }{
  if _,err=a.Invoke(context.Background(),tool.AdapterRequest{
   ToolID:ToolAppToolchainPreflight,Input:json.RawMessage(unsafe),
  });!errors.Is(err,ErrInvalidInput){t.Fatalf("injection accepted: %s: %v",unsafe,err)}
 }
 eng.extraBind=true
 if _,err=a.Invoke(context.Background(),request);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("unverified foreign Workspace bind accepted: %v",err)
 }
 eng.extraBind=false
 eng.profile.Rootless=false
 if _,err=a.Invoke(context.Background(),request);!errors.Is(err,ErrRootlessRequired){
  t.Fatalf("rootful preflight permitted: %v",err)
 }
 if calls!=eng.execs{t.Fatal("rejected preflight executed a new command")}
}
func TestToolchainPreflightCancelledNeverReportsReady(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true},blockExec:true}
 a:=NewAdapter(t.TempDir(),eng)
 root,exists,err:=managedWorkspacePath(a.dataDir,"world",true)
 if err!=nil||!exists{t.Fatal(err)}
 eng.inspectMount=root
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Millisecond)
 defer cancel()
 _,err=a.Invoke(ctx,tool.AdapterRequest{ToolID:ToolAppToolchainPreflight,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"builder","required":["go"]}`)})
 if !errors.Is(err,context.DeadlineExceeded){t.Fatalf("timed-out preflight claimed readiness: %v",err)}
}
