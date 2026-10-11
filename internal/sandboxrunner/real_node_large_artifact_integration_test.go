//go:build integration

package sandboxrunner

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "regexp"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

// This approval-gated test uses an ACTUAL verified rootless OCI application,
// never a mocked engine, to exercise the full chunk-transfer path. It does not
// claim to validate the separate SQLite Library publisher, which has its own
// real DB/blob integration coverage. The image must be pre-pulled by the
// runner's unprivileged identity; no tests pull new images automatically.
func TestRealRootlessLargeArtifactPublication(t *testing.T){
 image:=strings.TrimSpace(os.Getenv("ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE"))
 if image==""{t.Skip("requires approved digest-pinned Python 3 image on trusted rootless runner")}
 ok,err:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image)
 if err!=nil||!ok{t.Fatal("approved large-file smoke image must use a full sha256 digest")}
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless{t.Fatalf("real unprivileged OCI engine required: %v",err)}
 if _,err:=engine.InspectImage(ctx,image);err!=nil{t.Fatalf("approved immutable image absent: %v",err)}
 runtimeID:=fmt.Sprintf("opqa-large-%d",time.Now().UnixNano())
 const appID="builder"
 dataDir:=t.TempDir()
 workspace,exists,err:=managedWorkspacePath(dataDir,runtimeID,true)
 if err!=nil||!exists{t.Fatal(err)}
 t.Cleanup(func(){
  cleanupCtx,stop:=context.WithTimeout(context.Background(),30*time.Second)
  defer stop()
  _,_,_=runCLI(cleanupCtx,profile.Executable,"rm","-f",containerName(runtimeID,appID))
  _,_,_=runCLI(cleanupCtx,profile.Executable,"network","rm",runtimeNetworkName(runtimeID))
 })
 spec:=ContainerSpec{RuntimeID:runtimeID,ApplicationID:appID,
  Image:image,WorkspacePath:workspace,
  Command:[]string{"sh","-c","sleep 400"},WorkingDir:"/workspace",
  NetworkInternal:true,Limits:ResourceLimits{CPUMillis:1000,MemoryMB:256,PIDs:64},
 }
 if _,err:=engine.EnsureNetwork(ctx,runtimeID,true);err!=nil{t.Fatal(err)}
 if _,err:=engine.EnsureContainer(ctx,spec);err!=nil{t.Fatalf("rootless image launch: %v",err)}
 actual,err:=engine.InspectContainer(ctx,runtimeID,appID)
 if err!=nil||actual.Status!="running"||!actual.IsolationVerified||
  actual.SpecHash!=specHash(spec)||!exactWorkspaceMount(actual,workspace){
  t.Fatalf("physical verified OCI ownership missing: %+v %v",actual,err)
 }
 // Fixed OnePane-owned fixture writes 2.5 MiB through the sandbox's own
 // Python, proving the transfer is not just synthesised Go mock responses.
 const fixtureScript=`import os
fd=os.open("/workspace/genuine-build.bin",os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
try:
 block=bytes(range(256))*2048
 for i in range(5):
  pos=0
  while pos<len(block):
   pos+=os.write(fd,block[pos:])
 os.fsync(fd)
finally:os.close(fd)`
 created,err:=engine.ExecContainer(ctx,runtimeID,appID,[]string{"python3","-I","-S","-c",fixtureScript})
 if err!=nil||created.ExitCode!=0{
  t.Fatalf("physical Python 3 toolchain did not generate expected binary: %+v %v",created,err)
 }
 diskBytes,err:=os.ReadFile(workspace+"/genuine-build.bin")
 if err!=nil||len(diskBytes)!=(5*workspacePublicationChunkSize){
  t.Fatalf("physical OCI binary missing or incomplete: n=%d %v",len(diskBytes),err)
 }
 sum:=sha256.Sum256(diskBytes)
 digest:=hex.EncodeToString(sum[:])
 sink:=&mockWorkspacePublicationSink{outcome:WorkspacePublication{
  ArtifactID:"physical-artifact",LibraryAssetID:"physical-library-version",
  Version:1,ContentHash:digest,SizeBytes:int64(len(diskBytes)),
  SourceWorkspaceID:"world",
 }}
 adapter:=NewAdapter(dataDir,engine)
 adapter.SetPublisher(sink)
 taskID,attemptID:="approved-physical-task","approved-physical-attempt"
 payload,_:=json.Marshal(map[string]any{
  "runtime_id":runtimeID,"application_id":appID,"action":"publish_large",
  "path":"genuine-build.bin","name":"genuine-build.bin","media_type":"application/octet-stream",
 })
 result,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:ToolAppFilePublish,WorkspaceID:"tenant",TaskID:&taskID,AttemptID:&attemptID,
  Input:payload,
 })
 if err!=nil{t.Fatalf("physical OCI chunked publication failed: %v",err)}
 if sink.called!=1||sink.req.ContentHash!=digest||
  len(sink.req.Content)!=len(diskBytes)||
  sha256.Sum256(sink.req.Content)!=sum{
  t.Fatalf("physical content bytes were not SHA verified: called=%d n=%d digest=%s",sink.called,len(sink.req.Content),sink.req.ContentHash)
 }
 if strings.Contains(string(result.Result),"content_base64")||
  strings.Contains(string(result.Result),string(diskBytes[:64])){
  t.Fatal("physical publication exposed raw bytes to Agent tool result")
 }
 // The same trusted Python-capable immutable image also proves that
 // arbitrary toolchain prerequisites are checked only in this OCI namespace,
 // and that a declared required tool can guard a real Task command.
 required,_:=json.Marshal(map[string]any{
  "runtime_id":runtimeID,"application_id":appID,"required":[]string{"python3","sh"},
 })
 checked,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:ToolAppToolchainPreflight,WorkspaceID:"tenant",Input:required,
 })
 if err!=nil{t.Fatalf("physical toolchain preflight denied: %v",err)}
 var readiness struct{Preflight ToolchainPreflightResult `json:"preflight"`}
 if err=json.Unmarshal(checked.Result,&readiness);err!=nil||readiness.Preflight.Status!="ready"||
  !readiness.Preflight.Observed||len(readiness.Preflight.Missing)!=0{
  t.Fatalf("actual OCI image requirements not verified: %+v %v",readiness,err)
 }
 guarded,_:=json.Marshal(map[string]any{
  "runtime_id":runtimeID,"application_id":appID,
  "command":[]string{"python3","-I","-S","-c","print('onepane-approved-toolchain')"},
  "required_executables":[]string{"sh","python3"},
 })
 ran,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:ToolAppExec,WorkspaceID:"tenant",Input:guarded,
 })
 if err!=nil{t.Fatalf("physical guarded general-purpose execution failed: %v",err)}
 var executed struct{
  Succeeded bool `json:"succeeded"`
  ToolchainPreflight string `json:"toolchain_preflight"`
  Result ExecResult `json:"result"`
 }
 if err=json.Unmarshal(ran.Result,&executed);err!=nil||!executed.Succeeded||
  executed.ToolchainPreflight!="ready"||executed.Result.Stdout!="onepane-approved-toolchain"{
  t.Fatalf("OCI required software was not independently checked before launch: %+v %v",executed,err)
 }
 missing,_:=json.Marshal(map[string]any{
  "runtime_id":runtimeID,"application_id":appID,
  "command":[]string{"python3","-I","-S","-c","print('UNSAFE_SHOULD_NOT_RUN')"},
  "required_executables":[]string{"onepane-definitely-missing-preflight-tool"},
 })
 if _,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:ToolAppExec,WorkspaceID:"tenant",Input:missing,
 });!errors.Is(err,ErrInvalidInput){
  t.Fatalf("missing required physical OCI tool did not block command: %v",err)
 }
 t.Logf("Real rootless OCI 2.5 MiB published via 5 bounded chunks; source/host checksum %s",digest)
}
