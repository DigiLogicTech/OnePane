package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type originalNodeSealProjectService struct {
 *projectworkspace.Service
 activePermit projectworkspace.GoalNodeProbeExecutionPermit
}
func(s *originalNodeSealProjectService)ListGoalNodeProbeExecutionPermits(
 ctx context.Context,projectID,goalID string)(
 []projectworkspace.GoalNodeProbeExecutionPermit,error){
 if projectID!=s.activePermit.ProjectID||goalID!=s.activePermit.GoalID{
  return []projectworkspace.GoalNodeProbeExecutionPermit{},nil
 }
 return []projectworkspace.GoalNodeProbeExecutionPermit{s.activePermit},nil
}

// The route test simulates a transactionally admitted record. Actual
// SQLite revocation locking and append-audit behavior are checked by
// projectworkspace integration tests rather than this HTTP auth mock.
func(s *originalNodeSealProjectService)SealGoalNodeProbeExecutionPermit(
 _ context.Context,c projectworkspace.SealGoalNodeProbeExecutionPermitCommand,
 seal func(projectworkspace.GoalNodeProbeExecutionPermit)([]byte,error))([]byte,error){
 p:=s.activePermit
 if c.ProjectID!=p.ProjectID||c.GoalID!=p.GoalID||
  c.PermitID!=p.ID||c.ActorPrincipalID!=p.ApprovedBy{
  return nil,sql.ErrNoRows
 }
 if p.EffectiveExecutionState!="active"{
  return nil,projectworkspace.ErrRevisionConflict
 }
 return seal(p)
}

// API auth and real identity signing; the mock only supplies an already
// admitted effective-state snapshot. The original transactional grant
// service is tested separately against SQLite in integration tests.
func TestHumanOnlyOriginalOnePaneNodePermitSealRequiresPinnedLocalSigner(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/api-server-seal.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Robot','active',1,1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)",
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Server origin",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage("{\"onepane_operations\":{\"version\":1,\"mode\":\"continuous_operations\"}}"),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Node",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,
  Objective:"Admit only original server permits",
  ActorPrincipalID:"operator",
  MonitoringPlanJSON:json.RawMessage("{\"version\":1,\"source_ids\":[\"router\"]}"),
 })
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 original:=projectworkspace.GoalNodeProbeExecutionPermit{
  ID:"human-permit",ProjectID:p.ID,GoalID:goal.ID,
  ProjectWorkspaceID:ws.ID,InstrumentKeyID:"instrument",
  ChallengeID:"challenge",SourceID:"router",DeclaredTargetID:"target",
  TLSPinID:"pin",PeerNodeID:"remote-peer",PeerTLSFingerprint:"sha256:"+strings.Repeat("a",64),
  OriginalChallengeSHA256:"sha256:"+strings.Repeat("b",64),
  InstrumentPublicKeySHA256:"sha256:"+strings.Repeat("c",64),
  OperatorPublicKeySHA256:"sha256:"+strings.Repeat("d",64),
  PlanSHA256:"sha256:"+strings.Repeat("e",64),PlanGeneration:1,
  Status:"approved",EffectiveExecutionState:"active",Revision:1,
  ApprovedBy:"operator",ApprovedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),
 }
 projectAPI:=&originalNodeSealProjectService{Service:svc,activePermit:original}
 ident,err:=nodefederation.EnsureIdentity(t.TempDir(),"onepane-hub")
 if err!=nil{t.Fatal(err)}
 server:=NewServer(projectAPI,nil,goalAPIAuth{})
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+
  "/node-probe-execution-permits/"+original.ID+"/seal"
 hit:=func(s *Server,who string)*httptest.ResponseRecorder{
  request:=httptest.NewRequest("POST",route,nil)
  if who!=""{request.Header.Set("Authorization","Bearer "+who)}
  w:=httptest.NewRecorder();s.Handler().ServeHTTP(w,request)
  return w
 }
 for _,tc:=range []struct{who string;want int}{
  {"",401},{"robot",403},{"outsider",403},
 }{
  result:=hit(server,tc.who)
  if result.Code!=tc.want{
   t.Fatalf("nonhuman %q sealed host network authority: %d %s",
    tc.who,result.Code,result.Body)
  }
 }
 if result:=hit(server,"operator");result.Code!=503{
  t.Fatalf("OnePane invented a signing key when federation unavailable: %d %s",
   result.Code,result.Body)
 }
 federation:=nodefederation.NewService(db.SQL(),db,clock.Real{},
  nodefederation.NodeView{ID:"onepane-hub",Local:true},ident,"",30*time.Second)
 server.SetFederation(federation)
 result:=hit(server,"operator")
 if result.Code!=200{
  t.Fatalf("legitimate human live approval was not server sealed: %d %s",result.Code,result.Body)
 }
 var sealed nodefederation.SealedNodeProbeAuthority
 if err=json.Unmarshal(result.Body.Bytes(),&sealed);err!=nil{t.Fatal(err)}
 confirmed,err:=nodefederation.VerifySealedNodeProbeExecution(sealed,
  ident.Certificate.Certificate[0],"onepane-hub",ident.Fingerprint)
 if err!=nil||confirmed!=original{
  t.Fatalf("original pinned OnePane server approval not independently verified: %v",err)
 }
 projectAPI.activePermit.EffectiveExecutionState="peer_unavailable"
 if result=hit(server,"operator");result.Code!=409{
  t.Fatalf("inactive paired Node received fresh server signing authority: %d",result.Code)
 }
 projectAPI.activePermit.EffectiveExecutionState="active"
 projectAPI.activePermit.ApprovedBy="another-human"
 if result=hit(server,"operator");result.Code!=404{
  t.Fatalf("other human original grant was sealed under current actor: %d",result.Code)
 }
}
