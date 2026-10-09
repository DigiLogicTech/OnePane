package agentworker

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
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
