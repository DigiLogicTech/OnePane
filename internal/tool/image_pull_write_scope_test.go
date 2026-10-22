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

// OCI image pulls write a host/container image store despite being named
// "pull". Independent FileWrite=false cannot be bypassed by Network=true
// and ProcessExecute=true, or by an otherwise valid authority lease.
func TestOCIImagePullNeedsWriteGrantEvenWhenNetworkAndExecuteAllowed(t *testing.T){
 db:=featureScopeFixture(t)
 for _,query:=range []string{
  `UPDATE projects SET project_policy_json=
   '{"feature_policy":{"Files":"enabled","FileRead":"enabled",
    "FileWrite":"enabled","ProcessExecute":"enabled","NetworkAccess":"enabled"}}'
   WHERE id='game'`,
  `UPDATE project_workspaces SET ai_settings_json=
   '{"feature_policy":{"Files":"enabled","FileRead":"enabled",
    "FileWrite":"disabled","ProcessExecute":"enabled","NetworkAccess":"enabled"}}'
   WHERE id='world'`,
 }{
  if _,err:=db.Exec(query);err!=nil{t.Fatal(err)}
 }
 effects,known:=scopedToolActions("project.runtime.manage","project.app.pull",authority.ActionMutate)
 if !known||len(effects)!=3||effects[0]!="files.write"{
  t.Fatalf("image-store mutation not classified as Write: %v known=%t",effects,known)
 }
 adapter:=&countingAdapter{
  id:"oci",version:"1",
  result:AdapterResult{Summary:"unexpected execution",Result:json.RawMessage(`{}`)},
 }
 registry:=NewRegistry()
 if err:=registry.Register(Definition{
  ID:"project.app.pull",Version:"1",CapabilityID:"project.runtime.manage",
  Mode:authority.ActionMutate,AdapterID:adapter.id,
  AdapterVersion:adapter.version,Risk:policy.RiskMedium,
  MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter);err!=nil{t.Fatal(err)}
 auth:=&fakeAuthority{}
 gateway:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
  Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
 }},auth,registry,&fakeClock{now:1000})
 gateway.scopes=featurepolicy.StoredScopes{DB:db}
 command:=scopedReadCommand()
 command.ToolID="project.app.pull"
 command.ResourceRef="oci://example.com/base:latest"
 command.Input=json.RawMessage(`{"image":"example.com/base:latest"}`)
 got,err:=gateway.Invoke(context.Background(),command)
 if !errors.Is(err,ErrPolicyDenied)||got.Status!=StatusFailed||
  got.ErrorCode==nil||*got.ErrorCode!="feature_policy_denied"{
  t.Fatalf("read-only Workspace OCI pull did not fail closed: %+v err=%v",got,err)
 }
 if auth.calls!=0||adapter.calls!=0{
  t.Fatalf("denied image pull reached authority or container engine: auth=%d adapter=%d",
   auth.calls,adapter.calls)
 }
}
