package agentworker

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func completionWithAccess(v any) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"onepane_routing": map[string]any{"workspace_access": v}})
	return b
}

func TestWorkspaceToolPolicyBrokeredCapabilities(t *testing.T) {
	raw := completionWithAccess(map[string]any{"mode": "brokered", "project_workspace_id": "pw", "internet": false, "lan": false, "browser": false, "computer": false, "filesystem": "workspace-only"})
	if err := workspaceToolAllowed(raw, "filesystem.read", authority.ActionRead, "file.read", "/workspace/main.go"); err != nil {
		t.Fatalf("workspace-scoped file read should remain lease-governed: %v", err)
	}
	if err := workspaceToolAllowed(raw, "browser.navigate", authority.ActionRead, "browser.open", "https://example.com"); err == nil {
		t.Fatal("expected browser denial")
	}
	if err := workspaceToolAllowed(raw, "network.send", authority.ActionExternalSend, "http.post", "https://example.com"); err == nil {
		t.Fatal("expected internet denial")
	}
}

func TestLegacyTaskWithoutWorkspacePolicyRemainsCompatible(t *testing.T) {
	if err := workspaceToolAllowed(json.RawMessage(`{"type":"operator_review"}`), "browser.navigate", authority.ActionRead, "browser.open", "https://example.com"); err != nil {
		t.Fatalf("legacy task unexpectedly denied: %v", err)
	}
}

func TestWorkspaceToolPolicyBlocksSecretsWhenDisabled(t *testing.T) {
	raw := completionWithAccess(map[string]any{
		"mode": "brokered", "project_workspace_id": "pws-1", "filesystem": "workspace-only",
		"internet": true, "lan": true, "browser": true, "computer": true, "secrets": "none",
	})
	if err := workspaceToolAllowed(raw, "vault.secret.read", authority.ActionRead, "vault.get", "credential://provider"); err == nil {
		t.Fatal("expected secret access to be denied")
	}
}

func TestDelegatedCompletionInheritsWorkspacePolicy(t *testing.T) {
	parent := json.RawMessage(`{"type":"operator_review","onepane_routing":{"enabled":false,"project_workspace_id":"pws-1","workspace_access":{"mode":"brokered","project_workspace_id":"pws-1","remote_models":false,"filesystem":"workspace-only","internet":false,"lan":false,"browser":false,"computer":false,"secrets":"none"}}}`)
	child := json.RawMessage(`{"type":"operator_review","onepane_routing":{"enabled":true,"workspace_access":{"mode":"direct","internet":true}}}`)
	got := inheritOnePaneRouting(parent, child)
	policy := routingPolicyFromCompletion(got)
	if policy.Enabled == nil || *policy.Enabled {
		t.Fatalf("expected inherited routing disabled, got %#v", policy.Enabled)
	}
	if policy.ProjectWorkspaceID != "pws-1" || policy.WorkspaceAccess.Mode != "brokered" || policy.WorkspaceAccess.Internet {
		t.Fatalf("delegated completion did not inherit parent workspace policy: %+v", policy)
	}
}

func TestExplicitWorkspaceLocalOnlyPolicyOverridesLegacyCloudDefaults(t *testing.T){
 local:=json.RawMessage(`{"type":"operator_review","onepane_routing":{
  "project_workspace_id":"world",
  "workspace_access":{"mode":"brokered","project_workspace_id":"world",
   "remote_models":false,"filesystem":"workspace-only","internet":false,
   "lan":false,"browser":false,"computer":false,"secrets":"none"}
 }}`)
 routing:=routingPolicyFromCompletion(local)
 if routing.WorkspaceAccess.RemoteModels==nil || *routing.WorkspaceAccess.RemoteModels {
  t.Fatal("explicit local-only Workspace request lost before scheduling")
 }
 inherited:=routingPolicyFromCompletion(inheritOnePaneRouting(local,json.RawMessage(`{"type":"operator_review"}`)))
 if inherited.WorkspaceAccess.RemoteModels==nil || *inherited.WorkspaceAccess.RemoteModels ||
  inherited.WorkspaceAccess.ProjectWorkspaceID!="world" {
  t.Fatal("delegation lost local-only Workspace and canonical identity")
 }
 cloudOptIn:=json.RawMessage(`{"onepane_routing":{"workspace_access":{"remote_models":true}}}`)
 configured:=routingPolicyFromCompletion(cloudOptIn)
 if configured.WorkspaceAccess.RemoteModels==nil || !*configured.WorkspaceAccess.RemoteModels{
  t.Fatal("explicit cloud opt-in did not remain visible to governed scheduler")
 }
}

func TestUnscopedHistoricWorkspaceTasksCannotImplicitlyUseCloud(t *testing.T) {
 projectID, workspaceID := "p","world"
 cases:=[]struct{name string;t task.Task;completion json.RawMessage;legacy bool;want bool}{
  {"new_named_no_envelope",task.Task{ProjectID:&projectID,ProjectWorkspaceID:&workspaceID},json.RawMessage(`{}`),true,false},
  {"named_explicit_false",task.Task{ProjectID:&projectID,ProjectWorkspaceID:&workspaceID},json.RawMessage(`{"onepane_routing":{"workspace_access":{"remote_models":false}}}`),true,false},
  {"named_explicit_true",task.Task{ProjectID:&projectID,ProjectWorkspaceID:&workspaceID},json.RawMessage(`{"onepane_routing":{"workspace_access":{"remote_models":true}}}`),true,true},
  {"legacy_project",task.Task{ProjectID:&projectID},json.RawMessage(`{}`),true,true},
  {"legacy_disabled",task.Task{ProjectID:&projectID},json.RawMessage(`{}`),false,false},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   v:=effectiveRemoteModelAllowance(tc.t,routingPolicyFromCompletion(tc.completion),tc.legacy)
   if v!=tc.want{t.Fatalf("unexpected cloud eligibility: got %v want %v",v,tc.want)}
  })
 }
}

func TestHistoricalNamedWorkspaceToolsStayBrokeredWithoutPolicyJSON(t *testing.T){
 pid,wid:="project","world"
 taskRow:=task.Task{
  ProjectID:&pid,ProjectWorkspaceID:&wid,
  Completion:json.RawMessage(`{"type":"operator_review"}`),
 }
 if err:=workspaceToolAllowedForTask(taskRow,"project.app.execute",authority.ActionExecuteSandboxed,
  "project.app.exec","project_runtime:runtime-world");err!=nil{
  t.Fatalf("existing local sandbox command should remain brokered and lease-governed: %v",err)
 }
 for _,tc:=range []struct{name,capability,tool,ref string;mode authority.ActionMode}{
  {"internet","network.external","web.fetch","https://example.com",authority.ActionRead},
  {"external_send","network.send","http.post","resource",authority.ActionExternalSend},
  {"browser","browser.navigate","browser.open","resource",authority.ActionRead},
  {"computer","computer.control","computer.click","resource",authority.ActionExecuteSandboxed},
  {"vault","vault.read","vault.get","resource",authority.ActionRead},
 }{
  t.Run(tc.name,func(t *testing.T){
   if err:=workspaceToolAllowedForTask(taskRow,tc.capability,tc.mode,tc.tool,tc.ref);err==nil{
    t.Fatal("unscoped historical Workspace Task was granted privileged tool")
   }
  })
 }
}

func TestNamedWorkspaceExecutionRejectsForgedPolicyAndOwnership(t *testing.T){
 pid,wid:="project","world"
 base:=task.Task{ProjectID:&pid,ProjectWorkspaceID:&wid}
 denied:=[]struct{name string;completion string}{
  {"foreign_top_identity",`{"onepane_routing":{"project_workspace_id":"story"}}`},
  {"foreign_access_identity",`{"onepane_routing":{"workspace_access":{"project_workspace_id":"story"}}}`},
  {"direct_access",`{"onepane_routing":{"workspace_access":{"mode":"direct"}}}`},
  {"host_filesystem",`{"onepane_routing":{"workspace_access":{"filesystem":"host"}}}`},
  {"network_access",`{"onepane_routing":{"workspace_access":{"internet":true}}}`},
  {"lan_access",`{"onepane_routing":{"workspace_access":{"lan":true}}}`},
  {"browser_access",`{"onepane_routing":{"workspace_access":{"browser":true}}}`},
  {"computer_access",`{"onepane_routing":{"workspace_access":{"computer":true}}}`},
  {"unrestricted_vault",`{"onepane_routing":{"workspace_access":{"secrets":"all"}}}`},
 }
 for _,tc:=range denied{
  t.Run(tc.name,func(t *testing.T){
   scoped:=base
   scoped.Completion=json.RawMessage(tc.completion)
   if err:=workspaceToolAllowedForTask(scoped,"project.app.execute",
    authority.ActionExecuteSandboxed,"project.app.exec","project_runtime:world");err==nil{
    t.Fatal("forged or elevated historical Task policy accepted")
   }
  })
 }
 missing:=task.Task{ProjectWorkspaceID:&wid}
 if err:=workspaceToolAllowedForTask(missing,"project.app.execute",
  authority.ActionExecuteSandboxed,"project.app.exec","project_runtime:world");err==nil{
  t.Fatal("Workspace Task with no Project was accepted")
 }
}

func TestHistoricalProjectOnlyToolCompatibilityIsPreserved(t *testing.T){
 legacy:=task.Task{Completion:json.RawMessage(`{"type":"operator_review"}`)}
 if err:=workspaceToolAllowedForTask(legacy,"browser.navigate",
  authority.ActionRead,"browser.open","https://example.com");err!=nil{
  t.Fatalf("legacy Project-only Task unexpectedly restricted: %v",err)
 }
}

func TestFilesystemNoneDeniesImplicitOCIWorkspaceMount(t *testing.T){
 noFilesystem:=completionWithAccess(map[string]any{
  "mode":"brokered","project_workspace_id":"world","filesystem":"none",
  "internet":false,"lan":false,"browser":false,"computer":false,"secrets":"none",
 })
 for _,tc:=range []struct{name,tool string;mode authority.ActionMode}{
  {"command","project.app.exec",authority.ActionExecuteSandboxed},
  {"git_read","project.app.git.inspect",authority.ActionExecuteSandboxed},
  {"start_app","project.app.ensure",authority.ActionMutate},
  {"create_runtime","project.runtime.ensure",authority.ActionMutate},
 }{
  t.Run(tc.name,func(t *testing.T){
   if err:=workspaceToolAllowed(noFilesystem,"project.app.execute",tc.mode,
    tc.tool,"project_runtime:world");err==nil{
    t.Fatal("filesystem:none allowed implicitly writable OCI Workspace mount")
   }
  })
 }
 if err:=workspaceToolAllowed(noFilesystem,"project.runtime.observe",
  authority.ActionObserve,"project.app.inspect","project_runtime:world");err!=nil{
  t.Fatalf("read-only OCI state observation incorrectly treated as file access: %v",err)
 }
 workspaceOnly:=completionWithAccess(map[string]any{
  "mode":"brokered","project_workspace_id":"world","filesystem":"workspace-only",
  "internet":false,"lan":false,"browser":false,"computer":false,"secrets":"none",
 })
 if err:=workspaceToolAllowed(workspaceOnly,"project.app.execute",authority.ActionExecuteSandboxed,
  "project.app.exec","project_runtime:world");err!=nil{
  t.Fatalf("legitimate Workspace-only sandbox command denied: %v",err)
 }
 pid,wid:="project","world"
 scoped:=task.Task{ProjectID:&pid,ProjectWorkspaceID:&wid,
  Completion:noFilesystem}
 if err:=workspaceToolAllowedForTask(scoped,"project.app.execute",
  authority.ActionExecuteSandboxed,"project.app.exec","project_runtime:world");err==nil{
  t.Fatal("persisted Task filesystem:none policy did not survive historical Task boundary")
 }
}
