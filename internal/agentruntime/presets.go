package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Preset struct {
	ID                 string        `json:"id"`
	DisplayName        string        `json:"display_name"`
	Category           string        `json:"category"`
	IntegrationMode    string        `json:"integration_mode"`
	DefaultEndpoint    string        `json:"default_endpoint,omitempty"`
	RecommendedMode    OperatingMode `json:"recommended_mode"`
	AutonomousEligible bool          `json:"autonomous_eligible"`
	RequiresBridge     bool          `json:"requires_bridge"`
	Description        string        `json:"description"`
	Notes              []string      `json:"notes,omitempty"`
}

func runtimePreset(id, name, category string, bridge bool) Preset {
	return Preset{
		ID: id, DisplayName: name, Category: category, IntegrationMode: "onepane_agent_protocol_v1",
		DefaultEndpoint: "http://127.0.0.1:9000", RecommendedMode: ProposalOnly,
		AutonomousEligible: true, RequiresBridge: bridge,
		Description: "Connect through OnePane Agent Protocol v1. The external harness returns proposals; OnePane retains authority, tools, budgets and verification.",
	}
}

func unmanagedPreset(id, name, category string) Preset {
	return Preset{
		ID: id, DisplayName: name, Category: category, IntegrationMode: "unmanaged_human_only",
		RecommendedMode: Unmanaged, AutonomousEligible: false, RequiresBridge: false,
		Description: "Register as an opaque external runtime for human use/visibility only. It is not eligible for autonomous OnePane scheduling until a proposal-only bridge is installed.",
	}
}

func BuiltinPresets() []Preset {
	out := []Preset{
		runtimePreset("hermes", "Hermes Agent", "agent_harness", false),
		runtimePreset("microsoft-agent-framework", "Microsoft Agent Framework", "agent_framework", true),
		runtimePreset("google-adk", "Google Agent Development Kit (ADK)", "agent_framework", true),
		runtimePreset("aws-strands", "AWS Strands Agents", "agent_framework", true),
		runtimePreset("openai-agents-sdk", "OpenAI Agents SDK", "agent_framework", true),
		runtimePreset("langgraph", "LangGraph", "agent_framework", true),
		runtimePreset("langchain-deep-agents", "LangChain Deep Agents", "agent_framework", true),
		runtimePreset("crewai", "CrewAI", "agent_framework", true),
		runtimePreset("pydantic-ai", "Pydantic AI", "agent_framework", true),
		runtimePreset("llamaindex-agents", "LlamaIndex Agents", "agent_framework", true),
		runtimePreset("autogen", "Microsoft AutoGen", "legacy_agent_framework", true),
		runtimePreset("semantic-kernel", "Microsoft Semantic Kernel", "legacy_agent_framework", true),
		runtimePreset("openhands", "OpenHands", "coding_agent", true),
		runtimePreset("goose", "goose", "coding_agent", true),
		runtimePreset("opencode", "OpenCode", "coding_agent", true),
		runtimePreset("openclaw", "OpenClaw", "coding_agent", true),
		runtimePreset("claude-code", "Claude Code", "coding_agent", true),
		runtimePreset("openai-codex", "OpenAI Codex CLI", "coding_agent", true),
		runtimePreset("gemini-cli", "Gemini CLI", "coding_agent", true),
		runtimePreset("aider", "Aider", "coding_agent", true),
		runtimePreset("qwen-code", "Qwen Code", "coding_agent", true),
		runtimePreset("kilo-code", "Kilo Code", "coding_agent", true),
		runtimePreset("pi", "Pi coding agent", "coding_agent", true),
		runtimePreset("dify", "Dify", "workflow_agent", true),
		runtimePreset("flowise", "Flowise", "workflow_agent", true),
		runtimePreset("langflow", "Langflow", "workflow_agent", true),
		runtimePreset("n8n-ai", "n8n AI / workflow agents", "workflow_agent", true),
		unmanagedPreset("cursor", "Cursor", "ide_agent"),
		unmanagedPreset("windsurf", "Windsurf", "ide_agent"),
		unmanagedPreset("cline", "Cline", "ide_agent"),
		unmanagedPreset("roo-code", "Roo Code", "ide_agent"),
		runtimePreset("custom-agent-protocol", "Custom OnePane Agent Protocol runtime", "custom", false),
		runtimePreset("custom-a2a-bridge", "Custom A2A runtime via OnePane bridge", "custom", true),
	}
	for i := range out {
		if out[i].ID == "autogen" || out[i].ID == "semantic-kernel" {
			out[i].Notes = []string{"Supported for existing deployments; Microsoft Agent Framework is the current successor path."}
		}
		if out[i].Category == "coding_agent" && out[i].RequiresBridge {
			out[i].Notes = append(out[i].Notes, "Native tool execution must be disabled or mediated by the bridge for autonomous scheduling.")
		}
	}
	return out
}

func PresetByID(id string) (Preset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range BuiltinPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

type RegisterPresetCommand struct {
	WorkspaceID      *string
	NodeID           *string
	PresetID         string
	DisplayName      string
	BaseURL          string
	SecretRef        *string
	AuthType         string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

func (s *Service) RegisterPreset(ctx context.Context, cmd RegisterPresetCommand) (Connection, error) {
	p, ok := PresetByID(cmd.PresetID)
	if !ok {
		return Connection{}, fmt.Errorf("%w: unknown runtime preset", ErrInvalidCommand)
	}
	name := strings.TrimSpace(cmd.DisplayName)
	if name == "" {
		name = p.DisplayName
	}
	if p.RecommendedMode == Unmanaged {
		return s.Register(ctx, RegisterCommand{WorkspaceID: cmd.WorkspaceID, NodeID: cmd.NodeID, RuntimeKind: p.ID, DisplayName: name, AdapterName: "builtin.unmanaged_external", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{}`), AuthType: "none", TrustState: TrustUserTrusted, OperatingMode: Unmanaged, ProtocolJSON: json.RawMessage(`{"agent_protocol":"none"}`), CapabilitiesJSON: json.RawMessage(`{"autonomous":false}`), ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	}
	base := strings.TrimSpace(cmd.BaseURL)
	if base == "" {
		base = p.DefaultEndpoint
	}
	if base == "" {
		return Connection{}, fmt.Errorf("%w: base_url is required", ErrInvalidCommand)
	}
	auth := strings.TrimSpace(cmd.AuthType)
	if auth == "" {
		if cmd.SecretRef == nil {
			auth = "none"
		} else {
			auth = "bearer"
		}
	}
	if cmd.SecretRef != nil {
		if s.credentialValidator == nil {
			return Connection{}, fmt.Errorf("%w: runtime credential validator unavailable", ErrInvalidCommand)
		}
		if err := s.credentialValidator.ValidateAgentRuntimeCredentialRef(ctx, *cmd.SecretRef, p.ID); err != nil {
			return Connection{}, fmt.Errorf("%w: runtime credential scope: %v", ErrInvalidCommand, err)
		}
	}
	endpoint, _ := json.Marshal(map[string]any{"base_url": base, "invoke_path": "/v1/agent/run"})
	caps, _ := json.Marshal(map[string]any{"autonomous": true, "proposal_only": true, "preset_id": p.ID, "bridge_required": p.RequiresBridge})
	return s.Register(ctx, RegisterCommand{WorkspaceID: cmd.WorkspaceID, NodeID: cmd.NodeID, RuntimeKind: p.ID, DisplayName: name, AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: endpoint, AuthType: auth, SecretRef: cmd.SecretRef, TrustState: TrustUserTrusted, OperatingMode: ProposalOnly, ProtocolJSON: json.RawMessage(`{"agent_protocol":"v1"}`), CapabilitiesJSON: caps, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
}
