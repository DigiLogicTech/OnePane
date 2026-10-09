//go:build integration

package workspacepublisher

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestPublishingOCIArtifactRequiresActiveOwnedTaskAndOnlyGrantsSourceWorkspace(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"publish.db"))
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
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Game",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 runtime,err:=projects.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{ProjectID:p.ID,ProjectWorkspaceID:&world.ID,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 image:="ghcr.io/digilogic/godot@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
 app,err:=projects.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{RuntimeID:runtime.ID,Name:"World Builder",SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_runtimes SET status='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_applications SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 artifactStore,err:=artifact.NewLocalStore(filepath.Join(t.TempDir(),"blobs"))
 if err!=nil{t.Fatal(err)}
 artifacts:=artifact.NewService(db.SQL(),db,artifactStore,clock.Real{})
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&world.ID,
  Objective:"Export verified world map",SchedulingClass:task.ClassNormal,
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 actor:="operator"
 _,attempt,err:=tasks.Start(ctx,task.StartCommand{
  TaskID:ready.ID,ExpectedRevision:ready.Revision,WorkerPrincipalID:&actor,ActorPrincipalID:&actor,
 })
 if err!=nil{t.Fatal(err)}
 content:=[]byte("world-asset-content")
 hash:=sha256.Sum256(content)
 request:=sandboxrunner.WorkspacePublicationRequest{
  WorkspaceID:"tenant",TaskID:ready.ID,AttemptID:attempt.ID,
  RuntimeID:runtime.ID,ApplicationID:app.ID,
  Path:"maps/world.txt",Name:"World v1",MediaType:"text/plain",
  Content:content,ContentHash:hex.EncodeToString(hash[:]),
 }
 publisher:=New(db.SQL(),artifacts,projects,"local-node")
 for _,invalid:=range []struct{name string;modify func(*sandboxrunner.WorkspacePublicationRequest)}{
  {"wrong_task",func(r *sandboxrunner.WorkspacePublicationRequest){r.TaskID="other"}},
  {"wrong_attempt",func(r *sandboxrunner.WorkspacePublicationRequest){r.AttemptID="other"}},
  {"wrong_tenant",func(r *sandboxrunner.WorkspacePublicationRequest){r.WorkspaceID="different"}},
  {"wrong_app",func(r *sandboxrunner.WorkspacePublicationRequest){r.ApplicationID="other"}},
  {"wrong_hash",func(r *sandboxrunner.WorkspacePublicationRequest){r.ContentHash="invalid"}},
  {"foreign_path",func(r *sandboxrunner.WorkspacePublicationRequest){r.Path="../outside"}},
  {"secret_git",func(r *sandboxrunner.WorkspacePublicationRequest){r.Path=".git/config"}},
 }{
  t.Run(invalid.name,func(t *testing.T){
   c:=request
   invalid.modify(&c)
   if got,err:=publisher.PublishWorkspaceFile(ctx,c);err==nil{
    t.Fatalf("foreign/unsafe source published: %+v",got)
   }
  })
 }
 published,err:=publisher.PublishWorkspaceFile(ctx,request)
 if err!=nil{t.Fatal(err)}
 if published.Version!=1||published.SourceWorkspaceID!=world.ID||
  published.ContentHash!=request.ContentHash||published.SizeBytes!=int64(len(content)){
  t.Fatalf("published receipt mismatch: %+v",published)
 }
 if err:=artifacts.VerifyContent(ctx,published.ArtifactID);err!=nil{t.Fatal(err)}
 repeated,err:=publisher.PublishWorkspaceFile(ctx,request)
 if err!=nil{t.Fatalf("duplicate publication should reuse verified receipt: %v",err)}
 if repeated.ArtifactID!=published.ArtifactID||repeated.LibraryAssetID!=published.LibraryAssetID||
  repeated.Version!=published.Version{
  t.Fatalf("replayed Task publication created duplicate asset: first=%+v replay=%+v",published,repeated)
 }
 var ledgerCount int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM workspace_file_publications
 WHERE task_id=? AND relative_path=? AND content_hash=? AND status='complete'`,
 request.TaskID,request.Path,request.ContentHash).Scan(&ledgerCount);err!=nil||ledgerCount!=1{
  t.Fatalf("idempotency record not uniquely completed: count=%d err=%v",ledgerCount,err)
 }
 // An interrupted publication may have written a blob or Library version.
 // Do not re-run an ambiguous external side effect until recovery verifies it.
 interrupted:=request
 interrupted.Path="maps/unknown-outcome.txt"
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspace_file_publications(
 task_id,project_id,project_workspace_id,runtime_id,application_id,
 relative_path,content_hash,status,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,'in_progress',?,?)`,
 interrupted.TaskID,p.ID,world.ID,runtime.ID,app.ID,interrupted.Path,interrupted.ContentHash,now,now);err!=nil{t.Fatal(err)}
 if _,err:=publisher.PublishWorkspaceFile(ctx,interrupted);err!=ErrPublicationRecoveryRequired{
  t.Fatalf("uncertain publication outcome silently retried: %v",err)
 }
 source,err:=projects.WorkspaceLibraryAssets(ctx,p.ID,world.ID,"")
 if err!=nil||len(source)!=1||source[0].ID!=published.LibraryAssetID{
  t.Fatalf("World did not receive its own content-addressed asset: %+v %v",source,err)
 }
 notGranted,err:=projects.WorkspaceLibraryAssets(ctx,p.ID,story.ID,"")
 if err!=nil||len(notGranted)!=0{
  t.Fatalf("Story acquired ungranted World artifact: %+v %v",notGranted,err)
 }
}
