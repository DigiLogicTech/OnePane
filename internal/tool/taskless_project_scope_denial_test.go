package tool

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestTasklessProjectAdapterCannotSkipCanonicalProjectWorkspaceScope(t *testing.T){
 db:=featureScopeFixture(t)
 tests:=[]struct{id,cap string;mode authority.ActionMode}{
  {"project.app.files.inspect","filesystem.read",authority.ActionExecuteSandboxed},
  {"project.app.exec","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.pull","project.runtime.manage",authority.ActionMutate},
  {"project.runtime.inspect","project.runtime.observe",authority.ActionObserve},
  {"project.image.inspect","project.runtime.observe",authority.ActionObserve},
 }
 for _,tc:=range tests{
  t.Run(tc.id,func(t *testing.T){
   adapter:=&countingAdapter{id:"sandbox",version:"1",result:AdapterResult{
    Summary:"should not be dispatched",Result:json.RawMessage(`{}`)}}
   reg:=NewRegistry()
   if err:=reg.Register(Definition{
    ID:tc.id,Version:"1",CapabilityID:tc.cap,Mode:tc.mode,
    AdapterID:adapter.id,AdapterVersion:adapter.version,
    Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
    MinimumApproval:policy.ApprovalNone,
   },adapter);err!=nil{t.Fatal(err)}
   auth:=&fakeAuthority{}
   gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
    Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
   }},auth,reg,&fakeClock{now:1000})
   gateway.scopes=featurepolicy.StoredScopes{DB:db}
   gateway.EnableSandboxAdapter(adapter.id,adapter.version)
   cmd:=scopedReadCommand()
   cmd.TaskID=nil // Project affiliation is deliberately absent.
   cmd.ToolID=tc.id
   got,err:=gateway.Invoke(context.Background(),cmd)
   if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
    *got.ErrorCode!="feature_policy_denied"{
    t.Fatalf("taskless Project effect bypassed canonical scope: %+v err=%v",got,err)
   }
   if auth.calls!=0||adapter.calls!=0{
    t.Fatalf("unscoped Project adapter reached authority or execution: %d/%d",auth.calls,adapter.calls)
   }
  })
 }
}

func TestTaskPolicyStoreMissingFailsClosed(t *testing.T){
 // This also protects an incorrectly initialised/injected Tool Gateway:
 // if canonical Task scope lookups are unavailable, it must not dispatch.
 adapter:=&countingAdapter{id:"sandbox",version:"1",result:AdapterResult{
  Summary:"bad",Result:json.RawMessage(`{}`)}}
 reg:=NewRegistry()
 if err:=reg.Register(Definition{
  ID:"project.app.files.inspect",Version:"1",CapabilityID:"filesystem.read",
  Mode:authority.ActionExecuteSandboxed,
  AdapterID:adapter.id,AdapterVersion:adapter.version,
  Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter);err!=nil{t.Fatal(err)}
 auth:=&fakeAuthority{}
 gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
  Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
 }},auth,reg,&fakeClock{now:1000})
 gateway.EnableSandboxAdapter(adapter.id,adapter.version)
 cmd:=scopedReadCommand() // Task supplied, but gateway scope DB absent.
 got,err:=gateway.Invoke(context.Background(),cmd)
 if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
  *got.ErrorCode!="feature_policy_denied"{
  t.Fatalf("unavailable policy store fail-open: %+v err=%v",got,err)
 }
 if auth.calls!=0||adapter.calls!=0{
  t.Fatalf("unscoped adapter reached authority or execution: %d/%d",auth.calls,adapter.calls)
 }
}


func TestLegacyProjectToolNamesCannotSpoofModeOrBorrowGenericCapabilities(t *testing.T){
 db:=featureScopeFixture(t)
 // Preserve a historical Task with no explicit migrated feature policy.
 // Its existing lease still applies, but a new adapter must not impersonate
 // a built-in Project Tool ID or hide behind generic filesystem.read.
 for _,q:=range []string{
  `UPDATE projects SET project_policy_json='{}' WHERE id='game'`,
  `UPDATE project_workspaces SET ai_settings_json='{}' WHERE id='world'`,
 }{
  if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 candidates:=[]struct{id,cap string;mode authority.ActionMode}{
  {"project.app.files.inspect","filesystem.read",authority.ActionMutate},
  {"project.app.unreviewed","filesystem.read",authority.ActionRead},
  {"project.runtime.fake","project.runtime.observe",authority.ActionObserve},
 }
 for _,tc:=range candidates{
  t.Run(tc.id,func(t *testing.T){
   adapter:=&countingAdapter{id:"fake",version:"1",result:AdapterResult{
    Summary:"not called",Result:json.RawMessage(`{}`)}}
   registry:=NewRegistry()
   if err:=registry.Register(Definition{
    ID:tc.id,Version:"1",CapabilityID:tc.cap,Mode:tc.mode,
    AdapterID:adapter.id,AdapterVersion:adapter.version,
    Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
    MinimumApproval:policy.ApprovalNone,
   },adapter);err!=nil{t.Fatal(err)}
   auth:=&fakeAuthority{}
   gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
    Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
   }},auth,registry,&fakeClock{now:1000})
   gateway.scopes=featurepolicy.StoredScopes{DB:db}
   gateway.EnableSandboxAdapter(adapter.id,adapter.version)
   cmd:=scopedReadCommand();cmd.ToolID=tc.id
   got,err:=gateway.Invoke(context.Background(),cmd)
   if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
    *got.ErrorCode!="feature_policy_denied"{
    t.Fatalf("unmigrated policy accepted an unreviewed Project tool: %+v err=%v",got,err)
   }
   if auth.calls!=0||adapter.calls!=0{
    t.Fatalf("Project spoofing reached authority/adapter: %d/%d",auth.calls,adapter.calls)
   }
  })
 }
}
