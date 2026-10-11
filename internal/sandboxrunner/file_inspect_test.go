package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "os/exec"
 "strconv"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestWorkspaceFileInspectionRejectsPathEscapesAndSymlinks(t *testing.T){
 workspace:=t.TempDir()
 if err:=os.MkdirAll(filepath.Join(workspace,"src"),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(workspace,"src","main.go"),[]byte("package main"),0o600);err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(filepath.Join(workspace,".git"),0o700);err!=nil{t.Fatal(err)}
 if err:=verifyWorkspacePreviewPath(workspace,"src/main.go");err!=nil{t.Fatal(err)}
 for _,bad:=range []string{"","../outside","src/../main.go","/etc/passwd",
  "./src/main.go","src//main.go",".git/config","C:/Windows/system.ini",
  "src\\main.go","src","src/missing.txt"," src/main.go",
 }{
  if err:=verifyWorkspacePreviewPath(workspace,bad);err==nil{
   t.Fatalf("invalid file preview path allowed: %q",bad)
  }
 }
 outside:=filepath.Join(t.TempDir(),"secret.txt")
 if err:=os.WriteFile(outside,[]byte("secret"),0o600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(workspace,"src","link.txt"));err==nil{
  if err:=verifyWorkspacePreviewPath(workspace,"src/link.txt");err==nil{
   t.Fatal("preview followed a Workspace symlink into a different location")
  }
 }
 for _,action:=range []string{"list","preview_text"}{
  p:=""
  if action=="preview_text"{p="src/main.go"}
  argv,err:=fileInspectCommand(action,p)
  if err!=nil||len(argv)<4||argv[0]==""{
   t.Fatalf("no bounded file inspection plan for %s: %q %v",action,argv,err)
  }
  if strings.Join(argv," ")==""||strings.Contains(strings.Join(argv," "),"sh -c"){
   t.Fatal("file inspection launched a shell")
  }
 }
 for _,tc:=range []struct{action,path string}{
  {"write","src/main.go"},{"preview_text","../outside"},{"preview_text",".git/config"},
  {"list","src"},{"",""},
 }{
  if _,err:=fileInspectCommand(tc.action,tc.path);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("illegal file action/path allowed %q/%q: %v",tc.action,tc.path,err)
  }
 }
}

func TestFileInspectionRequiresVerifiedRootlessSandboxAndBoundedPreview(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 workspace,ok,err:=managedWorkspacePath(adapter.dataDir,"runtime-world",true)
 if err!=nil||!ok{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(workspace,"README.md"),[]byte("# world"),0o600);err!=nil{t.Fatal(err)}
 eng.inspectMount=workspace
 req:=tool.AdapterRequest{ToolID:ToolAppFileInspect,
  Input:json.RawMessage(`{"runtime_id":"runtime-world","application_id":"tool","action":"preview_text","path":"README.md"}`)}
 v,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 var result struct{
  Succeeded bool `json:"succeeded"`
  Action string `json:"action"`
  PreviewOnly bool `json:"preview_only"`
  Result ExecResult `json:"result"`
 }
 if err:=json.Unmarshal(v.Result,&result);err!=nil{t.Fatal(err)}
 if !result.Succeeded||result.Action!="preview_text"||!result.PreviewOnly||
  !strings.Contains(result.Result.Stdout,"python3 -I -S -c")||!strings.Contains(result.Result.Stdout,"README.md"){
  t.Fatalf("text preview not correctly bounded in Workspace: %+v",result)
 }
 before:=eng.execs
 for _,raw:=range []json.RawMessage{
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"tool","action":"preview_text","path":"../secret"}`),
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"tool","action":"list","path":"/host"}`),
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"tool","action":"preview_text","path":"README.md","command":["cat","/etc/shadow"]}`),
  json.RawMessage(`{"runtime_id":"runtime-world","application_id":"tool","action":"read","path":"README.md"}`),
 }{
  if _,err:=adapter.Invoke(context.Background(),tool.AdapterRequest{
   ToolID:ToolAppFileInspect,Input:raw,
  });!errors.Is(err,ErrInvalidInput){
   t.Fatalf("file command escalation accepted: %s => %v",raw,err)
  }
 }
 eng.extraBind=true
 if _,err:=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("preview accepted host-mounted OCI container: %v",err)
 }
 if eng.execs!=before{t.Fatalf("denied file read reached engine: %d->%d",before,eng.execs)}
}

func TestActualOCITextPreviewUsesNoFollowAtOpen(t *testing.T){
 python,err:=exec.LookPath("python3")
 if err!=nil{t.Skip("Python3 toolchain not available on CI host")}
 root:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(root,"src"),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"src","valid.txt"),[]byte("hello\n"),0o600);err!=nil{t.Fatal(err)}
 // The production interpreter can open ONLY /workspace. The test substitutes
 // that fixed root with a temporary fixture to verify descriptor behaviour.
 script:=strings.Replace(workspaceTextPreview,`os.open("/workspace",`,
  "os.open("+strconv.Quote(root)+",",1)
 run:=func(rel string)(int,string){
  t.Helper()
  output,err:=exec.Command(python,"-I","-S","-c",script,rel).CombinedOutput()
  if err==nil{return 0,string(output)}
  if ex,ok:=err.(*exec.ExitError);ok{return ex.ExitCode(),string(output)}
  t.Fatal(err)
  return -1,""
 }
 if code,out:=run("src/valid.txt");code!=0||out!="hello\n"{
  t.Fatalf("valid descriptor-bounded preview failed: %d %q",code,out)
 }
 outside:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(outside,"secret.txt"),[]byte("top-secret"),0o600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(root,"src","link"));err==nil{
  if code,out:=run("src/link/secret.txt");code==0||strings.Contains(out,"top-secret"){
   t.Fatalf("followed symlinked directory: %d %q",code,out)
  }
 }
 if err:=os.Symlink(filepath.Join(outside,"secret.txt"),filepath.Join(root,"src","secret-link"));err==nil{
  if code,out:=run("src/secret-link");code==0||strings.Contains(out,"top-secret"){
   t.Fatalf("followed symlinked file: %d %q",code,out)
  }
 }
 if err:=os.WriteFile(filepath.Join(root,"src","binary"),[]byte{0,1,2},0o600);err!=nil{t.Fatal(err)}
 if code,_:=run("src/binary");code==0{t.Fatal("binary data passed text preview")}
 if err:=os.WriteFile(filepath.Join(root,"src","large.txt"),[]byte(strings.Repeat("x",65537)),0o600);err!=nil{t.Fatal(err)}
 if code,out:=run("src/large.txt");code!=0||len(out)!=65536{
  t.Fatalf("preview exceeded text byte cap: %d %d bytes",code,len(out))
 }
}
