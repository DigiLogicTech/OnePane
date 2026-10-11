package projectorchestrator

import (
 "context"
 "encoding/json"
 "database/sql"
 "path/filepath"
 "testing"

 _ "modernc.org/sqlite"

 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

type orchestratorRouteCapture struct {
 calls int
 request scheduler.RouteRequest
}
func (m *orchestratorRouteCapture) Route(_ context.Context,req scheduler.RouteRequest)(scheduler.Decision,error){
 m.calls++
 m.request=req
 return scheduler.Decision{},scheduler.ErrNoEligibleCandidate
}

func TestProjectOrchestratorNeverSendsProjectSnapshotToUnapprovedRemoteOrUntestedModel(t *testing.T){
 db:=testReasoningPolicyDB(t)
 recorder:=&orchestratorRouteCapture{}
 svc:=&Service{db:db,scheduler:recorder}
 _,_,_,err:=svc.reason(context.Background(),"tenant","project","world","operator",
  "inspect my Project",json.RawMessage(`{"private":"project-context"}`))
 if err==nil{t.Fatal("missing local qualified model should block reasoning")}
 if recorder.calls!=1 {t.Fatalf("unexpected route count %d",recorder.calls)}
 req:=recorder.request
 if !req.LocalOnly||!req.RequireZeroIncrementalCost||req.AllowUntested||
  req.AllowSubscriptionUsage||req.AllowPotentialMonetarySpend||
  !req.AllowLimited{
  t.Fatalf("unsafe route admitted for private Project context: %+v",req)
 }
 if req.WorkspaceID!="tenant"||req.DataLabel.WorkspaceID!="tenant"||
  req.DataLabel.Confidentiality!=policy.ConfidentialityInternal{
  t.Fatalf("Project context routing escaped tenant and confidentiality: %+v",req)
 }
 if len(req.AllowedKinds)!=1||req.AllowedKinds[0]!=scheduler.CandidateModel||
  req.CapabilityID!="inference.general"||req.RoleName!="project-orchestrator"||
  req.ProtocolLevel!="L1"{
  t.Fatalf("unexpected Project reasoning route: %+v",req)
 }
}


func testReasoningPolicyDB(t *testing.T)*sql.DB {
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"reasoning-policy.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){db.Close()})
 for _,q:=range []string{
  `CREATE TABLE workspaces(id TEXT PRIMARY KEY,status TEXT NOT NULL)`,
  `CREATE TABLE projects(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,status TEXT NOT NULL,project_policy_json TEXT NOT NULL)`,
  `CREATE TABLE project_workspaces(id TEXT PRIMARY KEY,project_id TEXT NOT NULL,status TEXT NOT NULL,ai_settings_json TEXT NOT NULL)`,
  `INSERT INTO workspaces VALUES('tenant','active'),('foreign','active')`,
  `INSERT INTO projects VALUES('project','tenant','active','{}')`,
  `INSERT INTO project_workspaces VALUES('world','project','active','{}')`,
  `INSERT INTO project_workspaces VALUES('story','project','active','{"feature_policy":{"ProjectAI":"disabled"}}')`,
 }{if _,err=db.Exec(q);err!=nil{t.Fatal(err)}}
 return db
}

func TestProjectReasoningRejectsDeniedProjectAndSiblingScopeBeforeRouting(t *testing.T){
 db:=testReasoningPolicyDB(t)
 capture:=&orchestratorRouteCapture{}
 svc:=&Service{db:db,scheduler:capture}
 snapshot:=json.RawMessage(`{"project":{"id":"project"},"selected_project_workspace_id":"world"}`)
 for _,caseID:=range []struct{tenant,project,ws string}{
  {"tenant","project","story"},{"foreign","project","world"},
  {"tenant","missing","world"},{"tenant","project","unknown"},
 }{
  if _,_,_,err:=svc.reason(context.Background(),caseID.tenant,caseID.project,caseID.ws,"operator","inspect",snapshot);err==nil{
   t.Fatalf("invalid Project reasoning scope admitted: %+v",caseID)
  }
 }
 if capture.calls!=0{t.Fatalf("unauthorised scope reached model router %d times",capture.calls)}
 if _,err:=db.Exec(`UPDATE projects SET project_policy_json='{"feature_policy":{"Assistant":"disabled"}}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireProjectReasoning(context.Background(),"tenant","project","world");err==nil{
  t.Fatal("Project Assistant disabled flag ignored")
 }
 if _,err:=db.Exec(`UPDATE projects SET project_policy_json='{"feature_policy":null}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireProjectReasoning(context.Background(),"tenant","project","world");err==nil{
  t.Fatal("invalid policy object allowed")
 }
}


// A scheduler candidate may have been selected while an operator revoked the
// Workspace policy. The reasoning service must stop before inference dispatch.
type policyRevokingModelRoute struct {
 db *sql.DB
 calls int
}
func(m *policyRevokingModelRoute)Route(ctx context.Context, _ scheduler.RouteRequest)(scheduler.Decision,error){
 m.calls++
 _,err:=m.db.ExecContext(ctx,`UPDATE project_workspaces
  SET ai_settings_json='{"feature_policy":{"ProjectAI":"disabled"}}'
  WHERE id='world'`)
 if err!=nil{return scheduler.Decision{},err}
 return scheduler.Decision{
  Selected:&scheduler.RankedCandidate{Candidate:scheduler.Candidate{
   ID:"fake-host-model",Kind:scheduler.CandidateModel,Local:true,
   CostClass:scheduler.CostLocal,HardZeroIncrementalCost:true,
  }},
 },nil
}
func TestRevokedProjectReasoningStopsBeforeInferenceTransport(t *testing.T){
 db:=testReasoningPolicyDB(t)
 router:=&policyRevokingModelRoute{db:db}
 svc:=&Service{db:db,scheduler:router}
 _,_,_,err:=svc.reason(context.Background(),"tenant","project","world","operator",
  "perform Project work",json.RawMessage(`{"project":{"id":"project"}}`))
 if err==nil||router.calls!=1{
  t.Fatalf("live Workspace revocation did not block inference: err=%v routes=%d",err,router.calls)
 }
 // No inference or artifact implementation is present; reaching it would
 // cause a panic. A clean policy error proves the last local admission held.
}
