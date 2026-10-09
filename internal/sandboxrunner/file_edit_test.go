package sandboxrunner

import (
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestFileEditCommandRequiresBoundedCanonicalPayloadAndExpectedRevision(t *testing.T) {
 contents:=base64.StdEncoding.EncodeToString([]byte("package main\n"))
 _,sha,n,err:=fileEditCommand("create","src/main.go",contents,"")
 if err!=nil||len(sha)!=64||n!=13 {t.Fatalf("valid content rejected: %q %d %v",sha,n,err)}
 if _,_,_,err:=fileEditCommand("replace","src/main.go",contents,sha);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{action,path,data,expected string}{
  {"remove","src/main.go",contents,""},
  {"create","../outside",contents,""},
  {"create",".git/config",contents,""},
  {"create",".onepane-private-env",contents,""},
  {"create","src/link/../main.go",contents,""},
  {"create","src/main.go","",""},
  {"create","src/main.go","not-base64",""},
  {"create","src/main.go",contents,"0123"},
  {"replace","src/main.go",contents,""},
  {"replace","src/main.go",contents,strings.Repeat("x",64)},
  {"replace","src/main.go",contents,strings.ToUpper(sha)},
  {"create","src/main.go",base64.StdEncoding.EncodeToString(make([]byte,WorkspaceFileEditLimit+1)),""},
 } {
  if _,_,_,err:=fileEditCommand(tc.action,tc.path,tc.data,tc.expected);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("unsafe edit accepted: action=%q path=%q error=%v",tc.action,tc.path,err)
  }
 }
 command,_,_,err:=fileEditCommand("create","src/main.go",contents,"")
 if err!=nil||len(command)!=9||command[0]!="python3"||command[1]!="-I"||command[2]!="-S"||
 command[3]!="-c"||command[4]!=workspaceFileEditor||command[6]!="src/main.go" {
  t.Fatalf("fixed no-shell OCI edit command invalid: %q %v",command,err)
 }
}

func TestFileEditPythonAtomicCreateReplaceAndSymlinkEscape(t *testing.T){
 python,err:=exec.LookPath("python3")
 if err!=nil{t.Skip("Python3 runtime not available on this CI host")}
 root:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(root,"src"),0o700);err!=nil{t.Fatal(err)}
 // Replace only the fixed OCI mount path in the test interpreter. Production
 // always runs within the sandbox and opens /workspace with O_NOFOLLOW.
 script:=strings.Replace(workspaceFileEditor,`os.open("/workspace",`,
  "os.open("+strconv.Quote(root)+",",1)
 run:=func(action,relative,data,expected string)(int,string){
  t.Helper()
  args:=[]string{"-I","-S","-c",script,action,relative,base64.StdEncoding.EncodeToString([]byte(data)),expected}
  out,err:=exec.Command(python,args...).CombinedOutput()
  if err==nil{return 0,string(out)}
  if exit,ok:=err.(*exec.ExitError);ok{return exit.ExitCode(),string(out)}
  t.Fatal(err)
  return -1,""
 }
 code,out:=run("create","src/world.txt","world one","")
 if code!=0||!strings.Contains(out,`"written": true`){t.Fatalf("create failed: %d %q",code,out)}
 data,err:=os.ReadFile(filepath.Join(root,"src/world.txt"))
 if err!=nil||string(data)!="world one"{t.Fatalf("wrong write: %q %v",data,err)}
 if code,out=run("create","src/world.txt","overwrite","");code==0{t.Fatalf("create overwrote existing file: %q",out)}
 expected:=sha256.Sum256(data)
 if code,out=run("replace","src/world.txt","world two",strings.Repeat("0",64));code==0{t.Fatalf("stale version was overwritten: %q",out)}
 if code,out=run("replace","src/world.txt","world two",hex.EncodeToString(expected[:]));code!=0{t.Fatalf("replace failed: %d %q",code,out)}
 updated,err:=os.ReadFile(filepath.Join(root,"src/world.txt"))
 if err!=nil||string(updated)!="world two"{t.Fatalf("expected atomic replacement: %q %v",updated,err)}
 parentOutside:=t.TempDir()
 if err:=os.Symlink(parentOutside,filepath.Join(root,"src","link"));err==nil{
  if code,out=run("create","src/link/escaped.txt","escape","");code==0{t.Fatalf("followed symlink: %q",out)}
  if _,err:=os.Stat(filepath.Join(parentOutside,"escaped.txt"));!os.IsNotExist(err){
   t.Fatalf("symlink escaped into outside directory: %v",err)
  }
 }
 if code,out=run("replace","src","replace directory",strings.Repeat("0",64));code==0{
  t.Fatalf("directory replace was allowed: %q",out)
 }
 leftovers,err:=filepath.Glob(filepath.Join(root,"src",".onepane-write-*"))
 if err!=nil||len(leftovers)!=0{t.Fatalf("temporary OCI editor files leaked: %v %v",leftovers,err)}
}

func TestFileEditAdapterRedactsPayloadAndRejectsForgedSuccess(t *testing.T){
 engine:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),engine)
 path,exists,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!exists{t.Fatal(err)}
 engine.inspectMount=path
 secret:="not-for-tool-observation"
 payload:=base64.StdEncoding.EncodeToString([]byte(secret))
 sha:=sha256.Sum256([]byte(secret))
 digest:=hex.EncodeToString(sha[:])
 engine.execStdout=`{"action":"create","path":"hello.txt","bytes":24,"sha256":"broken","written":true}`
 request:=tool.AdapterRequest{ToolID:ToolAppFileEdit,Input:json.RawMessage(
 `{"runtime_id":"world","application_id":"editor","action":"create","path":"hello.txt","content_base64":"`+payload+`"}`)}
 returned,err:=adapter.Invoke(context.Background(),request)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(returned.Result),payload)||strings.Contains(string(returned.Result),secret){
  t.Fatal("encoded private file contents leaked to Task observation")
 }
 var response struct{ Succeeded bool `json:"succeeded"`; ReceiptVerified bool `json:"receipt_verified"` }
 if err:=json.Unmarshal(returned.Result,&response);err!=nil{t.Fatal(err)}
 if response.Succeeded||response.ReceiptVerified{t.Fatal("accepted forged OCI receipt")}
 engine.execStdout=`{"action":"create","path":"hello.txt","bytes":`+strconv.Itoa(len(secret))+
  `,"sha256":"`+digest+`","written":true}`
 returned,err=adapter.Invoke(context.Background(),request)
 if err!=nil{t.Fatal(err)}
 if err:=json.Unmarshal(returned.Result,&response);err!=nil{t.Fatal(err)}
 if !response.Succeeded||!response.ReceiptVerified{
  t.Fatalf("correct receipt not accepted: %s",returned.Result)
 }
 before:=engine.execs
 engine.extraBind=true
 _,err=adapter.Invoke(context.Background(),request)
 if !errors.Is(err,ErrInvalidInput)||engine.execs!=before{
  t.Fatalf("host-mounted or extra-bind editor executed: %d=>%d: %v",before,engine.execs,err)
 }
}
