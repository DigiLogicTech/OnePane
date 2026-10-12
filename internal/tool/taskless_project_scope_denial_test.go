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
