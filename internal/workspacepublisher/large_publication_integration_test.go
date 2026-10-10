//go:build integration

package workspacepublisher

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestLargeWorkspaceArtifactPublicationIsImmutableAndNotCrossGranted(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"large-publish.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Software Workshop",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 build,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Build",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 qa,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"QA",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&build.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 image:="ghcr.io/example/go-builder@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Go Compiler",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_runtimes SET status='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_applications SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 store,err:=artifact.NewLocalStore(filepath.Join(t.TempDir(),"blobs"))
 if err!=nil{t.Fatal(err)}
 artifacts:=artifact.NewService(db.SQL(),db,store,clock.Real{})
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&build.ID,
  Objective:"Compile application binary",SchedulingClass:task.ClassNormal})
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 actor:="operator"
 _,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:ready.ID,ExpectedRevision:ready.Revision,
  WorkerPrincipalID:&actor,ActorPrincipalID:&actor})
 if err!=nil{t.Fatal(err)}
 binary:=bytes.Repeat([]byte("compiled-binary-block"),(2<<20)/21+1)
 if len(binary)<=sandboxrunner.WorkspacePublicationLimit{
  t.Fatal("fixture must exceed legacy 256 KiB limit")
 }
 hash:=sha256.Sum256(binary)
 request:=sandboxrunner.WorkspacePublicationRequest{
  WorkspaceID:"tenant",TaskID:ready.ID,AttemptID:attempt.ID,
  RuntimeID:runtime.ID,ApplicationID:app.ID,
  Path:"dist/program.bin",Name:"program.bin",MediaType:"application/octet-stream",
  Content:binary,ContentHash:hex.EncodeToString(hash[:]),
 }
 publisher:=New(db.SQL(),artifacts,projects,"local-node")
 published,err:=publisher.PublishWorkspaceFile(ctx,request)
 if err!=nil{t.Fatalf("publisher rejected valid >256 KiB build binary: %v",err)}
 if published.SizeBytes!=int64(len(binary))||published.ContentHash!=request.ContentHash||published.Version!=1{
  t.Fatalf("large build receipt mismatch: %+v",published)
 }
 if err:=artifacts.VerifyContent(ctx,published.ArtifactID);err!=nil{t.Fatal(err)}
 opened,_,err:=artifacts.Open(ctx,published.ArtifactID)
 if err!=nil{t.Fatal(err)}
 data,err:=io.ReadAll(opened)
 _=opened.Close()
 if err!=nil||len(data)!=len(binary){t.Fatalf("immutable artifact read truncated: %d %v",len(data),err)}
 if !bytes.Equal(data,binary){t.Fatal("published binary differs from approved OCI file")}
 again,err:=publisher.PublishWorkspaceFile(ctx,request)
 if err!=nil||again.ArtifactID!=published.ArtifactID||
  again.LibraryAssetID!=published.LibraryAssetID||again.Version!=1{
  t.Fatalf("large Task retry duplicated immutable artifact: %+v %v",again,err)
 }
 inBuild,err:=projects.WorkspacePublishedOutputs(ctx,p.ID,build.ID)
 if err!=nil||len(inBuild)!=1||inBuild[0].ContentHash!=request.ContentHash{
  t.Fatalf("Build Workspace lost own immutable output: %+v %v",inBuild,err)
 }
 inQA,err:=projects.WorkspacePublishedOutputs(ctx,p.ID,qa.ID)
 if err!=nil||len(inQA)!=0{
  t.Fatalf("QA Workspace accessed ungranted Build binary: %+v %v",inQA,err)
 }
 revised:=request
 revised.Content=append([]byte(nil),binary...)
 revised.Content[0]^=0x01
 nextHash:=sha256.Sum256(revised.Content)
 revised.ContentHash=hex.EncodeToString(nextHash[:])
 next,err:=publisher.PublishWorkspaceFile(ctx,revised)
 if err!=nil||next.LibraryAssetID!=published.LibraryAssetID||
  next.Version!=2||next.ArtifactID==published.ArtifactID{
  t.Fatalf("large changed build lost immutable version lineage: %+v %v",next,err)
 }
 if err:=artifacts.VerifyContent(ctx,published.ArtifactID);err!=nil{
  t.Fatalf("older version corrupted by large overwrite: %v",err)
 }
}
