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
 localSeat,err:=council.AddMember(ctx,team.AddMemberCommand{
  TeamID:registered.ID,MemberKind:"agent",DisplayName:"Local analyst",
  RoleName:"Research analyst",CapabilityID:"inference.general",
  ProtocolLevel:"L2",CreatedBy:"human",
  RoutePolicy:json.RawMessage(`{}`),Config:json.RawMessage(`{}`),
 })
 if err!=nil{t.Fatal(err)}
 otherSeat,err:=council.AddMember(ctx,team.AddMemberCommand{
  TeamID:registered.ID,MemberKind:"agent",DisplayName:"Independent analyst",
  RoleName:"Research reviewer",CapabilityID:"inference.general",
  ProtocolLevel:"L2",CreatedBy:"human",
  RoutePolicy:json.RawMessage(`{}`),Config:json.RawMessage(`{}`),
 })
 if err!=nil{t.Fatal(err)}
 session,err:=council.StartSession(ctx,team.StartSessionCommand{
  TaskID:job.ID,TeamID:registered.ID,CreatedBy:"human",ExecutionMode:"council",
 })
 if err!=nil{t.Fatal(err)}
 // The picker is strictly tenant-, role-, Project- and Workspace-scoped.
 eligible,err:=svc.EligibleResearchEvidenceSessions(ctx,p.ID,world.ID,"human")
 if err!=nil||len(eligible)!=1||eligible[0].ID!=session.ID||
  eligible[0].TaskID!=job.ID||eligible[0].TeamID!=registered.ID{
  t.Fatalf("expected exact pre-round Research session, got %+v: %v",eligible,err)
 }
 for _,tc:=range []struct{ project,workspace,actor string }{
  {p.ID,research.ID,"human"},
  {p.ID,world.ID,"agent"},
  {p.ID,world.ID,"unknown-human"},
 }{
  candidates,e:=svc.EligibleResearchEvidenceSessions(ctx,tc.project,tc.workspace,tc.actor)
  if e!=nil||len(candidates)!=0{
   t.Fatalf("unauthorised picker disclosure for %+v: %+v %v",tc,candidates,e)
  }
 }
 history,err:=svc.FrozenResearchEvidenceSessions(ctx,p.ID,world.ID,"human")
 if err!=nil||len(history)!=0{
  t.Fatalf("unapproved Research session appeared in frozen history: %+v %v",history,err)
 }
 requested:=[]WorkspaceEvidenceSelection{{AssetID:a.ID,Version:1}}
 frozen,err:=svc.FreezeResearchEvidence(ctx,session.ID,"human",requested)
 if err!=nil{t.Fatal(err)}
 eligible,err=svc.EligibleResearchEvidenceSessions(ctx,p.ID,world.ID,"human")
 if err!=nil||len(eligible)!=0{
  t.Fatalf("frozen Research session still listed: %+v %v",eligible,err)
 }
 history,err=svc.FrozenResearchEvidenceSessions(ctx,p.ID,world.ID,"human")
 if err!=nil||len(history)!=1||history[0].ID!=session.ID||
  history[0].TaskID!=job.ID||history[0].ApprovedAt<1{
  t.Fatalf("exact frozen Research approval missing: %+v %v",history,err)
 }
 for _,tc:=range []struct{workspace,actor string}{
  {research.ID,"human"},{world.ID,"agent"},{world.ID,"unknown-human"},
 }{
  candidates,e:=svc.FrozenResearchEvidenceSessions(ctx,p.ID,tc.workspace,tc.actor)
  if e!=nil||len(candidates)!=0{
   t.Fatalf("frozen history leaked to %+v: %+v %v",tc,candidates,e)
  }
 }
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
 // A human must separately name exact runnable seats. This is an
 // inactive local-only sharing intent, NEVER a Council context attachment.
 if _,err=svc.ApproveResearchSeatEvidence(ctx,session.ID,"agent",
  []string{localSeat.ID});err==nil{
  t.Fatal("agent created Research seat evidence approval")
 }
 if _,err=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{"forged-other-workspace-member"});err==nil{
  t.Fatal("unfrozen/foreign Research seat accepted")
 }
 first,err:=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{localSeat.ID})
 if err!=nil{t.Fatal(err)}
 if first.State!="approved"||first.Policy!="local_only"||
  first.ModelVisible||first.DispatchEnabled||len(first.SeatIDs)!=1||
  first.SeatIDs[0]!=localSeat.ID||
  first.EvidenceManifestSHA256!=frozen.Evidence.ManifestSHA256{
  t.Fatalf("Research seat approval widened scope or omitted provenance: %+v",first)
 }
 identical,err:=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{localSeat.ID})
 if err!=nil||identical.ApprovedAt!=first.ApprovedAt{
  t.Fatalf("same seat approval was not idempotent: %+v %v",identical,err)
 }
 if _,err=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{otherSeat.ID});err==nil{
  t.Fatal("conflicting Research seat sharing plan replaced the frozen approval")
 }
 if _,err=svc.ResearchSeatEvidenceApproval(ctx,session.ID,"agent");err==nil{
  t.Fatal("agent could inspect frozen seat evidence sharing intent")
 }
 // Revocation must remain available even if the source Workspace Library
 // grant was withdrawn after the human sharing approval.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_library_grants
  SET enabled=0 WHERE asset_id=? AND project_workspace_id=?`,
  a.ID,world.ID);err!=nil{t.Fatal(err)}
 revoked,err:=svc.RevokeResearchSeatEvidence(ctx,session.ID,"human")
 if _,restoreErr:=db.SQL().ExecContext(ctx,`UPDATE workspace_library_grants
  SET enabled=1 WHERE asset_id=? AND project_workspace_id=?`,
  a.ID,world.ID);restoreErr!=nil{t.Fatal(restoreErr)}
 if err!=nil||revoked.State!="revoked"||revoked.RevokedBy==nil||
  *revoked.RevokedBy!="human"||revoked.DispatchEnabled{
  t.Fatalf("seat evidence revocation missing: %+v %v",revoked,err)
 }
 if _,err=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{localSeat.ID});err==nil{
  t.Fatal("revoked Research sharing consent was silently restored")
 }
 if _,err=svc.RevokeResearchSeatEvidence(ctx,session.ID,"agent");err==nil{
  t.Fatal("agent could revoke human Research seat approval")
 }
 if _,err=svc.FreezeResearchEvidence(ctx,session.ID,"agent",requested);err==nil{
  t.Fatal("agent could freeze its own Research evidence")
 }
 if _,err=svc.ResearchEvidence(ctx,session.ID,"agent");err==nil{
  t.Fatal("agent could list frozen Research evidence")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE team_sessions SET round_number=1
  WHERE id=?`,session.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{otherSeat.ID});err==nil{
  t.Fatal("late-round Research seat approval accepted")
 }
 if _,err=svc.FreezeResearchEvidence(ctx,session.ID,"human",requested);err==nil{
  t.Fatal("Research metadata changed after first round")
 }
 if _,err=svc.ResearchEvidence(ctx,session.ID,"human");err!=nil{
  t.Fatalf("operator could not inspect prior metadata after round: %v",err)
 }
 // Separate Research Workspace remains isolated from World Library,
 // even if its Task and Council belong to the same parent Project.
 other,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&research.ID,
  Objective:"Independent scoped evidence",
 })
 if err!=nil{t.Fatal(err)}
 otherSession,err:=council.StartSession(ctx,team.StartSessionCommand{
  TaskID:other.ID,TeamID:registered.ID,CreatedBy:"human",ExecutionMode:"council",
 })
 if err!=nil{t.Fatal(err)}
 eligible,err=svc.EligibleResearchEvidenceSessions(ctx,p.ID,research.ID,"human")
 if err!=nil||len(eligible)!=1||eligible[0].ID!=otherSession.ID{
  t.Fatalf("sibling Workspace picker mixed scoped Council sessions: %+v %v",eligible,err)
 }
 // Revoked/suspended human access and archived Team rows must disappear
 // from discovery immediately, not remain valid via a stale picker snapshot.
 checkHidden:=func(reason string){
  t.Helper()
  candidates,e:=svc.EligibleResearchEvidenceSessions(ctx,p.ID,research.ID,"human")
  if e!=nil||len(candidates)!=0{
   t.Fatalf("%s leaked eligible Research session: %+v %v",reason,candidates,e)
  }
  frozenCandidates,e:=svc.FrozenResearchEvidenceSessions(ctx,p.ID,world.ID,"human")
  if e!=nil||len(frozenCandidates)!=0{
   t.Fatalf("%s disclosed frozen Research session: %+v %v",reason,frozenCandidates,e)
  }
  if _,e=svc.ResearchSeatEvidenceApproval(ctx,session.ID,"human");e==nil{
   t.Fatalf("%s disclosed revoked Research seat approval",reason)
  }
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships SET status='suspended'
  WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 checkHidden("suspended tenant membership")
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_memberships SET status='active'
  WHERE workspace_id='tenant' AND principal_id='human'`);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE principals SET status='disabled'
  WHERE id='human'`);err!=nil{t.Fatal(err)}
 checkHidden("disabled human principal")
 if _,err=db.SQL().ExecContext(ctx,`UPDATE principals SET status='active'
  WHERE id='human'`);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE teams SET status='archived'
  WHERE id=?`,registered.ID);err!=nil{t.Fatal(err)}
 checkHidden("archived Council Team")
 if _,err=db.SQL().ExecContext(ctx,`UPDATE teams SET status='active'
  WHERE id=?`,registered.ID);err!=nil{t.Fatal(err)}
 eligible,err=svc.EligibleResearchEvidenceSessions(ctx,p.ID,research.ID,"human")
 if err!=nil||len(eligible)!=1||eligible[0].ID!=otherSession.ID{
  t.Fatalf("restored scope did not restore eligible Research session: %+v %v",eligible,err)
 }
 if _,err=db.SQL().ExecContext(ctx,"UPDATE team_sessions SET round_number=1 WHERE id=?",otherSession.ID);err!=nil{
  t.Fatal(err)
 }
 eligible,err=svc.EligibleResearchEvidenceSessions(ctx,p.ID,research.ID,"human")
 if err!=nil||len(eligible)!=0{
  t.Fatalf("post-round Research session remained selectable: %+v %v",eligible,err)
 }
 if _,err=svc.FreezeResearchEvidence(ctx,otherSession.ID,"human",requested);
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("Research Workspace inherited World evidence: %v",err)
 }
 // A recorded approval never becomes a perpetual read lease.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_library_grants
  SET enabled=0 WHERE asset_id=? AND project_workspace_id=?`,a.ID,world.ID);
  err!=nil{t.Fatal(err)}
 if _,err=svc.ResearchEvidence(ctx,session.ID,"human");
  !errors.Is(err,ErrCrossWorkspace){
  t.Fatalf("revoked evidence selection was still readable: %v",err)
 }
}
