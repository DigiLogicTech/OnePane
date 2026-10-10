package sandboxrunner

import (
 "context"
 "crypto/sha256"
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

func largeFixture(t *testing.T,size int)(string,string,[]byte){
 t.Helper()
 root:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(root,"artifacts"),0o700);err!=nil{t.Fatal(err)}
 bytes:=make([]byte,size)
 for i:=range bytes{bytes[i]=byte((i*71+13)%251)}
 if err:=os.WriteFile(filepath.Join(root,"artifacts","build.bin"),bytes,0o600);err!=nil{t.Fatal(err)}
 return root,"artifacts/build.bin",bytes
}
func executeActualLargeRead(t *testing.T,root string)largePublicationExecutor{
 t.Helper()
 python,err:=exec.LookPath("python3")
 if err!=nil{t.Skip("Python3 required to test fixed in-sandbox reader")}
 return func(ctx context.Context,command []string)(ExecResult,error){
  if len(command)!=8||command[0]!="python3"||command[1]!="-I"||
   command[2]!="-S"||command[3]!="-c"{
   t.Fatalf("unsafe or malformed approved command: %q",command)
  }
  patched:=strings.Replace(command[4],`os.open("/workspace",`,
   "os.open("+strconv.Quote(root)+",",1)
  args:=append([]string{"-I","-S","-c",patched},command[5:]...)
  call:=exec.CommandContext(ctx,python,args...)
  raw,err:=call.Output()
  if err!=nil{
   var exit *exec.ExitError
   if errors.As(err,&exit){return ExecResult{ExitCode:exit.ExitCode()},nil}
   return ExecResult{},err
  }
  return ExecResult{Stdout:strings.TrimSpace(string(raw))},nil
 }
}
func TestChunkedLargePublicationReadsActualNoFollowSnapshot(t *testing.T){
 root,path,original:=largeFixture(t,(2<<20)+173)
 run:=executeActualLargeRead(t,root)
 var calls int
 executor:=func(ctx context.Context,cmd []string)(ExecResult,error){
  calls++;return run(ctx,cmd)
 }
 data,got,err:=readWorkspaceLargePublication(context.Background(),path,executor)
 if err!=nil{t.Fatalf("valid large Workspace output rejected: %v",err)}
 sum:=sha256.Sum256(original)
 if len(data)!=len(original)||got!=hex.EncodeToString(sum[:])||
  sha256.Sum256(data)!=sum||
  calls!=(len(original)+workspacePublicationChunkSize-1)/workspacePublicationChunkSize+2{
  t.Fatalf("chunked source mismatch: n=%d calls=%d sha=%s",len(data),calls,got)
 }
}
func TestLargePublicationRejectsSymlinksAndOversizeSource(t *testing.T){
 root,_,_:=largeFixture(t,10)
 outside:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(outside,"private"),[]byte("DO_NOT_DISCLOSE"),0o600);err!=nil{t.Fatal(err)}
 run:=executeActualLargeRead(t,root)
 for _,bad:=range []string{"../private",".git/config","/host/passwd","artifacts/../private","", ".onepane-internal"}{
  if _,err:=publicationLargeReadCommand(bad,"manifest",0);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("unsafe path accepted: %q %v",bad,err)
  }
 }
 if err:=os.Symlink(outside,filepath.Join(root,"artifacts","link"));err==nil{
  data,_,err:=readWorkspaceLargePublication(context.Background(),"artifacts/link/private",run)
  if !errors.Is(err,ErrInvalidInput)||len(data)!=0{t.Fatalf("followed symlink parent: %v",err)}
 }
 if err:=os.Symlink(filepath.Join(outside,"private"),filepath.Join(root,"artifacts","filelink"));err==nil{
  data,_,err:=readWorkspaceLargePublication(context.Background(),"artifacts/filelink",run)
  if !errors.Is(err,ErrInvalidInput)||len(data)!=0{t.Fatalf("followed symlink file: %v",err)}
 }
 large:=make([]byte,WorkspaceLargePublicationLimit+1)
 if err:=os.WriteFile(filepath.Join(root,"artifacts","too-big.bin"),large,0o600);err!=nil{t.Fatal(err)}
 data,_,err:=readWorkspaceLargePublication(context.Background(),"artifacts/too-big.bin",run)
 if !errors.Is(err,ErrInvalidInput)||len(data)!=0{t.Fatalf("oversize file was published: %v",err)}
}
func TestLargePublicationDetectsMutationDuringChunkTransfer(t *testing.T){
 root,path,_:=largeFixture(t,(1<<20)+33)
 run:=executeActualLargeRead(t,root)
 var chunk int
 executor:=func(ctx context.Context,cmd []string)(ExecResult,error){
  result,err:=run(ctx,cmd)
  if cmd[5]=="chunk"&&chunk==0{
   chunk++
   if err:=os.WriteFile(filepath.Join(root,path),[]byte("changed DURING READ"),0o600);err!=nil{t.Fatal(err)}
  }
  return result,err
 }
 data,_,err:=readWorkspaceLargePublication(context.Background(),path,executor)
 if !errors.Is(err,ErrInvalidInput)||len(data)!=0{t.Fatalf("mixed build versions accepted: %v",err)}
}
func TestLargePublicationRejectsCorruptChunkAndForgedManifest(t *testing.T){
 root,path,_:=largeFixture(t,(1<<20)+101)
 run:=executeActualLargeRead(t,root)
 for _,method:=range []string{"chunk","manifest"}{
  t.Run(method,func(t *testing.T){
   executor:=func(ctx context.Context,cmd []string)(ExecResult,error){
    observed,err:=run(ctx,cmd)
    if err!=nil{return observed,err}
    if cmd[5]==method{
     var row map[string]any
     if err:=json.Unmarshal([]byte(observed.Stdout),&row);err!=nil{t.Fatal(err)}
     row["sha256"]=strings.Repeat("0",64)
     payload,_:=json.Marshal(row)
     observed.Stdout=string(payload)
    }
    return observed,nil
   }
   data,_,err:=readWorkspaceLargePublication(context.Background(),path,executor)
   if !errors.Is(err,ErrInvalidInput)||len(data)!=0{t.Fatalf("corrupt %s accepted: %v",method,err)}
  })
 }
 for _,bad:=range []string{"{}","[]",
  `{"size_bytes":50,"sha256":"BAD","fingerprint":[1,2,50,3,4]}`,
  `{"size_bytes":50,"sha256":"`+strings.Repeat("f",64)+`","fingerprint":[1,2,30,3,4]}`,
  strings.Repeat("x",workspacePublicationReceiptLimit+1),
 }{
  if _,err:=decodeLargeManifest(bad);!errors.Is(err,ErrInvalidInput){
   t.Fatalf("forged manifest accepted: %q %v",bad[:min(len(bad),90)],err)
  }
 }
 if _,err:=publicationLargeReadCommand(path,"chunk",1);!errors.Is(err,ErrInvalidInput){t.Fatal("unaligned chunk accepted")}
 if _,err:=publicationLargeReadCommand(path,"exec",0);!errors.Is(err,ErrInvalidInput){t.Fatal("untrusted action accepted")}
}
func TestLargePublishAdapterReusesTaskOwnedPublisherWithoutLeakingBytes(t *testing.T){
 root,path,original:=largeFixture(t,(1<<20)+49)
 run:=executeActualLargeRead(t,root)
 engine:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),engine)
 workspace,ok,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 engine.inspectMount=workspace
 engine.execHandler=run
 sink:=&mockWorkspacePublicationSink{}
 sum:=sha256.Sum256(original)
 digest:=hex.EncodeToString(sum[:])
 sink.outcome=WorkspacePublication{ArtifactID:"managed-artifact",LibraryAssetID:"library-version",
  Version:2,ContentHash:digest,SizeBytes:int64(len(original)),SourceWorkspaceID:"world"}
 adapter.SetPublisher(sink)
 taskID,attemptID:="owned-task","owned-attempt"
 req:=tool.AdapterRequest{ToolID:ToolAppFilePublish,
  WorkspaceID:"tenant",TaskID:&taskID,AttemptID:&attemptID,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"builder","action":"publish_large","path":"artifacts/build.bin","name":"build.bin","media_type":"application/octet-stream"}`)}
 out,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 if sink.called!=1||sink.req.TaskID!=taskID||sink.req.AttemptID!=attemptID||
  sink.req.Path!=path||sink.req.ContentHash!=digest||
  sha256.Sum256(sink.req.Content)!=sum{
  t.Fatalf("large binary was not passed through governed publisher: %+v",sink.req)
 }
 if strings.Contains(string(out.Result),hex.EncodeToString(original[:50]))||
  strings.Contains(string(out.Result),"content_base64")||
  strings.Contains(string(out.Result),"owned-attempt"){
  t.Fatalf("published bytes or task provenance leaked to Tool output: %s",out.Result)
 }
 initial:=sink.called
 engine.extraBind=true
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||sink.called!=initial{
  t.Fatalf("foreign writable mount was allowed to publish: %v",err)
 }
 engine.extraBind=false
 req.AttemptID=nil
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||sink.called!=initial{
  t.Fatalf("unowned Task published: %v",err)
 }
}
