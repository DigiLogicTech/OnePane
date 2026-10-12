package tool

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/policy"
)

// A tenant lease without a Task is not a Project/Workspace feature grant.
// This regression uses a non-nil production-style policy store; no SQL
// statements are needed because rejection must precede adapter dispatch.
func TestTasklessGenericSideEffectsDeniedBeforeLeaseOrAdapter(t *testing.T) {
 for _,mode:=range []authority.ActionMode{
  authority.ActionExecuteSandboxed,
  authority.ActionMutate,
  authority.ActionExternalSend,
 }{
  t.Run(string(mode),func(t *testing.T){
   adapter:=&countingAdapter{
    id:"generic-adapter",version:"1",
    result:AdapterResult{Summary:"must not run",Result:json.RawMessage(`{"ok":true}`)},
   }
   g,_,_,lease:=testGateway(t,mode,policy.Decision{
    Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
   },adapter)
   g.scopes.DB=&sql.DB{}
   inv,err:=g.Invoke(context.Background(),command())
   if !errors.Is(err,ErrPolicyDenied){
    t.Fatalf("Taskless %s effect not denied: %v",mode,err)
   }
   if inv.ErrorCode==nil||*inv.ErrorCode!="feature_policy_denied"||
    adapter.calls!=0||lease.calls!=0{
    t.Fatalf("effect escaped before Project scope admission: %+v calls=%d lease=%d",
     inv,adapter.calls,lease.calls)
   }
  })
 }
}

func TestTasklessOrdinaryReadStillRespectsExistingLease(t *testing.T){
 adapter:=&countingAdapter{id:"read-adapter",version:"1",
  result:AdapterResult{Summary:"read",Result:json.RawMessage(`{"ok":true}`)}}
 g,_,_,lease:=testGateway(t,authority.ActionRead,policy.Decision{
  Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
 },adapter)
 g.scopes.DB=&sql.DB{}
 inv,err:=g.Invoke(context.Background(),command())
 if err!=nil||inv.Status!=StatusSucceeded||adapter.calls!=1||lease.calls!=1{
  t.Fatalf("legitimate legacy read was blocked: %+v %v adapter=%d lease=%d",
   inv,err,adapter.calls,lease.calls)
 }
}
