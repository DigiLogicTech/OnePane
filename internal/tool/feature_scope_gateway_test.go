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
  // Mirror the real built-in Project files.inspect registration:
  // it executes a fixed command inside an approved OCI sandbox.
  Mode:authority.ActionExecuteSandboxed,AdapterID:adapter.id,AdapterVersion:adapter.version,
  Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter)
 if err!=nil{t.Fatal(err)}
 store:=&fakeStore{}
 evaluator:=&fakePolicy{decision:policy.Decision{Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone}}
 gateway:=newGateway(store,evaluator,a,reg,&fakeClock{now:1000})
 gateway.scopes=featurepolicy.StoredScopes{DB:db}
 gateway.EnableSandboxAdapter(adapter.id,adapter.version)
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

// Generic command execution is never authorized by the Files master
// switch alone: it requires positive Execute plus conservative file scope.
func TestConfiguredWorkspaceToolEffectsBlockSandboxAndNetworkBypass(t *testing.T){
 db:=featureScopeFixture(t)
 for _,q:=range []string{
  `UPDATE projects SET project_policy_json='{"feature_policy":{"Files":"enabled","ProcessExecute":"enabled","NetworkAccess":"enabled"}}' WHERE id='game'`,
  `UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"FileWrite":"disabled","ProcessExecute":"enabled","NetworkAccess":"disabled"}}' WHERE id='world'`,
 }{if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}}
 test:=func(toolID string,mode authority.ActionMode) (*Gateway,*countingAdapter,*fakeAuthority){
  t.Helper()
  ad:=&countingAdapter{id:"sandbox",version:"1",result:AdapterResult{Summary:"ok",Result:json.RawMessage(`{}`)}}
  reg:=NewRegistry()
  if err:=reg.Register(Definition{ID:toolID,Version:"1",CapabilityID:"project.app.execute",Mode:mode,
   AdapterID:ad.id,AdapterVersion:ad.version,Risk:policy.RiskLow,
   MinimumVerification:policy.VerificationV1,MinimumApproval:policy.ApprovalNone},ad);err!=nil{t.Fatal(err)}
  auth:=&fakeAuthority{}
  gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
   Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone}},auth,reg,&fakeClock{now:1000})
  gateway.scopes=featurepolicy.StoredScopes{DB:db}
  gateway.EnableSandboxAdapter(ad.id,ad.version)
  return gateway,ad,auth
 }
 for _,tc:=range []struct{tool string;mode authority.ActionMode}{
  {"project.app.exec",authority.ActionExecuteSandboxed},
  {"project.app.git.mutate",authority.ActionExecuteSandboxed},
  {"project.app.files.edit",authority.ActionExecuteSandboxed},
  {"project.app.pull",authority.ActionMutate},
  {"unknown.mutate",authority.ActionMutate},
 }{
  g,ad,auth:=test(tc.tool,tc.mode)
  cmd:=scopedReadCommand();cmd.ToolID=tc.tool
  got,err:=g.Invoke(context.Background(),cmd)
  if !errors.Is(err,ErrPolicyDenied)||got.Status!=StatusFailed||
   auth.calls!=0||ad.calls!=0{
   t.Fatalf("%s bypassed Workspace policy: %+v err=%v auth=%d adapter=%d",
    tc.tool,got,err,auth.calls,ad.calls)
  }
 }
}

type revokeExecuteAuthority struct{db *sql.DB}
func(a *revokeExecuteAuthority)Consume(ctx context.Context,_ authority.ConsumeCommand)(authority.Lease,error){
 _,err:=a.db.ExecContext(ctx,
  `UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"ProcessExecute":"disabled"}}' WHERE id='world'`)
 return authority.Lease{},err
}
// An adapter may claim "filesystem.write" or "vault.read" while its
// actual mutation can do anything. Configured Workspaces require an exact,
// reviewed tool effect mapping instead of trusting its capability label.
func TestUnknownMutatingToolCannotBorrowNarrowCapability(t *testing.T){
 db:=featureScopeFixture(t)
 for _,q:=range []string{
  `UPDATE projects SET project_policy_json='{"feature_policy":{"Files":"enabled","FileWrite":"enabled","ProcessExecute":"enabled","NetworkAccess":"enabled","SecretsAccess":"enabled"}}' WHERE id='game'`,
  `UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"Files":"enabled","FileWrite":"enabled","ProcessExecute":"enabled","NetworkAccess":"enabled","SecretsAccess":"enabled"}}' WHERE id='world'`,
 }{
  if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 for _,tc:=range []struct{name,cap,id string;mode authority.ActionMode}{
  {"file-write-labelled mutation","filesystem.write","customer.generic.mutate",authority.ActionMutate},
  {"vault-labelled mutation","vault.read","customer.generic.mutate",authority.ActionMutate},
  {"network-labelled mutation","network.send","customer.generic.mutate",authority.ActionMutate},
  {"reserved-goal-read impersonation","filesystem.read","project.goal.unreviewed",authority.ActionRead},
  {"reserved-goal-send impersonation","network.send","project.goal.unreviewed",authority.ActionExternalSend},
 }{
  t.Run(tc.name,func(t *testing.T){
   adapter:=&countingAdapter{id:"unreviewed-adapter",version:"1",
    result:AdapterResult{Summary:"must not run",Result:json.RawMessage(`{}`)}}
   reg:=NewRegistry()
   if err:=reg.Register(Definition{
    ID:tc.id,Version:"1",CapabilityID:tc.cap,Mode:tc.mode,
    AdapterID:adapter.id,AdapterVersion:adapter.version,
    Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
    MinimumApproval:policy.ApprovalNone,
   },adapter);err!=nil{t.Fatal(err)}
   auth:=&fakeAuthority{}
   g:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
    Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone}},
    auth,reg,&fakeClock{now:1000})
   g.scopes=featurepolicy.StoredScopes{DB:db}
   cmd:=scopedReadCommand()
   cmd.ToolID=tc.id
   got,err:=g.Invoke(context.Background(),cmd)
   if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
    *got.ErrorCode!="feature_policy_denied"||
    auth.calls!=0||adapter.calls!=0{
    t.Fatalf("unknown mutation/Goal adapter got rights from forged capability label: %+v err=%v authority=%d adapter=%d",
     got,err,auth.calls,adapter.calls)
   }
  })
 }
}

func TestToolGatewayRevokedSandboxExecuteNeverCallsAdapter(t *testing.T){
 db:=featureScopeFixture(t)
 if _,err:=db.Exec(`UPDATE projects SET project_policy_json='{"feature_policy":{"ProcessExecute":"enabled"}}' WHERE id='game'`);err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"ProcessExecute":"enabled"}}' WHERE id='world'`);err!=nil{t.Fatal(err)}
 ad:=&countingAdapter{id:"sandbox",version:"1",result:AdapterResult{Summary:"ok",Result:json.RawMessage(`{}`)}}
 reg:=NewRegistry()
 if err:=reg.Register(Definition{
  ID:"project.app.exec",Version:"1",CapabilityID:"project.app.execute",
  Mode:authority.ActionExecuteSandboxed,AdapterID:ad.id,AdapterVersion:ad.version,
  Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone},ad);err!=nil{t.Fatal(err)}
 gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
  Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone}},
  &revokeExecuteAuthority{db:db},reg,&fakeClock{now:1000})
 gateway.scopes=featurepolicy.StoredScopes{DB:db}
 gateway.EnableSandboxAdapter(ad.id,ad.version)
 cmd:=scopedReadCommand();cmd.ToolID="project.app.exec"
 got,err:=gateway.Invoke(context.Background(),cmd)
 if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
  *got.ErrorCode!="feature_policy_revoked"||ad.calls!=0{
  t.Fatalf("revoked Execute permission reached sandbox: %+v err=%v adapter=%d",
   got,err,ad.calls)
 }
}
