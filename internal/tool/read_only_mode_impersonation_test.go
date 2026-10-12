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

func TestReadOnlyToolIdentitiesCannotMasqueradeAsMutations(t *testing.T){
 bad:=[]struct{id,cap string;mode authority.ActionMode}{
  {"project.app.files.inspect","filesystem.read",authority.ActionMutate},
  {"project.app.git.inspect","project.app.execute",authority.ActionExternalSend},
  {"project.runtime.inspect","project.runtime.observe",authority.ActionMutate},
  {"project.app.inspect","project.runtime.observe",authority.ActionMutate},
  {"project.image.inspect","project.runtime.observe",authority.ActionExternalSend},
  {"another.read.tool","filesystem.read",authority.ActionMutate},
  {"another.read.tool","file.read",authority.ActionExternalSend},
 }
 for _,tc:=range bad{
  if effects,ok:=scopedToolActions(tc.cap,tc.id,tc.mode);ok||len(effects)>0{
   t.Errorf("read-only capability %s impersonated a mutating adapter (%s): %v",
    tc.id,tc.mode,effects)
  }
 }
 // Actual sandbox file inspections still work with their explicitly
 // classified read-only effects despite being sandbox-executed.
 effects,ok:=scopedToolActions("filesystem.read","project.app.files.inspect",authority.ActionExecuteSandboxed)
 if !ok||len(effects)!=1||effects[0]!="files.read"{
  t.Fatalf("legitimate sandbox file inspection broke: %v %v",effects,ok)
 }
}

func TestMutatingReadOnlyImpersonationDeniedBeforeOperationPermit(t *testing.T){
 db:=featureScopeFixture(t)
 for _,sqlText:=range []string{
  `UPDATE projects SET project_policy_json=
   '{"feature_policy":{"Files":"enabled","FileRead":"enabled","FileWrite":"enabled",
    "ProcessExecute":"enabled","NetworkAccess":"enabled"}}'
   WHERE id='game'`,
  `UPDATE project_workspaces SET ai_settings_json=
   '{"feature_policy":{"Files":"enabled","FileRead":"enabled","FileWrite":"enabled",
    "ProcessExecute":"enabled","NetworkAccess":"enabled"}}'
   WHERE id='world'`,
 }{
  if _,err:=db.Exec(sqlText);err!=nil{t.Fatal(err)}
 }
 adapter:=&countingAdapter{
  id:"impersonator",version:"1",
  result:AdapterResult{Summary:"should never run",Result:json.RawMessage(`{}`)},
 }
 registry:=NewRegistry()
 if err:=registry.Register(Definition{
  ID:"project.app.files.inspect",Version:"1",CapabilityID:"filesystem.read",
  Mode:authority.ActionMutate,AdapterID:adapter.id,AdapterVersion:adapter.version,
  Risk:policy.RiskLow,MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter);err!=nil{t.Fatal(err)}
 auth:=&fakeAuthority{}
 g:=newGateway(&fakeStore{},&fakePolicy{decision:policy.Decision{
  Allowed:true,LeaseRevision:1,RequiredApproval:policy.ApprovalNone,
 }},auth,registry,&fakeClock{now:1000})
 g.scopes=featurepolicy.StoredScopes{DB:db}
 cmd:=scopedReadCommand()
 got,err:=g.Invoke(context.Background(),cmd)
 if !errors.Is(err,ErrPolicyDenied)||got.ErrorCode==nil||
  *got.ErrorCode!="feature_policy_denied"{
  t.Fatalf("mutating adapter disguised as reader passed classification: %+v %v",got,err)
 }
 if auth.calls!=0||adapter.calls!=0{
  t.Fatalf("denied impostor reached authority or adapter: %d/%d",auth.calls,adapter.calls)
 }
}
