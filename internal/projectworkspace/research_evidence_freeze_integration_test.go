//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/team"
)

func TestResearchSessionFrozenEvidenceApprovalAndRevocation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/research-evidence.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Human reviewer','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','agent','Agent','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','agent','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Data-isolated Council",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 views,err:=svc.WorkspaceViews(ctx,p.ID)
 if err!=nil||len(views)!=1{t.Fatalf("default Workspace: %v",err)}
 world:=views[0]
 research,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Research",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 a,err:=svc.ImportLibraryAsset(ctx,ImportLibraryAssetCommand{
  ProjectID:p.ID,SourceWorkspaceID:world.ID,Name:"World.md",
  MIMEType:"text/markdown",ArtifactID:"private-managed-artifact",
  ContentHash:strings.Repeat("a",64),SizeBytes:40,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 b,err:=svc.ImportLibraryAsset(ctx,ImportLibraryAssetCommand{
  ProjectID:p.ID,SourceWorkspaceID:world.ID,Name:"Secret.md",
  MIMEType:"text/markdown",ArtifactID:"private-asset-two",
  ContentHash:strings.Repeat("b",64),SizeBytes:55,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 job,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&world.ID,
  Objective:"Compare hypotheses independently",
 })
 if err!=nil{t.Fatal(err)}
 council:=team.NewService(db.SQL(),db,clock.Real{},tasks)
 registered,err:=council.CreateTeam(ctx,team.CreateTeamCommand{
  WorkspaceID:"tenant",Name:"Research Council",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 _,err=council.UpdateConfiguration(ctx,team.UpdateConfigurationCommand{
  TeamID:registered.ID,ExpectedRevision:registered.Revision,ActorPrincipalID:"human",
  Configuration:json.RawMessage(`{"research_mode":true,"research":{"critique_rounds":2}}`),
 })
 if err!=nil{t.Fatal(err)}
 session,err:=council.StartSession(ctx,team.StartSessionCommand{
  TaskID:job.ID,TeamID:registered.ID,CreatedBy:"human",ExecutionMode:"council",
 })
 if err!=nil{t.Fatal(err)}
 requested:=[]WorkspaceEvidenceSelection{{AssetID:a.ID,Version:1}}
 frozen,err:=svc.FreezeResearchEvidence(ctx,session.ID,"human",requested)
 if err!=nil{t.Fatal(err)}
 if frozen.ProjectID!=p.ID||frozen.ProjectWorkspaceID!=world.ID||
  frozen.ResearchManifestSHA256==""||frozen.Evidence.ManifestSHA256==""||
  len(frozen.Evidence.Entries)!=1||frozen.ApprovedBy!="human"{
  t.Fatalf("incomplete scope or provenance: %+v",frozen)
 }
 raw,_:=json.Marshal(frozen)
 if strings.Contains(string(raw),"private-managed-artifact")||
  strings.Contains(string(raw),"storage_uri")||strings.Contains(string(raw),"content_bytes"){
  t.Fatalf("Research selection leaked storage or contents: %s",raw)
 }
 retry,err:=svc.FreezeResearchEvidence(ctx,session.ID,"human",requested)
 if err!=nil||retry.Evidence.ManifestSHA256!=frozen.Evidence.ManifestSHA256{
  t.Fatalf("same approval was not idempotent: %+v %v",retry,err)
 }
 if _,err=svc.FreezeResearchEvidence(ctx,session.ID,"human",
  []WorkspaceEvidenceSelection{{AssetID:b.ID,Version:1}});!errors.Is(err,ErrInvalidCommand){
  t.Fatalf("conflicting human approval replaced a frozen selection: %v",err)
 }
 if _,err=svc.FreezeResearchEvidence(ctx,session.ID,"agent",requested);err==nil{
  t.Fatal("agent could freeze its own Research evidence")
 }
 if _,err=svc.ResearchEvidence(ctx,session.ID,"agent");err==nil{
  t.Fatal("agent could list frozen Research evidence")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE team_sessions SET round_number=1
  WHERE id=?`,session.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.FreezeResearchEvidence(ctx,session.ID,"human",requested);err==nil{
  t.Fatal("Research metadata changed after first round")
 }
 if _,err=svc.ResearchEvidence(ctx,session.ID,"human");err!=nil{
  t.Fatalf("operator could not inspect prior metadata after round: %v",err)
 }
 _=research
}
