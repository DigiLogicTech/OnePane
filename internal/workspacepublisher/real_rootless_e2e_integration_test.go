//go:build integration

package workspacepublisher

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

// TestRealRootlessEndToEndLibraryPublication is an opt-in physical acceptance
// test. Unlike in-memory adapter receipt fixtures it uses an actual pre-pulled
// immutable OCI image AND the real SQLite, artifact blob and Project Library
// publisher in one Task-owned pipeline. It never installs an image or grants
// sibling Workspaces implicit access. Skipping is NOT a physical pass.
func TestRealRootlessEndToEndLibraryPublication(t *testing.T) {
 image:=strings.TrimSpace(os.Getenv("ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE"))
 if image=="" {t.Skip("requires a trusted rootless Node and approved pre-pulled digest-pinned Python image")}
 if matched,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!matched {
  t.Fatal("physical Library acceptance requires an immutable image digest")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Minute)
 defer cancel()
 engine:=sandboxrunner.NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless||profile.Kind!="podman" {
  t.Fatalf("physical Library acceptance requires trusted rootless Podman: %v",err)
 }
 if _,err=engine.InspectImage(ctx,image);err!=nil {
  t.Fatalf("approved immutable Python image is not pre-pulled under runner identity: %v",err)
 }

 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"physical-library.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 const tenant,operator,node="physical-tenant","physical-operator","physical-local-node"
 now:=clock.Real{}.UnixMilli()
 for _,statement:=range []string{
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
   protocol_json,capabilities_json,revision,created_at,updated_at)
   VALUES('physical-local-node','Physical trusted Node',1,'physical-trusted-fixture','local','{}','{}',1,?,?)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('physical-tenant','Physical tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('physical-operator','human','Physical operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('physical-tenant','physical-operator','active',?,?)`,
 } {
  if _,err=db.SQL().ExecContext(ctx,statement,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:tenant,Name:"Physical toolchain acceptance",CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}
 build,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Builder",ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatal(err)}
 sibling,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Isolated reviewer",ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&build.ID,CreatedBy:operator,NodeID:ptrPhysical(node),
 })
 if err!=nil{t.Fatal(err)}
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Python immutable builder",
  SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}
 store,err:=artifact.NewLocalStore(filepath.Join(t.TempDir(),"immutable-blobs"))
 if err!=nil{t.Fatal(err)}
 artifacts:=artifact.NewService(db.SQL(),db,store,clock.Real{})
 publisher:=New(db.SQL(),artifacts,projects,node)
 adapter:=sandboxrunner.NewAdapter(t.TempDir(),engine)
 adapter.SetPublisher(publisher)

 ensureInput,_:=json.Marshal(map[string]any{"runtime_id":runtime.ID})
 provisioned,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:sandboxrunner.ToolRuntimeEnsure,WorkspaceID:tenant,Input:ensureInput,
 })
 if err!=nil{t.Fatalf("cannot provision physical managed Workspace: %v",err)}
 var bound struct{
  WorkspacePath string `json:"workspace_path"`
 }
 if err=json.Unmarshal(provisioned.Result,&bound);err!=nil||bound.WorkspacePath=="" {
  t.Fatalf("managed Workspace provisioning receipt unavailable: %v",err)
 }
 network,err:=engine.InspectNetwork(ctx,runtime.ID)
 if err!=nil||!network.Internal||network.RuntimeID!=runtime.ID||network.Name==""{
  t.Fatalf("managed private network not verified: %+v %v",network,err)
 }
 // Cleanup only the generated, independently verified runtime identity.
 // No images are deleted, no shared networks or user directories are pruned.
 t.Cleanup(func(){
  cleanupCtx,stop:=context.WithTimeout(context.Background(),35*time.Second)
  defer stop()
  observed,inspectErr:=engine.InspectContainer(cleanupCtx,runtime.ID,app.ID)
  if inspectErr==nil && observed.RuntimeID==runtime.ID &&
   observed.ApplicationID==app.ID && observed.IsolationVerified &&
   observed.Image==image && observed.Name!=""{
   if output,removeErr:=exec.CommandContext(cleanupCtx,profile.Executable,
    "rm","-f",observed.Name).CombinedOutput();removeErr!=nil{
    t.Errorf("could not remove owned physical QA container: %v (%s)",removeErr,string(output))
   }
  }
  if current,e:=engine.InspectNetwork(cleanupCtx,runtime.ID);e==nil &&
   current.RuntimeID==runtime.ID && current.Name==network.Name{
   if output,removeErr:=exec.CommandContext(cleanupCtx,profile.Executable,
    "network","rm",network.Name).CombinedOutput();removeErr!=nil{
    t.Errorf("could not remove owned physical QA network: %v (%s)",removeErr,string(output))
   }
  }
 })
 spec:=sandboxrunner.ContainerSpec{
  RuntimeID:runtime.ID,ApplicationID:app.ID,
  Image:image,WorkspacePath:bound.WorkspacePath,
  Command:[]string{"sh","-c","sleep 400"},WorkingDir:"/workspace",
  NetworkInternal:true,
  Limits:sandboxrunner.ResourceLimits{CPUMillis:1000,MemoryMB:256,PIDs:64},
 }
 observed,err:=engine.EnsureContainer(ctx,spec)
 if err!=nil||observed.Status!="running"||!observed.IsolationVerified||
  observed.RuntimeID!=runtime.ID||observed.ApplicationID!=app.ID||
  !observed.ReadOnlyRootFS||observed.NetworkInternal!=true{
  t.Fatalf("physical builder container not independently verified: %+v %v",observed,err)
 }
 // Registered observed state comes only AFTER the real OCI verification.
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE project_runtimes SET status='running',desired_state='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE project_applications SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}

 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:tenant,ProjectID:&p.ID,ProjectWorkspaceID:&build.ID,
  Objective:"Build and publish an immutable software output",
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 _,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:ready.ID,ExpectedRevision:ready.Revision,
  WorkerPrincipalID:ptrPhysical(operator),ActorPrincipalID:ptrPhysical(operator),
 })
 if err!=nil{t.Fatal(err)}

 // Build through the actual governed sandbox adapter rather than writing
 // host fixture bytes. The fixed test script creates a deterministic 2.5MiB
 // output with Python's no-follow file creation and fsync.
 const buildScript=`import os
fd=os.open("/workspace/real-output.bin",os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
try:
 block=bytes(range(256))*2048
 for _ in range(5):
  sent=0
  while sent<len(block):
   sent+=os.write(fd,block[sent:])
 os.fsync(fd)
finally:
 os.close(fd)
print("ONEPANE_PHYSICAL_BUILD_OK")`
 executeInput,_:=json.Marshal(map[string]any{
  "runtime_id":runtime.ID,"application_id":app.ID,
  "command":[]string{"python3","-I","-S","-c",buildScript},
  "required_executables":[]string{"sh","python3"},
 })
 built,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:sandboxrunner.ToolAppExec,WorkspaceID:tenant,
  TaskID:&created.ID,AttemptID:&attempt.ID,Input:executeInput,
 })
 if err!=nil{t.Fatalf("physical approved toolchain execution failed: %v",err)}
 var ran struct{
  Succeeded bool `json:"succeeded"`
  ToolchainPreflight string `json:"toolchain_preflight"`
  Result sandboxrunner.ExecResult `json:"result"`
 }
 if err=json.Unmarshal(built.Result,&ran);err!=nil||!ran.Succeeded||
  ran.ToolchainPreflight!="ready"||ran.Result.ExitCode!=0||
  ran.Result.Stdout!="ONEPANE_PHYSICAL_BUILD_OK" {
  t.Fatalf("physical sandbox build did not pass guarded execution: %+v %v",ran,err)
 }

 // Verify independent host bytes, then traverse the *real* Task authority
 // boundary, OCI chunk reader, publisher, artifact blob and Library version.
 source,err:=os.ReadFile(filepath.Join(bound.WorkspacePath,"real-output.bin"))
 if err!=nil||len(source)!=(5*(512<<10)){
  t.Fatalf("physical compiled output incomplete: size=%d %v",len(source),err)
 }
 expected:=sha256.Sum256(source)
 expectedHex:=hex.EncodeToString(expected[:])
 publishInput,_:=json.Marshal(map[string]any{
  "runtime_id":runtime.ID,"application_id":app.ID,
  "action":"publish_large","path":"real-output.bin",
  "name":"real-output.bin","media_type":"application/octet-stream",
 })
 publish:=func() sandboxrunner.WorkspacePublication {
  t.Helper()
  receipt,err:=adapter.Invoke(ctx,tool.AdapterRequest{
   ToolID:sandboxrunner.ToolAppFilePublish,WorkspaceID:tenant,
   TaskID:&created.ID,AttemptID:&attempt.ID,Input:publishInput,
  })
  if err!=nil{t.Fatalf("physical end-to-end Library publication failed: %v",err)}
  var payload struct{
   Publication sandboxrunner.WorkspacePublication `json:"publication"`
   Succeeded bool `json:"succeeded"`
  }
  if err=json.Unmarshal(receipt.Result,&payload);err!=nil||!payload.Succeeded{
   t.Fatalf("invalid publication receipt: %+v %v",payload,err)
  }
  if bytes.Contains(receipt.Result,source[:64]){
   t.Fatal("raw binary leaked into Agent Tool result")
  }
  return payload.Publication
 }
 first:=publish()
 if first.Version!=1||first.SizeBytes!=int64(len(source))||
  first.ContentHash!=expectedHex||first.SourceWorkspaceID!=build.ID||
  first.ArtifactID==""||first.LibraryAssetID==""{
  t.Fatalf("physical immutable Library receipt differs from observed source: %+v",first)
 }
 if err=artifacts.VerifyContent(ctx,first.ArtifactID);err!=nil{
  t.Fatalf("physical immutable artifact checksum verification failed: %v",err)
 }
 reader,_,err:=artifacts.Open(ctx,first.ArtifactID)
 if err!=nil{t.Fatal(err)}
 stored,readErr:=io.ReadAll(reader)
 _=reader.Close()
 if readErr!=nil||!bytes.Equal(stored,source){
  t.Fatalf("physical OCI file != immutable artifact blob: n=%d %v",len(stored),readErr)
 }
 second:=publish()
 if second.ArtifactID!=first.ArtifactID||
  second.LibraryAssetID!=first.LibraryAssetID||second.Version!=1{
  t.Fatalf("retry duplicated real immutable publication: first=%+v second=%+v",first,second)
 }
 own,err:=projects.WorkspacePublishedOutputs(ctx,p.ID,build.ID)
 if err!=nil||len(own)!=1||own[0].ContentHash!=expectedHex{
  t.Fatalf("source Workspace lacks physical build artifact: %+v %v",own,err)
 }
 foreign,err:=projects.WorkspacePublishedOutputs(ctx,p.ID,sibling.ID)
 if err!=nil||len(foreign)!=0{
  t.Fatalf("other Workspace acquired ungranted physical build: %+v %v",foreign,err)
 }
 foreignAssets,err:=projects.WorkspaceLibraryAssets(ctx,p.ID,sibling.ID,"")
 if err!=nil||len(foreignAssets)!=0{
  t.Fatalf("other Workspace listed ungranted immutable artifact: %+v %v",foreignAssets,err)
 }
 if err=projects.RevokeLibraryAsset(ctx,p.ID,first.LibraryAssetID,build.ID,operator);err!=nil{
  t.Fatalf("could not revoke source Workspace read access: %v",err)
 }
 own,err=projects.WorkspacePublishedOutputs(ctx,p.ID,build.ID)
 if err!=nil||len(own)!=0{
  t.Fatalf("revoked physical Library grant remained accessible: %+v %v",own,err)
 }
 if err=artifacts.VerifyContent(ctx,first.ArtifactID);err!=nil{
  t.Fatalf("revocation incorrectly deleted immutable physical blob: %v",err)
 }
 // A fresh attempt to replay a revoked receipt must not silently restore
 // access merely because the OCI bytes and immutable artifact still exist.
 if _,err=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:sandboxrunner.ToolAppFilePublish,WorkspaceID:tenant,
  TaskID:&created.ID,AttemptID:&attempt.ID,Input:publishInput,
 });err==nil{
  t.Fatal("revoked Workspace grant was reissued by a repeated publish")
 }
 stillRevoked,err:=projects.WorkspacePublishedOutputs(ctx,p.ID,build.ID)
 if err!=nil||len(stillRevoked)!=0{
  t.Fatalf("revoked Workspace regained read grant: %+v %v",stillRevoked,err)
 }

 if _,err=engine.StopRuntime(ctx,runtime.ID);err!=nil{t.Fatal(err)}
 stopped,err:=engine.InspectContainer(ctx,runtime.ID,app.ID)
 if err!=nil||stopped.Status=="running"||stopped.Status=="restarting"{
  t.Fatalf("physical OCI runtime did not stop: %+v %v",stopped,err)
 }
 t.Logf("Verified rootless build -> 2.5 MiB OCI readback -> immutable Library artifact SHA-256 %s; idempotent retry, sibling denial and revocation",expectedHex)
}

func ptrPhysical(v string)*string {return &v}
