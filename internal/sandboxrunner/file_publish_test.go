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

func TestOCIWorkspacePublicationReceiptIsBoundedAndVerified(t *testing.T){
 b:=[]byte("world asset\n")
 sum:=sha256.Sum256(b)
 raw,_:=json.Marshal(map[string]any{
  "content_base64":base64.StdEncoding.EncodeToString(b),
  "size_bytes":len(b),"sha256":hex.EncodeToString(sum[:]),
 })
 data,got,err:=decodePublicationRead(string(raw))
 if err!=nil||string(data)!=string(b)||got!=hex.EncodeToString(sum[:]){
  t.Fatalf("valid immutable publication receipt rejected: %v",err)
 }
 for _,bad:=range []string{
  "{}",`{"content_base64":"@@@","size_bytes":3,"sha256":"none"}`,
  strings.Replace(string(raw),hex.EncodeToString(sum[:]),strings.Repeat("0",64),1),
  strings.Replace(string(raw),`"size_bytes":12`,`"size_bytes":100`,1),
 }{
  if _,_,err:=decodePublicationRead(bad);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("forged receipt accepted: %s %v",bad,err)
  }
 }
 for _,path:=range []string{"../secret",".git/config","/host/file","src/../secret",""}{
  if _,err:=publicationReadCommand(path);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("unsafe publication path accepted: %s %v",path,err)
  }
 }
}

func TestActualOCIWorkspacePublicationReadIsNoFollowAndSizeBounded(t *testing.T){
 python,err:=exec.LookPath("python3")
 if err!=nil{t.Skip("Python3 unavailable on CI host")}
 root:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(root,"src"),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"src","good.bin"),[]byte("trusted"),0o600);err!=nil{t.Fatal(err)}
 script:=strings.Replace(workspacePublicationRead,`os.open("/workspace",`,
  "os.open("+strconv.Quote(root)+",",1)
 run:=func(rel string)(int,string){
  t.Helper()
  raw,err:=exec.Command(python,"-I","-S","-c",script,rel).CombinedOutput()
  if err==nil{return 0,string(raw)}
  if ex,ok:=err.(*exec.ExitError);ok{return ex.ExitCode(),string(raw)}
  t.Fatal(err)
  return -1,""
 }
 if code,out:=run("src/good.bin");code!=0{
  t.Fatalf("valid OCI bounded publication failed: %d %s",code,out)
 }else if b,_,err:=decodePublicationRead(strings.TrimSpace(out));err!=nil||string(b)!="trusted"{
  t.Fatalf("export content/hash corrupt: %s %v",out,err)
 }
 outside:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(outside,"hidden"),[]byte("private"),0o600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(root,"src","escape"));err==nil{
  if code,out:=run("src/escape/hidden");code==0||strings.Contains(out,"private"){
   t.Fatalf("followed OCI parent symlink: %d %s",code,out)
  }
 }
 if err:=os.Symlink(filepath.Join(outside,"hidden"),filepath.Join(root,"src","escape-file"));err==nil{
  if code,out:=run("src/escape-file");code==0||strings.Contains(out,"private"){
   t.Fatalf("followed OCI file symlink: %d %s",code,out)
  }
 }
 if err:=os.WriteFile(filepath.Join(root,"src","too-large"),make([]byte,WorkspacePublicationLimit+1),0o600);err!=nil{t.Fatal(err)}
 if code,out:=run("src/too-large");code==0||strings.Contains(out,"content_base64"){
  t.Fatalf("published oversized file: %d %s",code,out)
 }
}

type mockWorkspacePublicationSink struct{
 called int
 req WorkspacePublicationRequest
 outcome WorkspacePublication
}
func (p *mockWorkspacePublicationSink) PublishWorkspaceFile(_ context.Context,r WorkspacePublicationRequest)(WorkspacePublication,error){
 p.called++;p.req=r
 return p.outcome,nil
}
func TestPublishedOCIContentNeverAppearsInToolObservations(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 workspace,ok,err:=managedWorkspacePath(adapter.dataDir,"runtime",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=workspace
 value:=[]byte("private generated build content")
 sha:=sha256.Sum256(value)
 hash:=hex.EncodeToString(sha[:])
 raw,_:=json.Marshal(map[string]any{
  "content_base64":base64.StdEncoding.EncodeToString(value),"size_bytes":len(value),"sha256":hash,
 })
 eng.execStdout=string(raw)
 sink:=&mockWorkspacePublicationSink{
  outcome:WorkspacePublication{ArtifactID:"art",LibraryAssetID:"lib",Version:1,ContentHash:hash,SizeBytes:int64(len(value)),SourceWorkspaceID:"world"},
 }
 adapter.SetPublisher(sink)
 taskID,attemptID:="t","att"
 req:=tool.AdapterRequest{ToolID:ToolAppFilePublish,
  WorkspaceID:"tenant",TaskID:&taskID,AttemptID:&attemptID,
  Input:json.RawMessage(`{"runtime_id":"runtime","application_id":"editor","action":"publish","path":"assets/compiled.bin","name":"compiled.bin","media_type":"application/octet-stream"}`),
 }
 result,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 if sink.called!=1||sink.req.TaskID!="t"||sink.req.AttemptID!="att"||
  sink.req.Path!="assets/compiled.bin"||string(sink.req.Content)!=string(value){
  t.Fatalf("publication sink never received tenant Task-bound verified content: %+v",sink.req)
 }
 if strings.Contains(string(result.Result),string(value))||
  strings.Contains(string(result.Result),base64.StdEncoding.EncodeToString(value)){
  t.Fatalf("sensitive file bytes leaked to agent observation: %s",result.Result)
 }
 before:=sink.called
 eng.extraBind=true
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||sink.called!=before{
  t.Fatalf("foreign host-mounted app published: %v",err)
 }
 eng.extraBind=false
 eng.execStdout=`{"content_base64":"dGVzdA==","size_bytes":4,"sha256":"bad"}`
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||sink.called!=before{
  t.Fatalf("invalid in-container receipt reached managed Project Library: %v",err)
 }
}
