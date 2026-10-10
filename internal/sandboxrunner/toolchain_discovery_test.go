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

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestGeneralWorkspaceToolInventoryAllowsArbitraryImageTools(t *testing.T){
 for _,tc:=range []struct{
  in string
  valid bool
 }{
  {`{"runtime_id":"world","application_id":"builder"}`,true},
  {`{"runtime_id":"world","application_id":"builder","action":"exec"}`,false},
  {`{"runtime_id":"world","application_id":"builder","command":["sh","-c","id"]}`,false},
  {`{"runtime_id":"world","application_id":"builder","url":"http://outside"}`,false},
  {`{"runtime_id":"world","application_id":"builder","workspace_id":"story"}`,false},
  {`{"runtime_id":"../host","application_id":"builder"}`,false},
  {`{"runtime_id":"world","application_id":""}`,false},
  {`{"runtime_id":"world"}`,false},
 }{
  if got:=validToolDiscoveryEnvelope(json.RawMessage(tc.in));got!=tc.valid{
   t.Fatalf("unexpected input acceptance for %s, got=%v",tc.in,got)
  }
 }
 if p:=workspaceToolDiscoveryCommand();len(p)!=3||p[0]!="sh"||p[1]!="-c"||
  strings.Contains(p[2],"eval")||strings.Contains(p[2],"curl")||strings.Contains(p[2],"wget"){
  t.Fatalf("tool scanner accepts unsafe dynamic commands: %q",p)
 }
 inv,err:=decodeWorkspaceToolDiscovery("rustc\npython3\nmadeup-builder\ngodot\nGoTool\nrustc\n__ONEPANE_TRUNCATED__\n")
 if err!=nil||inv.Count!=5||!inv.Truncated||inv.Source!="fixed_in_container_path_scan"{
  t.Fatalf("arbitrary executable inventory not correctly bounded: %+v %v",inv,err)
 }
 byName:=map[string]string{}
 for _,t:=range inv.Tools{byName[t.Name]=t.Category}
 if byName["rustc"]!="languages_and_build"||byName["godot"]!="creative_and_engines"||
  byName["madeup-builder"]!="other_installed_executable"||
  byName["GoTool"]!="other_installed_executable"{
  t.Fatalf("hardcoded engine-only tools or missing unknown tool family: %+v",byName)
 }
 for _,s:=range []string{
  "sh\n../../host\n","sh\nprivate api-key\n","sh\n/path\n","bad\x00exec\n",
  strings.Repeat("a",toolDiscoveryStdoutMax+1),
 }{
  if _,err:=decodeWorkspaceToolDiscovery(s);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("unsafe inventory data leaked to agent: %q %v",s[:min(len(s),120)],err)
  }
 }
 empty,err:=decodeWorkspaceToolDiscovery("")
 if err!=nil||empty.Count!=0||len(empty.Tools)!=0{t.Fatalf("distroless/empty PATH mishandled: %+v %v",empty,err)}
}
func TestGeneralWorkspaceToolDiscoveryActuallyScansOnlyContainerPathNames(t *testing.T){
 sh,err:=exec.LookPath("sh")
 if err!=nil{t.Skip("POSIX shell not available on CI host")}
 dir:=t.TempDir()
 bin:=filepath.Join(dir,"bin")
 if err:=os.Mkdir(bin,0o700);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name string;mode os.FileMode}{
  {"some-unknown-compiler",0o700},{"python3",0o700},{"nonexecutable",0o600},
 }{
  if err:=os.WriteFile(filepath.Join(bin,tc.name),[]byte("test"),tc.mode);err!=nil{t.Fatal(err)}
 }
 // This host-side unit fixture executes the exact static script against
 // disposable test binaries. The *production* command runs in OCI only.
 cmd:=exec.Command(sh,"-c",workspaceToolDiscoveryScript)
 cmd.Env=[]string{"PATH="+bin}
 raw,err:=cmd.Output()
 if err!=nil{t.Fatalf("fixed Workspace scanner did not execute: %v",err)}
 inventory,err:=decodeWorkspaceToolDiscovery(string(raw))
 if err!=nil||inventory.Count!=2||inventory.Truncated{
  t.Fatalf("scanner incorrectly enumerated paths: %s %+v %v",raw,inventory,err)
 }
 if !strings.Contains(string(raw),"some-unknown-compiler\n")||
  strings.Contains(string(raw),dir)||strings.Contains(string(raw),"nonexecutable"){
  t.Fatalf("PATH scanner exposed directories or non-executables: %s",raw)
 }
}
func TestGeneralWorkspaceDiscoveryRequiresVerifiedTaskSandbox(t *testing.T){
 engine:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 a:=NewAdapter(t.TempDir(),engine)
 root,exists,err:=managedWorkspacePath(a.dataDir,"world",true)
 if err!=nil||!exists{t.Fatal(err)}
 engine.inspectMount=root
 engine.execStdout="python3\ncargo\nrandom-app\n"
 req:=tool.AdapterRequest{ToolID:ToolAppToolsDiscover,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"tools"}`)}
 out,err:=a.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 var v struct{
  Status string `json:"status"`
  Inventory WorkspaceToolInventory `json:"inventory"`
  SandboxVerified bool `json:"sandbox_verified"`
  ExecutionEnabled bool `json:"execution_enabled"`
  InstallationPermitted bool `json:"installation_permitted"`
 }
 if err:=json.Unmarshal(out.Result,&v);err!=nil{t.Fatal(err)}
 if v.Status!="observed"||v.Inventory.Count!=3||!v.SandboxVerified||
  v.ExecutionEnabled||v.InstallationPermitted||engine.execs!=1{
  t.Fatalf("unverified or exaggerated Workspace scanner results: %+v",v)
 }
 engine.execExitCode=127
 unavailable,err:=a.Invoke(context.Background(),req)
 if err!=nil{t.Fatalf("scanner unavailable should be explicit not host fallback: %v",err)}
 var result struct{Status string `json:"status"`}
 if err=json.Unmarshal(unavailable.Result,&result);err!=nil||result.Status!="unavailable"{
  t.Fatalf("scanner failure claimed known tool inventory: %+v %v",result,err)
 }
 engine.execExitCode=0
 before:=engine.execs
 for _,input:=range []string{
  `{"runtime_id":"world","application_id":"tools","command":["/bin/sh"]}`,
  `{"runtime_id":"world","application_id":"tools","image":"myimage:latest"}`,
  `{"runtime_id":"world","application_id":"tools","path":"/host/secrets"}`,
 }{
  if _,err:=a.Invoke(context.Background(),tool.AdapterRequest{ToolID:ToolAppToolsDiscover,Input:json.RawMessage(input)});!errors.Is(err,ErrInvalidInput){
   t.Fatalf("unsafe tool enumeration accepted: %s %v",input,err)
  }
 }
 engine.extraBind=true
 if _,err=a.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("extra host bind inspection accepted: %v",err)
 }
 engine.extraBind=false
 engine.profile.Rootless=false
 if _,err=a.Invoke(context.Background(),req);!errors.Is(err,ErrRootlessRequired){
  t.Fatalf("rootful tool discovery accepted: %v",err)
 }
 if engine.execs!=before{t.Fatal("rejected tool discovery executed a command")}
}

func TestGeneralWorkspaceCapabilityFamiliesAreConditionalAndNotAllowlist(t *testing.T){
 families:=GeneralWorkspaceCapabilities()
 if len(families)<7{t.Fatalf("game engine only catalog: %d",len(families))}
 seen:=map[string]bool{}
 for _,f:=range families{
  if f.ID==""||seen[f.ID]||len(f.Examples)==0||f.Name==""{
   t.Fatalf("invalid multi-domain profile: %+v",f)
  }
  seen[f.ID]=true
  if f.ID!="mcp_integrations"&&(f.Execution!="project.app.exec"||
   f.Availability!="requires_approved_installed_toolchain"){
   t.Fatalf("advertised unsupported privileged capability: %+v",f)
  }
 }
 for _,id:=range []string{"software_development","web_applications","data_research",
  "creative_media","automation","infrastructure","documents","mcp_integrations"}{
  if !seen[id]{t.Fatalf("missing general Workspace category %s",id)}
 }
 if knownToolCategory("random-homelab-test")==""||
  knownToolCategory("random-homelab-test")=="unsupported"{
  t.Fatal("generic tools wrongly excluded from Workspace execution")
 }
}
