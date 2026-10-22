package featurepolicy

import (
 "encoding/json"
 "testing"
)

func TestWorkspaceActionPolicyKeepsFileReadWriteIndependent(t *testing.T){
 base:=AssistedDefaults()
 if !base.ValidateExecution("files.read")||!base.ValidateExecution("files.write"){
  t.Fatal("legacy Workspace-local file permissions regressed")
 }
 for _,action:=range []string{"process.execute","network.connect","secrets.use"}{
  if base.ValidateExecution(action){t.Fatalf("unapproved elevated action %s became enabled",action)}
 }
 workspace:=Policy{FileRead:Disabled,FileWrite:Enabled}
 task:=Policy{FileRead:Enabled,FileWrite:Enabled}
 applied:=ResolvePolicyChain(base,workspace,task)
 if applied.ValidateExecution("files.read")||!applied.ValidateExecution("files.write"){
  t.Fatalf("read prohibition was bypassed or write was incorrectly coupled: %+v",applied)
 }
 project:=base;project.FileWrite=Disabled
 narrower:=ResolvePolicyChain(project,Policy{FileWrite:Enabled})
 if narrower.ValidateExecution("files.write")||!narrower.ValidateExecution("files.read"){
  t.Fatal("child restored denied parent write privilege")
 }
 master:=base;master.Files=Disabled
 if effective:=ResolvePolicyChain(master,Policy{FileRead:Enabled,FileWrite:Enabled});
  effective.ValidateExecution("files.read")||effective.ValidateExecution("files.write"){
  t.Fatal("new file toggles bypassed the legacy parent Files master switch")
 }
 legacy:=ResolvePolicyChain(Policy{Files:Enabled})
 if !legacy.ValidateExecution("files.read")||!legacy.ValidateExecution("files.write"){
  t.Fatal("older root policy unexpectedly lost file access")
 }
}

func TestWorkspaceExecuteNetworkSecretsRequireIndependentPositiveGrants(t *testing.T){
 project:=AssistedDefaults()
 project.ProcessExecute=Enabled
 project.NetworkAccess=Enabled
 project.SecretsAccess=Enabled
 ws:=Policy{NetworkAccess:Disabled}
 task:=Policy{NetworkAccess:Enabled,ProcessExecute:Enabled,SecretsAccess:Enabled}
 effective:=ResolvePolicyChain(project,ws,task)
 if !effective.ValidateExecution("process.execute")||!effective.ValidateExecution("secrets.use"){
  t.Fatal("approved independent Workspace actions denied")
 }
 if effective.ValidateExecution("network.connect"){
  t.Fatal("child Task restored revoked network privilege")
 }
 for _,action:=range []string{"shell.exec","network.open","secrets.export","files.publish",""}{
  if effective.ValidateExecution(action){t.Fatalf("unclassified action was granted: %s",action)}
 }
}

func TestStoredWorkspaceActionPolicyValuesValidatedAndVersionable(t *testing.T){
 base:=AssistedDefaults()
 raw:=[]byte(`{"feature_policy":{"FileRead":"enabled","FileWrite":"disabled","ProcessExecute":"enabled","NetworkAccess":"disabled","SecretsAccess":"disabled"}}`)
 policy,configured,err:=parseFeatureDocument(raw,base)
 if err!=nil||!configured{t.Fatalf("accepted typed policy rejected: %v",err)}
 scope:=ResolvePolicyChain(policy)
 if scope.ValidateExecution("files.write")||!scope.ValidateExecution("files.read")||
   !scope.ValidateExecution("process.execute")||scope.ValidateExecution("network.connect")||
   scope.ValidateExecution("secrets.use"){
  t.Fatalf("effective policy did not enforce independent actions: %+v",scope)
 }
 for _,field:=range []string{"FileRead","FileWrite","ProcessExecute","NetworkAccess","SecretsAccess"}{
  blob,_:=json.Marshal(map[string]any{"feature_policy":map[string]any{field:"unrestricted"}})
  if _,_,err:=parseFeatureDocument(blob,base);err==nil{
   t.Fatalf("unknown state for %s was accepted",field)
  }
 }
}
