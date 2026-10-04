package botruntime

import "strings"

type Preset struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"display_name"`
	Vendor               string   `json:"vendor"`
	SourceKind           string   `json:"source_kind"`
	AccessMode           string   `json:"access_mode"`
	InAppChat            bool     `json:"in_app_chat"`
	PersistentSessions   bool     `json:"persistent_sessions"`
	NativeTools          bool     `json:"native_tools"`
	NativeMemory         bool     `json:"native_memory"`
	GroupChat            bool     `json:"group_chat"`
	Routines             bool     `json:"routines"`
	RequiresAgentRuntime bool     `json:"requires_agent_runtime"`
	RequiresBridge       bool     `json:"requires_bridge"`
	AllowedLaunchHosts   []string `json:"allowed_launch_hosts,omitempty"`
	Description          string   `json:"description"`
	Notes                []string `json:"notes,omitempty"`
}

func BuiltinPresets() []Preset {
	return []Preset{
		{ID: "hermes", DisplayName: "Hermes Bot Mode", Vendor: "Nous Research", SourceKind: "harness_native", AccessMode: "harness_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, GroupChat: true, Routines: true, Description: "Connect a Hermes profile/Bot through the authenticated Hermes Sessions API. OnePane preserves the Hermes profile and remote session; Hermes retains its native tools, memory, skills and routines."},
		{ID: "chatgpt-gpt", DisplayName: "ChatGPT GPT", Vendor: "OpenAI", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, AllowedLaunchHosts: []string{"chatgpt.com", "chat.openai.com"}, Description: "Register an existing GPT and launch its supported ChatGPT-hosted conversation surface. OpenAI does not expose Custom GPTs as an embeddable external Bot API."},
		{ID: "grok-bot", DisplayName: "Grok Bot", Vendor: "xAI / SpaceXAI", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, GroupChat: true, Routines: true, AllowedLaunchHosts: []string{"x.ai", "grok.com", "cursor.com"}, Description: "Register a Grok Bot and launch its provider-hosted Bot surface. The current public Grok Bot product documents app-based messaging rather than an external Bot-session API."},
		{ID: "gemini-gem", DisplayName: "Gemini Gem", Vendor: "Google", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, AllowedLaunchHosts: []string{"gemini.google.com"}, Description: "Register a Gemini Gem and launch its Google-hosted conversation surface."},
		{ID: "microsoft-copilot-agent", DisplayName: "Microsoft Copilot Agent", Vendor: "Microsoft", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, AllowedLaunchHosts: []string{"copilot.microsoft.com", "m365.cloud.microsoft", "teams.microsoft.com"}, Description: "Register a Microsoft Copilot agent and launch its Microsoft-hosted surface."},
		{ID: "poe-bot", DisplayName: "Poe Bot", Vendor: "Quora", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, AllowedLaunchHosts: []string{"poe.com"}, Description: "Register a Poe Bot and launch its provider-hosted conversation."},
		{ID: "dify-bot", DisplayName: "Dify App / Bot", Vendor: "Dify", SourceKind: "relay", AccessMode: "relay_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, RequiresBridge: true, Description: "Connect through the OnePane Bot Protocol relay so Dify keeps its own conversation/workflow state."},
		{ID: "flowise-bot", DisplayName: "Flowise Chatflow / Agentflow", Vendor: "Flowise", SourceKind: "relay", AccessMode: "relay_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, RequiresBridge: true, Description: "Connect through the OnePane Bot Protocol relay so Flowise retains native chatflow/agentflow behavior."},
		{ID: "n8n-bot", DisplayName: "n8n Chat / Agent", Vendor: "n8n", SourceKind: "relay", AccessMode: "relay_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, RequiresBridge: true, Description: "Connect an n8n conversational agent through the OnePane Bot Protocol relay."},
		{ID: "openwebui-bot", DisplayName: "Open WebUI Pipeline / Agent", Vendor: "Open WebUI", SourceKind: "relay", AccessMode: "relay_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, RequiresBridge: true, Description: "Connect an Open WebUI conversational runtime through the OnePane Bot Protocol relay."},
		{ID: "custom-bot-relay", DisplayName: "Custom Bot Runtime", Vendor: "Custom", SourceKind: "relay", AccessMode: "relay_api", InAppChat: true, PersistentSessions: true, NativeTools: true, NativeMemory: true, RequiresBridge: true, Description: "Generic persistent Bot bridge implementing OnePane Bot Protocol v1."},
		{ID: "custom-hosted-bot", DisplayName: "Custom Hosted Bot", Vendor: "Custom", SourceKind: "provider_hosted", AccessMode: "hosted_surface", PersistentSessions: true, NativeTools: true, NativeMemory: true, Description: "Register a provider-hosted Bot URL. No credentials are proxied and OnePane does not claim in-app API control."},
	}
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
