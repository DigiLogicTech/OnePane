package tool

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 _ "modernc.org/sqlite"
)

// Only minimal SQL schema is needed because the Tool Gateway dependencies
// are mocked. These checks are against persisted, not supplied, policy scopes.
func featureScopeFixture(t *testing.T) *sql.DB {
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"tool-policy.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){db.Close()})
 for _,q:=range []string{
  `CREATE TABLE workspaces(id TEXT PRIMARY KEY,status TEXT NOT NULL)`,
  `CREATE TABLE projects(id TEXT PRIMARY KEY,workspace_id TEXT,status TEXT,project_policy_json TEXT)`,
  `CREATE TABLE project_workspaces(id TEXT PRIMARY KEY,project_id TEXT,status TEXT,ai_settings_json TEXT)`,
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT,completion_json TEXT)`,
  `INSERT INTO workspaces VALUES('ws','active')`,
  `INSERT INTO projects VALUES('game','ws','active','{"feature_policy":{"Files":"enabled"}}')`,
  `INSERT INTO project_workspaces VALUES('world','game','active','{"feature_policy":{"Files":"disabled"}}')`,
  `INSERT INTO tasks VALUES('task','ws','game','world','{}')`,
 }{
  if _,err:=db.Exec(q);err!=nil{t.Fatalf("fixture %s: %v",q,err)}
 }
 return db
}

func scopedReadGateway(t *testing.T,db *sql.DB, a authorityConsumer)(*Gateway,*countingAdapter,*fakeStore){
 t.Helper()
 adapter:=&countingAdapter{id:"fileadapter",version:"1",result:AdapterResult{Summary:"ok",Result:json.RawMessage(`{}`)}}
 reg:=NewRegistry()
 err:=reg.Register(Definition{
  ID:"project.app.files.inspect",Version:"1",CapabilityID:"filesystem.read",
  Mode:authority.ActionRead,AdapterID:adapter.id,AdapterVersion:adapter.version,
  Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter)
 if err!=nil{t.Fatal(err)}
 store:=&fakeStore{}
 evaluator:=&fakePolicy{decision:policy.Decision{Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone}}
 gateway:=newGateway(store,evaluator,a,reg,&fakeClock{now:1000})
 gateway.scopes=featurepolicy.StoredScopes{DB:db}
 return gateway,adapter,store
}

func scopedReadCommand() InvokeCommand{
 task:="task"
 return InvokeCommand{WorkspaceID:"ws",TaskID:&task,PrincipalID:"agent",LeaseID:"lease",
  ToolID:"project.app.files.inspect",ToolVersion:"1",ResourceRef:"/workspace/game.txt",
  Input:json.RawMessage(`{"path":"game.txt"}`)}
}

func TestToolGatewayDeniesStoredWorkspacePolicyBeforeAuthorityConsumption(t *testing.T){
 db:=featureScopeFixture(t)
 auth:=&fakeAuthority{}
 gateway,adapter,_:=scopedReadGateway(t,db,auth)
 denied,err:=gateway.Invoke(context.Background(),scopedReadCommand())
 if !errors.Is(err,ErrPolicyDenied)||denied.Status!=StatusFailed{
  t.Fatalf("scope denial missing: %+v, %v",denied,err)
 }
 if auth.calls!=0||adapter.calls!=0{t.Fatalf("denied file reached authority/adapter: %d/%d",auth.calls,adapter.calls)}
 if _,err:=db.Exec(`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"Files":"enabled"}}' WHERE id='world'`);err!=nil{t.Fatal(err)}
 good,err:=gateway.Invoke(context.Background(),scopedReadCommand())
 if err!=nil||good.Status!=StatusSucceeded||adapter.calls!=1||auth.calls!=1{
  t.Fatalf("explicitly granted file read denied: %+v %v calls=%d/%d",good,err,auth.calls,adapter.calls)
 }
}

// Authority service may consume a lease while the operator changes Workspace
// policy. The last boundary must not dispatch the adapter after revocation.
type policyRevokingAuthority struct{db *sql.DB; calls int}
func (a *policyRevokingAuthority)Consume(ctx context.Context,_ authority.ConsumeCommand)(authority.Lease,error){
 a.calls++
 _,err:=a.db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"Files":"disabled"}}' WHERE id='world'`)
 return authority.Lease{},err
}
func TestToolGatewayRechecksPolicyImmediatelyBeforeAdapter(t *testing.T){
 db:=featureScopeFixture(t)
 if _,err:=db.Exec(`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"Files":"enabled"}}' WHERE id='world'`);err!=nil{t.Fatal(err)}
 auth:=&policyRevokingAuthority{db:db}
 gateway,adapter,_:=scopedReadGateway(t,db,auth)
 got,err:=gateway.Invoke(context.Background(),scopedReadCommand())
 if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||*got.ErrorCode!="feature_policy_revoked"{
  t.Fatalf("last-moment policy revoke missed: %+v err=%v",got,err)
 }
 if auth.calls!=1||adapter.calls!=0{t.Fatalf("revoked file reached adapter: auth=%d adapter=%d",auth.calls,adapter.calls)}
}
