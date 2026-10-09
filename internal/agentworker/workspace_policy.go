package agentworker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type workspaceAccessPolicy struct {
	Mode               string `json:"mode"`
	ProjectWorkspaceID string `json:"project_workspace_id"`
	RemoteModels       *bool  `json:"remote_models,omitempty"`
	Filesystem         string `json:"filesystem,omitempty"`
	Internet           bool   `json:"internet"`
	LAN                bool   `json:"lan"`
	Browser            bool   `json:"browser"`
	Computer           bool   `json:"computer"`
	Secrets            string `json:"secrets,omitempty"`
}

type onePaneRoutingPolicy struct {
	Enabled               *bool                 `json:"enabled,omitempty"`
	CandidateID           string                `json:"candidate_id,omitempty"`
	FallbackCandidateIDs  []string              `json:"fallback_candidate_ids,omitempty"`
	AgentProfile          string                `json:"agent_profile,omitempty"`
	FallbackAgentProfiles []string              `json:"fallback_agent_profiles,omitempty"`
	ProjectWorkspaceID    string                `json:"project_workspace_id,omitempty"`
	WorkspaceAccess       workspaceAccessPolicy `json:"workspace_access,omitempty"`
}

func routingPolicyFromCompletion(raw json.RawMessage) onePaneRoutingPolicy {
	var envelope struct {
		OnePaneRouting onePaneRoutingPolicy `json:"onepane_routing"`
	}
	_ = json.Unmarshal(raw, &envelope)
	return envelope.OnePaneRouting
}

// effectiveRemoteModelAllowance preserves legacy Project-only routing while
// preventing pre-policy named Workspace Tasks from silently selecting cloud.
// A deliberate Workspace remote_models=true remains governed by scheduler,
// provider approval and budget checks.
func effectiveRemoteModelAllowance(t task.Task, p onePaneRoutingPolicy, legacyDefault bool) bool {
	if p.WorkspaceAccess.RemoteModels != nil {
		return *p.WorkspaceAccess.RemoteModels
	}
	if t.ProjectWorkspaceID != nil {
		return false
	}
	return legacyDefault
}

// inheritOnePaneRouting preserves the parent workspace routing/access envelope for
// delegated child tasks. A delegated model may narrow its own completion contract,
// but it cannot drop or widen the workspace security/routing policy.
func inheritOnePaneRouting(parent, child json.RawMessage) json.RawMessage {
	var parentEnvelope map[string]json.RawMessage
	if json.Unmarshal(parent, &parentEnvelope) != nil {
		return child
	}
	routing, ok := parentEnvelope["onepane_routing"]
	if !ok || len(routing) == 0 {
		return child
	}
	childEnvelope := map[string]json.RawMessage{}
	if len(child) > 0 {
		_ = json.Unmarshal(child, &childEnvelope)
	}
	childEnvelope["onepane_routing"] = routing
	b, err := json.Marshal(childEnvelope)
	if err != nil {
		return child
	}
	return b
}

// workspaceToolAllowedForTask enforces the *persisted* Project Workspace
// identity even for Tasks created before the new Task creation invariant.
// Model-generated completion or missing legacy scope cannot grant extra tools.
func workspaceToolAllowedForTask(t task.Task, capabilityID string, mode authority.ActionMode, toolID, resourceRef string) error {
 if t.ProjectWorkspaceID==nil{
  return workspaceToolAllowed(t.Completion,capabilityID,mode,toolID,resourceRef)
 }
 if t.ProjectID==nil || *t.ProjectID=="" || *t.ProjectWorkspaceID=="" {
  return fmt.Errorf("named Workspace Task requires persisted Project identity")
 }
 routing:=routingPolicyFromCompletion(t.Completion)
 if routing.ProjectWorkspaceID!=""&&routing.ProjectWorkspaceID!=*t.ProjectWorkspaceID{
  return fmt.Errorf("Workspace routing identity does not match persisted Task ownership")
 }
 p:=routing.WorkspaceAccess
 if p.ProjectWorkspaceID!=""&&p.ProjectWorkspaceID!=*t.ProjectWorkspaceID{
  return fmt.Errorf("Workspace access identity does not match persisted Task ownership")
 }
 if p.Mode!=""&&p.Mode!="brokered"{
  return fmt.Errorf("named Workspace Task requires brokered access")
 }
 if p.Filesystem!=""&&p.Filesystem!="none"&&p.Filesystem!="workspace-only"{
  return fmt.Errorf("named Workspace Task cannot access host filesystems")
 }
 if p.Internet||p.LAN||p.Browser||p.Computer||(p.Secrets!=""&&p.Secrets!="none"){
  return fmt.Errorf("Workspace Task tool privileges require independently authorised grants")
 }
 p.Mode="brokered"
 p.ProjectWorkspaceID=*t.ProjectWorkspaceID
 if p.Filesystem==""{p.Filesystem="workspace-only"}
 p.Secrets="none"
 return workspaceToolAllowedWithPolicy(p,capabilityID,mode,toolID,resourceRef)
}

func workspaceToolAllowed(raw json.RawMessage, capabilityID string, mode authority.ActionMode, toolID, resourceRef string) error {
 p:=routingPolicyFromCompletion(raw).WorkspaceAccess
 if strings.TrimSpace(p.Mode)==""&&strings.TrimSpace(p.ProjectWorkspaceID)==""{
  return nil // Historical Project-only Tasks retain their existing semantics.
 }
 return workspaceToolAllowedWithPolicy(p,capabilityID,mode,toolID,resourceRef)
}

func workspaceToolAllowedWithPolicy(p workspaceAccessPolicy, capabilityID string, mode authority.ActionMode, toolID, resourceRef string) error {
	if p.Mode != "brokered" {
		return fmt.Errorf("workspace policy requires brokered access")
	}
	hay := strings.ToLower(strings.Join([]string{capabilityID, toolID, resourceRef}, " "))
	if !p.Browser && strings.Contains(hay, "browser") {
		return fmt.Errorf("browser capability is disabled for this workspace")
	}
	if !p.Computer && (strings.Contains(hay, "computer") || strings.Contains(hay, "desktop")) {
		return fmt.Errorf("computer capability is disabled for this workspace")
	}
	if !p.Internet && mode == authority.ActionExternalSend {
		return fmt.Errorf("external network sends are disabled for this workspace")
	}
	if !p.Internet && (strings.Contains(hay, "http://") || strings.Contains(hay, "https://") || strings.Contains(hay, "web.fetch") || strings.Contains(hay, "network.external")) {
		return fmt.Errorf("internet access is disabled for this workspace")
	}
	if !p.LAN && (strings.Contains(hay, "network.lan") || strings.Contains(hay, "lan://")) {
		return fmt.Errorf("LAN access is disabled for this workspace")
	}
	if p.Filesystem == "none" && (strings.Contains(hay, "file") || strings.Contains(hay, "filesystem") || strings.Contains(hay, "/workspace")) {
		return fmt.Errorf("filesystem access is disabled for this workspace")
	}
	if p.Secrets == "none" && (strings.Contains(hay, "secret") || strings.Contains(hay, "vault") || strings.Contains(hay, "credential")) {
		return fmt.Errorf("secret access is disabled for this workspace")
	}
	// workspace-only filesystem boundaries are additionally enforced by the
	// CapabilityLease/resource scope. This policy can narrow access, never widen it.
	return nil
}
