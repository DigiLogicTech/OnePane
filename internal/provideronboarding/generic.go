package provideronboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/vault"
)

type providerCredentialValidator interface {
	ValidateProviderCredentialRef(context.Context, string, vault.CredentialScope, string) error
}

type ProviderProbe struct {
	PresetID  PresetID `json:"preset_id"`
	Reachable bool     `json:"reachable"`
	BaseURL   string   `json:"base_url"`
	Models    []string `json:"models,omitempty"`
	Supported bool     `json:"supported"`
}

type ProviderCommand struct {
	WorkspaceID          *string
	PresetID             PresetID
	BaseURL              string
	SecretRef            *string
	ModelRef             string
	ActivateWithoutProbe bool
	ActorPrincipalID     *string
	RequestID            *string
	TraceID              *string
}

type ProviderResult struct {
	Preset      Preset                       `json:"preset"`
	Probe       *ProviderProbe               `json:"probe,omitempty"`
	Provider    inference.ProviderConnection `json:"provider"`
	Model       inference.Model              `json:"model"`
	Deployment  inference.ModelDeployment    `json:"deployment"`
	Schedulable bool                         `json:"schedulable"`
}

func normalizeProviderURL(p Preset, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = strings.TrimSpace(p.DefaultEndpoint)
	}
	if raw == "" || strings.Contains(raw, "{") || strings.Contains(raw, "}") {
		return "", fmt.Errorf("%w: a concrete provider endpoint is required", ErrInvalidOnboarding)
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%w: invalid provider endpoint", ErrInvalidOnboarding)
	}
	host := strings.ToLower(u.Hostname())
	loopback := strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	if u.Scheme != "https" && !(loopback && u.Scheme == "http") {
		return "", fmt.Errorf("%w: provider endpoints must use HTTPS except loopback development endpoints", ErrInvalidOnboarding)
	}
	if len(p.AllowedHostSuffixes) > 0 {
		allowed := false
		for _, suffix := range p.AllowedHostSuffixes {
			suffix = strings.ToLower(strings.TrimSpace(suffix))
			if suffix == "" {
				continue
			}
			if strings.HasPrefix(suffix, ".") {
				if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
					allowed = true
					break
				}
			} else if host == suffix {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("%w: host %q is not valid for %s", ErrEndpointNotAllowed, host, p.ID)
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *Service) validateDirectCredential(ctx context.Context, p Preset, ref *string) error {
	if p.AuthType == "none" {
		return nil
	}
	if ref == nil || strings.TrimSpace(*ref) == "" {
		return fmt.Errorf("%w: secret_ref is required", ErrInvalidOnboarding)
	}
	if s.secrets == nil {
		return fmt.Errorf("%w: secret resolver is unavailable", ErrInvalidOnboarding)
	}
	if v, ok := s.secrets.(providerCredentialValidator); ok && p.CredentialProvider != "" {
		if err := v.ValidateProviderCredentialRef(ctx, *ref, vault.ScopeDirectProvider, p.CredentialProvider); err != nil {
			return fmt.Errorf("%w: %v", ErrCredentialScope, err)
		}
	}
	return nil
}

func (s *Service) ProbeProvider(ctx context.Context, presetID PresetID, baseURL string, secretRef *string) (ProviderProbe, error) {
	p, ok := PresetByID(presetID)
	if !ok || p.ID == PresetOmniRoute || p.ID == PresetOpenAIChatGPTPlan {
		return ProviderProbe{}, fmt.Errorf("%w: unsupported generic provider preset", ErrInvalidOnboarding)
	}
	base, err := normalizeProviderURL(p, baseURL)
	if err != nil {
		return ProviderProbe{}, err
	}
	out := ProviderProbe{PresetID: p.ID, BaseURL: base, Supported: strings.TrimSpace(p.ProbePath) != ""}
	if !out.Supported {
		return out, nil
	}
	if err := s.validateDirectCredential(ctx, p, secretRef); err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+ensureLeadingSlash(p.ProbePath), nil)
	if err != nil {
		return out, err
	}
	if secretRef != nil && p.AuthType != "none" {
		secret, err := s.secrets.Resolve(ctx, *secretRef)
		if err != nil {
			return out, err
		}
		header := p.AuthHeader
		if header == "" {
			header = "Authorization"
		}
		value := secret
		if !p.AuthRaw && strings.TrimSpace(p.AuthPrefix) != "" {
			value = strings.TrimSpace(p.AuthPrefix) + " " + secret
		}
		req.Header.Set(header, value)
	}
	if p.ID == PresetAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrProbeFailed, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("%w: models probe returned HTTP %d", ErrProbeFailed, resp.StatusCode)
	}
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return out, fmt.Errorf("%w: invalid model-list JSON", ErrProbeFailed)
	}
	for _, m := range env.Data {
		if strings.TrimSpace(m.ID) != "" {
			out.Models = append(out.Models, strings.TrimSpace(m.ID))
		}
	}
	for _, m := range env.Models {
		if strings.TrimSpace(m.ID) != "" {
			out.Models = append(out.Models, strings.TrimSpace(m.ID))
		}
	}
	out.Reachable = true
	return out, nil
}

func ensureLeadingSlash(v string) string {
	if v == "" || strings.HasPrefix(v, "/") {
		return v
	}
	return "/" + v
}

func (s *Service) AddProvider(ctx context.Context, cmd ProviderCommand) (ProviderResult, error) {
	if s.inference == nil {
		return ProviderResult{}, fmt.Errorf("%w: inference service unavailable", ErrInvalidOnboarding)
	}
	p, ok := PresetByID(cmd.PresetID)
	if !ok || p.ID == PresetOmniRoute || p.ID == PresetOpenAIChatGPTPlan {
		return ProviderResult{}, fmt.Errorf("%w: use the dedicated onboarding flow for %s", ErrInvalidOnboarding, cmd.PresetID)
	}
	base, err := normalizeProviderURL(p, cmd.BaseURL)
	if err != nil {
		return ProviderResult{Preset: p}, err
	}
	if err := s.validateDirectCredential(ctx, p, cmd.SecretRef); err != nil {
		return ProviderResult{Preset: p}, err
	}
	modelRef := strings.TrimSpace(cmd.ModelRef)
	if modelRef == "" {
		return ProviderResult{Preset: p}, fmt.Errorf("%w: model_ref is required", ErrInvalidOnboarding)
	}

	cfg, _ := json.Marshal(map[string]any{
		"base_url":              base,
		"chat_path":             "/chat/completions",
		"auth_header":           p.AuthHeader,
		"auth_prefix":           p.AuthPrefix,
		"auth_raw":              p.AuthRaw,
		"allowed_host_suffixes": p.AllowedHostSuffixes,
		"preset_id":             p.ID,
		"data_policy":           map[string]any{"max_confidentiality": "public", "allowed_residency": []string{"any"}, "destination_kind": "cloud", "allow_raw_secrets": false},
		"scheduling":            map[string]any{"cost_class": "provider_managed", "hard_zero_incremental_cost": false, "cost_source": "provider_preset"},
	})
	provider, err := s.inference.RegisterProvider(ctx, inference.RegisterProviderCommand{WorkspaceID: cmd.WorkspaceID, Provider: string(p.ID), DisplayName: p.DisplayName, AuthType: p.AuthType, SecretRef: cmd.SecretRef, ConnectionJSON: cfg, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return ProviderResult{Preset: p}, err
	}

	providerName := string(p.ID)
	meta, _ := json.Marshal(map[string]any{"onboarding_preset": p.ID, "transport": p.Transport, "endpoint": base})
	model, err := s.inference.RegisterModel(ctx, inference.RegisterModelCommand{ProviderName: &providerName, ModelRef: modelRef, ModalitiesJSON: json.RawMessage(`["text"]`), StaticMetadataJSON: meta, TrustState: inference.ModelUserTrusted, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return ProviderResult{Preset: p, Provider: provider}, err
	}
	runtime := p.Transport
	dcfg, _ := json.Marshal(map[string]any{"allowed_host_suffixes": p.AllowedHostSuffixes, "scheduling": map[string]any{"cost_class": "provider_managed", "hard_zero_incremental_cost": false}})
	deployment, err := s.inference.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: model.ID, ProviderConnectionID: &provider.ID, RuntimeName: &runtime, RuntimeConfigJSON: dcfg, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return ProviderResult{Preset: p, Provider: provider, Model: model}, err
	}
	out := ProviderResult{Preset: p, Provider: provider, Model: model, Deployment: deployment}

	var probe *ProviderProbe
	verified := false
	if p.ProbePath != "" {
		pr, probeErr := s.ProbeProvider(ctx, p.ID, base, cmd.SecretRef)
		probe = &pr
		if probeErr == nil && pr.Reachable {
			verified = true
		}
	}
	if verified || cmd.ActivateWithoutProbe {
		reason := "provider model-list probe succeeded"
		if !verified {
			reason = "operator explicitly activated a provider without an automated probe"
		}
		provider, err = s.inference.SetProviderStatus(ctx, inference.SetProviderStatusCommand{ConnectionID: provider.ID, ExpectedRevision: provider.Revision, Status: inference.ProviderConnected, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: reason})
		if err != nil {
			return out, err
		}
		deployment, err = s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: deployment.ID, ExpectedRevision: deployment.Revision, Status: inference.DeploymentQualifying, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: reason})
		if err != nil {
			return out, err
		}
		deployment, err = s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: deployment.ID, ExpectedRevision: deployment.Revision, Status: inference.DeploymentReady, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "operator/provider connectivity qualification accepted"})
		if err != nil {
			return out, err
		}
		out.Provider, out.Deployment, out.Schedulable = provider, deployment, true
	}
	out.Probe = probe
	return out, nil
}
