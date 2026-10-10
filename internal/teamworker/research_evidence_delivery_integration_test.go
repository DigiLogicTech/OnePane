package teamworker

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/team"
)

func TestResearchSeatVerifiedTextDeliveryUsesRealLibraryAndArtifactGrants(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/research-delivery.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,query:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Research reviewer','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,query,now,now);err!=nil{t.Fatal(err)}
 }
 projectService:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projectService.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Research evidence",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 workspaces,err:=projectService.WorkspaceViews(ctx,project.ID)
 if err!=nil||len(workspaces)!=1{t.Fatalf("workspaces: %v",err)}
 workspaceID:=workspaces[0].ID
 store,err:=artifact.NewLocalStore(t.TempDir()+"/blobs")
 if err!=nil{t.Fatal(err)}
 artifacts:=artifact.NewService(db.SQL(),db,store,clock.Real{})
 human:="human"
 secret:="# Research notes\nAn untrusted Library paragraph with a private marker.\n"
 blob,err:=artifacts.Create(ctx,artifact.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,MediaType:"text/markdown",
  CreatedBy:&human,Status:artifact.StatusActive,
  Label:policy.DataLabel{WorkspaceID:"tenant",
   Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction},
 },strings.NewReader(secret))
 if err!=nil{t.Fatal(err)}
 asset,err:=projectService.ImportLibraryAsset(ctx,projectworkspace.ImportLibraryAssetCommand{
  ProjectID:project.ID,SourceWorkspaceID:workspaceID,
  Name:"research.md",MIMEType:"text/markdown",
  ArtifactID:blob.ID,ContentHash:blob.ContentHash,SizeBytes:blob.SizeBytes,
  ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 taskService:=task.NewService(db.SQL(),db,clock.Real{})
 job,err:=taskService.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&workspaceID,
  Objective:"Independent comparison of hypotheses",
 })
 if err!=nil{t.Fatal(err)}
 council:=team.NewService(db.SQL(),db,clock.Real{},taskService)
 t0,err:=council.CreateTeam(ctx,team.CreateTeamCommand{
  WorkspaceID:"tenant",Name:"Local-only Council",CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 _,err=council.UpdateConfiguration(ctx,team.UpdateConfigurationCommand{
  TeamID:t0.ID,ExpectedRevision:t0.Revision,ActorPrincipalID:"human",
  Configuration:json.RawMessage(`{"research_mode":true,"research":{"critique_rounds":2}}`),
 })
 if err!=nil{t.Fatal(err)}
 first,err:=council.AddMember(ctx,team.AddMemberCommand{
  TeamID:t0.ID,MemberKind:"agent",DisplayName:"Local seat one",
  RoleName:"Researcher",CapabilityID:"inference.general",ProtocolLevel:"L2",
  CreatedBy:"human",RoutePolicy:json.RawMessage(`{}`),Config:json.RawMessage(`{}`),
 })
 if err!=nil{t.Fatal(err)}
 second,err:=council.AddMember(ctx,team.AddMemberCommand{
  TeamID:t0.ID,MemberKind:"agent",DisplayName:"Local seat two",
  RoleName:"Critic",CapabilityID:"inference.general",ProtocolLevel:"L2",
  CreatedBy:"human",RoutePolicy:json.RawMessage(`{}`),Config:json.RawMessage(`{}`),
 })
 if err!=nil{t.Fatal(err)}
 session,err:=council.StartSession(ctx,team.StartSessionCommand{
  TaskID:job.ID,TeamID:t0.ID,CreatedBy:"human",ExecutionMode:"council",
 })
 if err!=nil{t.Fatal(err)}
 frozen,err:=projectService.FreezeResearchEvidence(ctx,session.ID,"human",
  []projectworkspace.WorkspaceEvidenceSelection{{AssetID:asset.ID,Version:1}})
 if err!=nil{t.Fatal(err)}
 approved,err:=projectService.ApproveResearchSeatEvidence(ctx,session.ID,"human",
  []string{first.ID,second.ID})
 if err!=nil||approved.State!="approved"{t.Fatalf("approval: %+v %v",approved,err)}
 manifest,err:=council.SessionManifest(ctx,session.ID)
 if err!=nil{t.Fatal(err)}
 worker:=&Service{db:db.SQL(),tx:db,clock:clock.Real{},artifacts:artifacts}
 consent,err:=worker.consentForSeat(ctx,"tenant",session.ID,first.ID,manifest.SnapshotSHA256)
 if err!=nil||!consent.Active{t.Fatalf("seat consent failed: %+v %v",consent,err)}
 input,err:=worker.loadResearchSeatEvidence(ctx,"tenant",session.ID,first.ID,
  manifest.SnapshotSHA256,consent)
 if err!=nil{t.Fatal(err)}
 if len(input.Sections)!=1||input.DocumentCount!=1||
  input.ManifestSHA256!=frozen.Evidence.ManifestSHA256||
  input.Bytes!=int64(len(secret))||
  input.Sections[0].Trust!="UNVERIFIED_DERIVED"||
  input.Sections[0].Authoritative{
  t.Fatalf("local seat did not receive exact untrusted frozen context: %+v",input)
 }
 var decoded struct{
  Text string `json:"text"`
  Trust string `json:"trust"`
  SHA256 string `json:"sha256"`
 }
 if err=json.Unmarshal(input.Sections[0].Content,&decoded);err!=nil||
  decoded.Text!=secret||decoded.Trust!="untrusted_library_content"||
  decoded.SHA256!=blob.ContentHash{
  t.Fatalf("verified local context does not preserve exact text/hash: %+v %v",decoded,err)
 }
 // A protected response and its turn state are committed only together.
 // Exercise actual Research turn requests and a managed system principal.
 if err=worker.EnsureSystemPrincipal(ctx);err!=nil{t.Fatal(err)}
 turns,err:=council.RequestRound(ctx,team.RequestRoundCommand{
  SessionID:session.ID,RequestedByPrincipalID:"human",
  ResearchPhase:team.ResearchPhaseIndependent,
 })
 if err!=nil||len(turns)!=2{t.Fatalf("Research round creation: %d %v",len(turns),err)}
 bySeat:=map[string]team.TurnRequest{}
 for _,turn:=range turns{
  bySeat[turn.MemberID]=turn
  if _,err=db.SQL().ExecContext(ctx,`UPDATE team_turn_requests
   SET status='running' WHERE id=?`,turn.ID);err!=nil{t.Fatal(err)}
 }
 makeResponse:=func(label string)json.RawMessage{
  raw,e:=json.Marshal(map[string]any{
   "text":label,"research_provenance":map[string]any{
    "research_evidence_attached":true,
    "session_manifest_sha256":manifest.SnapshotSHA256,
    "research_evidence_manifest_sha256":frozen.Evidence.ManifestSHA256,
   },
  })
  if e!=nil{t.Fatal(e)}
  return raw
 }
 firstTurn:=bySeat[first.ID]
 secondTurn:=bySeat[second.ID]
 if _,err=worker.completeProtectedResearchTurn(ctx,"tenant",session.ID,firstTurn.ID,
  first.ID,manifest.SnapshotSHA256,frozen.Evidence.ManifestSHA256,
  1,makeResponse("first protected local answer"));err!=nil{t.Fatal(err)}
 if _,err=worker.completeProtectedResearchTurn(ctx,"tenant",session.ID,firstTurn.ID,
  first.ID,manifest.SnapshotSHA256,frozen.Evidence.ManifestSHA256,
  1,makeResponse("replay"));err==nil{
  t.Fatal("completed protected turn was replayed")
 }
 assertMessages:=func(expected int){
  t.Helper()
  var count int
  if e:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM team_messages
   WHERE session_id=?`,session.ID).Scan(&count);e!=nil||count!=expected{
   t.Fatalf("unexpected protected messages %d wanted %d: %v",count,expected,e)
  }
 }
 assertMessages(1)
 // The source grant governs every subsequent read, even when its original
 // metadata is approved and content-addressed.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_library_grants
  SET enabled=0 WHERE project_workspace_id=? AND asset_id=?`,workspaceID,asset.ID);err!=nil{t.Fatal(err)}
 if leaked,e:=worker.loadResearchSeatEvidence(ctx,"tenant",session.ID,first.ID,
  manifest.SnapshotSHA256,consent);e==nil||len(leaked.Sections)>0{
  t.Fatal("revoked Workspace Library grant still released private evidence")
 }
 if _,e:=worker.completeProtectedResearchTurn(ctx,"tenant",session.ID,secondTurn.ID,
  second.ID,manifest.SnapshotSHA256,frozen.Evidence.ManifestSHA256,
  1,makeResponse("must not persist after grant withdrawal"));e==nil{
  t.Fatal("withdrawn Library grant permitted protected reply persistence")
 }
 assertMessages(1)
 if _,err=db.SQL().ExecContext(ctx,`UPDATE workspace_library_grants
  SET enabled=1 WHERE project_workspace_id=? AND asset_id=?`,workspaceID,asset.ID);err!=nil{t.Fatal(err)}
 if _,err=projectService.RevokeResearchSeatEvidence(ctx,session.ID,"human");err!=nil{t.Fatal(err)}
 if _,e:=worker.completeProtectedResearchTurn(ctx,"tenant",session.ID,secondTurn.ID,
  second.ID,manifest.SnapshotSHA256,frozen.Evidence.ManifestSHA256,
  1,makeResponse("must not persist after human consent revocation"));e==nil{
  t.Fatal("revoked human consent permitted protected reply persistence")
 }
 assertMessages(1)
 // A previously protected, paused Council cannot be resurrected by
 // either provider-timer auto retry or an operator retry after revocation.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE team_sessions
  SET status='paused_for_deliberation' WHERE id=?`,session.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE team_turn_requests
  SET status='blocked',attempt_count=1,retry_after=?,response_message_id=NULL
  WHERE id=?`,clock.Real{}.UnixMilli()-1,secondTurn.ID);err!=nil{t.Fatal(err)}
 for _,retry:=range []team.RetryResearchRoundCommand{
  {SessionID:session.ID,RequestedByPrincipalID:"human",Automatic:false},
  {SessionID:session.ID,RequestedByPrincipalID:"human",Automatic:true},
 }{
  if _,err=council.RetryResearchRound(ctx,retry);err==nil{
   t.Fatalf("revoked protected Council retry was accepted: %+v",retry)
  }
 }
 var status string
 if err=db.SQL().QueryRowContext(ctx,`SELECT status FROM team_sessions WHERE id=?`,
  session.ID).Scan(&status);err!=nil||status!="paused_for_deliberation"{
  t.Fatalf("revoked Council resumed despite retry denial: %s %v",status,err)
 }
 assertMessages(1)
 if _,err=worker.consentForSeat(ctx,"tenant",session.ID,second.ID,manifest.SnapshotSHA256);err==nil{
  t.Fatal("revoked Council silently restarted without local evidence protection")
 }
 if leaked,e:=worker.loadResearchSeatEvidence(ctx,"tenant",session.ID,first.ID,
  manifest.SnapshotSHA256,consent);e==nil||len(leaked.Sections)>0{
  t.Fatal("previously captured approval bypassed permanent revocation")
 }
}
