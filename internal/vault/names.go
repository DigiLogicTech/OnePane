package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type CredentialScope string

type CredentialKind string

const (
	ScopeDirectProvider CredentialScope = "provider"
	ScopeOmniRoute      CredentialScope = "omniroute"
	ScopePlugin         CredentialScope = "plugin"

	CredentialAPIKey      CredentialKind = "api-key"
	CredentialAccessToken CredentialKind = "access-token"
)

type PluginCredentialCommand struct {
	WorkspaceID  *string
	PluginID     string
	Kind         CredentialKind
	Value        []byte
	DisplayLabel string
	CreatedBy    string
}

func PluginCredentialLogicalName(pluginID string, kind CredentialKind) (string, error) {
	pluginID = normalizeProviderName(pluginID)
	kind = CredentialKind(strings.ToLower(strings.TrimSpace(string(kind))))
	if pluginID == "" {
		return "", fmt.Errorf("%w: plugin id required", ErrInvalid)
	}
	if kind != CredentialAPIKey && kind != CredentialAccessToken {
		return "", fmt.Errorf("%w: unsupported plugin credential kind", ErrInvalid)
	}
	return "plugin/" + pluginID + "/" + string(kind), nil
}

func (s *Service) CreatePluginCredential(ctx context.Context, cmd PluginCredentialCommand) (SecretRecord, error) {
	logical, err := PluginCredentialLogicalName(cmd.PluginID, cmd.Kind)
	if err != nil {
		return SecretRecord{}, err
	}
	pluginID := normalizeProviderName(cmd.PluginID)
	label := strings.TrimSpace(cmd.DisplayLabel)
	if label == "" {
		label = pluginID + " · " + string(cmd.Kind)
	}
	meta, _ := json.Marshal(map[string]any{"credential_scope": "plugin", "plugin_id": pluginID, "credential_kind": string(cmd.Kind), "display_label": label})
	return s.Create(ctx, CreateCommand{WorkspaceID: cmd.WorkspaceID, LogicalName: logical, ProviderType: "plugin:" + pluginID, Value: cmd.Value, Metadata: meta, CreatedBy: cmd.CreatedBy})
}

func (s *Service) ValidatePluginCredentialRef(ctx context.Context, ref, pluginID string) error {
	r, err := s.RecordForRef(ctx, ref)
	if err != nil {
		return err
	}
	pluginID = normalizeProviderName(pluginID)
	want, err := PluginCredentialLogicalName(pluginID, credentialKindFromLogical(r.LogicalName))
	if err != nil {
		return err
	}
	if r.LogicalName != want || r.ProviderType != "plugin:"+pluginID {
		return fmt.Errorf("%w: secret %s is not scoped to plugin %s", ErrInvalid, r.ID, pluginID)
	}
	return nil
}

type ProviderCredentialCommand struct {
	WorkspaceID      *string
	Scope            CredentialScope
	UpstreamProvider string
	Kind             CredentialKind
	Value            []byte
	DisplayLabel     string
	CreatedBy        string
}

// ProviderCredentialLogicalName gives credentials stable, human-readable Vault
// names that make the consumption boundary obvious. Direct xAI and OmniRoute's
// xAI credential must never share a logical name.
func ProviderCredentialLogicalName(scope CredentialScope, upstream string, kind CredentialKind) (string, error) {
	scope = CredentialScope(strings.ToLower(strings.TrimSpace(string(scope))))
	upstream = normalizeProviderName(upstream)
	kind = CredentialKind(strings.ToLower(strings.TrimSpace(string(kind))))
	if scope != ScopeDirectProvider && scope != ScopeOmniRoute {
		return "", fmt.Errorf("%w: unsupported credential scope", ErrInvalid)
	}
	if upstream == "" {
		return "", fmt.Errorf("%w: upstream provider required", ErrInvalid)
	}
	if kind != CredentialAPIKey && kind != CredentialAccessToken {
		return "", fmt.Errorf("%w: unsupported credential kind", ErrInvalid)
	}
	return string(scope) + "/" + upstream + "/" + string(kind), nil
}

func OmniRouteGatewayCredentialLogicalName() string { return "omniroute/gateway/access-token" }

func normalizeProviderName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", "\\", "-", ":", "-")
	v = replacer.Replace(v)
	for strings.Contains(v, "--") {
		v = strings.ReplaceAll(v, "--", "-")
	}
	return strings.Trim(v, "-.")
}

func (s *Service) CreateProviderCredential(ctx context.Context, cmd ProviderCredentialCommand) (SecretRecord, error) {
	logical, err := ProviderCredentialLogicalName(cmd.Scope, cmd.UpstreamProvider, cmd.Kind)
	if err != nil {
		return SecretRecord{}, err
	}
	provider := normalizeProviderName(cmd.UpstreamProvider)
	label := strings.TrimSpace(cmd.DisplayLabel)
	if label == "" {
		if cmd.Scope == ScopeOmniRoute {
			label = "OmniRoute · " + provider + " · " + string(cmd.Kind)
		} else {
			label = provider + " · " + string(cmd.Kind)
		}
	}
	meta, _ := json.Marshal(map[string]any{
		"credential_scope":  string(cmd.Scope),
		"upstream_provider": provider,
		"credential_kind":   string(cmd.Kind),
		"display_label":     label,
	})
	return s.Create(ctx, CreateCommand{
		WorkspaceID:  cmd.WorkspaceID,
		LogicalName:  logical,
		ProviderType: string(cmd.Scope) + ":" + provider,
		Value:        cmd.Value,
		Metadata:     meta,
		CreatedBy:    cmd.CreatedBy,
	})
}

func (s *Service) RecordForRef(ctx context.Context, ref string) (SecretRecord, error) {
	if !strings.HasPrefix(ref, "vault:") {
		return SecretRecord{}, fmt.Errorf("unsupported secret_ref %q", ref)
	}
	idv := strings.TrimSpace(strings.TrimPrefix(ref, "vault:"))
	if idv == "" {
		return SecretRecord{}, ErrInvalid
	}
	return s.record(ctx, idv)
}

// ValidateProviderCredentialRef prevents a credential created for OmniRoute
// from being silently reused as a direct-provider credential (and vice versa).
func (s *Service) ValidateProviderCredentialRef(ctx context.Context, ref string, scope CredentialScope, upstream string) error {
	r, err := s.RecordForRef(ctx, ref)
	if err != nil {
		return err
	}
	logical, err := ProviderCredentialLogicalName(scope, upstream, credentialKindFromLogical(r.LogicalName))
	if err != nil {
		return err
	}
	if r.LogicalName != logical || r.ProviderType != string(scope)+":"+normalizeProviderName(upstream) {
		return fmt.Errorf("%w: secret %s belongs to %s/%s, not %s/%s", ErrInvalid, r.ID, r.LogicalName, r.ProviderType, scope, normalizeProviderName(upstream))
	}
	return nil
}

func credentialKindFromLogical(logical string) CredentialKind {
	if strings.HasSuffix(logical, "/access-token") {
		return CredentialAccessToken
	}
	return CredentialAPIKey
}

type AgentRuntimeCredentialCommand struct {
	WorkspaceID  *string
	RuntimeKind  string
	Kind         CredentialKind
	Value        []byte
	DisplayLabel string
	CreatedBy    string
}

func AgentRuntimeCredentialLogicalName(runtimeKind string, kind CredentialKind) (string, error) {
	runtimeKind = normalizeProviderName(runtimeKind)
	kind = CredentialKind(strings.ToLower(strings.TrimSpace(string(kind))))
	if runtimeKind == "" {
		return "", fmt.Errorf("%w: runtime kind required", ErrInvalid)
	}
	if kind != CredentialAPIKey && kind != CredentialAccessToken {
		return "", fmt.Errorf("%w: unsupported runtime credential kind", ErrInvalid)
	}
	return "agent-runtime/" + runtimeKind + "/" + string(kind), nil
}

func (s *Service) CreateAgentRuntimeCredential(ctx context.Context, cmd AgentRuntimeCredentialCommand) (SecretRecord, error) {
	logical, err := AgentRuntimeCredentialLogicalName(cmd.RuntimeKind, cmd.Kind)
	if err != nil {
		return SecretRecord{}, err
	}
	runtimeKind := normalizeProviderName(cmd.RuntimeKind)
	label := strings.TrimSpace(cmd.DisplayLabel)
	if label == "" {
		label = runtimeKind + " · " + string(cmd.Kind)
	}
	meta, _ := json.Marshal(map[string]any{"credential_scope": "agent-runtime", "runtime_kind": runtimeKind, "credential_kind": string(cmd.Kind), "display_label": label})
	return s.Create(ctx, CreateCommand{WorkspaceID: cmd.WorkspaceID, LogicalName: logical, ProviderType: "agent-runtime:" + runtimeKind, Value: cmd.Value, Metadata: meta, CreatedBy: cmd.CreatedBy})
}

func (s *Service) ValidateAgentRuntimeCredentialRef(ctx context.Context, ref, runtimeKind string) error {
	r, err := s.RecordForRef(ctx, ref)
	if err != nil {
		return err
	}
	runtimeKind = normalizeProviderName(runtimeKind)
	want, err := AgentRuntimeCredentialLogicalName(runtimeKind, credentialKindFromLogical(r.LogicalName))
	if err != nil {
		return err
	}
	if r.LogicalName != want || r.ProviderType != "agent-runtime:"+runtimeKind {
		return fmt.Errorf("%w: secret %s is not scoped to agent runtime %s", ErrInvalid, r.ID, runtimeKind)
	}
	return nil
}

type GatewayCredentialCommand struct {
	WorkspaceID  *string
	GatewayID    string
	Kind         string
	Value        []byte
	DisplayLabel string
	CreatedBy    string
}

var gatewayCredentialKinds = map[string]struct{}{
	"access-token": {}, "api-key": {}, "bot-token": {}, "password": {},
	"webhook-url": {}, "relay-token": {}, "smtp-password": {}, "auth-token": {},
}

func GatewayCredentialLogicalName(gatewayID, kind string) (string, error) {
	gatewayID = normalizeProviderName(gatewayID)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if gatewayID == "" {
		return "", fmt.Errorf("%w: gateway id required", ErrInvalid)
	}
	if _, ok := gatewayCredentialKinds[kind]; !ok {
		return "", fmt.Errorf("%w: unsupported gateway credential kind %q", ErrInvalid, kind)
	}
	return "gateway/" + gatewayID + "/" + kind, nil
}

func (s *Service) CreateGatewayCredential(ctx context.Context, cmd GatewayCredentialCommand) (SecretRecord, error) {
	logical, err := GatewayCredentialLogicalName(cmd.GatewayID, cmd.Kind)
	if err != nil {
		return SecretRecord{}, err
	}
	gatewayID := normalizeProviderName(cmd.GatewayID)
	kind := strings.ToLower(strings.TrimSpace(cmd.Kind))
	label := strings.TrimSpace(cmd.DisplayLabel)
	if label == "" {
		label = gatewayID + " · " + kind
	}
	meta, _ := json.Marshal(map[string]any{"credential_scope": "gateway", "gateway_id": gatewayID, "credential_kind": kind, "display_label": label})
	return s.Create(ctx, CreateCommand{WorkspaceID: cmd.WorkspaceID, LogicalName: logical, ProviderType: "gateway:" + gatewayID, Value: cmd.Value, Metadata: meta, CreatedBy: cmd.CreatedBy})
}

func (s *Service) ValidateGatewayCredentialRef(ctx context.Context, ref, gatewayID string) error {
	r, err := s.RecordForRef(ctx, ref)
	if err != nil {
		return err
	}
	gatewayID = normalizeProviderName(gatewayID)
	if !strings.HasPrefix(r.LogicalName, "gateway/"+gatewayID+"/") || r.ProviderType != "gateway:"+gatewayID {
		return fmt.Errorf("%w: secret %s is not scoped to gateway %s", ErrInvalid, r.ID, gatewayID)
	}
	return nil
}

type BotCredentialCommand struct {
	WorkspaceID  *string
	BotPresetID  string
	Kind         CredentialKind
	Value        []byte
	DisplayLabel string
	CreatedBy    string
}

func BotCredentialLogicalName(botPresetID string, kind CredentialKind) (string, error) {
	botPresetID = normalizeProviderName(botPresetID)
	kind = CredentialKind(strings.ToLower(strings.TrimSpace(string(kind))))
	if botPresetID == "" {
		return "", fmt.Errorf("%w: bot preset id required", ErrInvalid)
	}
	if kind != CredentialAPIKey && kind != CredentialAccessToken {
		return "", fmt.Errorf("%w: unsupported bot credential kind", ErrInvalid)
	}
	return "bot/" + botPresetID + "/" + string(kind), nil
}

func (s *Service) CreateBotCredential(ctx context.Context, cmd BotCredentialCommand) (SecretRecord, error) {
	logical, err := BotCredentialLogicalName(cmd.BotPresetID, cmd.Kind)
	if err != nil {
		return SecretRecord{}, err
	}
	preset := normalizeProviderName(cmd.BotPresetID)
	label := strings.TrimSpace(cmd.DisplayLabel)
	if label == "" {
		label = preset + " · " + string(cmd.Kind)
	}
	meta, _ := json.Marshal(map[string]any{"credential_scope": "bot", "bot_preset_id": preset, "credential_kind": string(cmd.Kind), "display_label": label})
	return s.Create(ctx, CreateCommand{WorkspaceID: cmd.WorkspaceID, LogicalName: logical, ProviderType: "bot:" + preset, Value: cmd.Value, Metadata: meta, CreatedBy: cmd.CreatedBy})
}

func (s *Service) ValidateBotCredentialRef(ctx context.Context, ref, botPresetID string) error {
	r, err := s.RecordForRef(ctx, ref)
	if err != nil {
		return err
	}
	preset := normalizeProviderName(botPresetID)
	want, err := BotCredentialLogicalName(preset, credentialKindFromLogical(r.LogicalName))
	if err != nil {
		return err
	}
	if r.LogicalName != want || r.ProviderType != "bot:"+preset {
		return fmt.Errorf("%w: secret %s is not scoped to bot preset %s", ErrInvalid, r.ID, preset)
	}
	return nil
}
