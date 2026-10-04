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
	"time"

	"github.com/DigiLogicTech/OnePane/internal/inference"
)

type OmniRouteProbe struct {
	Reachable              bool     `json:"reachable"`
	BaseURL                string   `json:"base_url"`
	Models                 []string `json:"models"`
	StrictZeroCostVerified bool     `json:"strict_zero_cost_verified"`
	SettingsReadable       bool     `json:"settings_readable"`
}

type OmniRouteCommand struct {
	WorkspaceID           *string
	BaseURL               string
	SecretRef             *string
	RequireStrictZeroCost bool
	DefaultModel          string
	ActorPrincipalID      *string
	RequestID             *string
	TraceID               *string
}

type OmniRouteResult struct {
	Probe      OmniRouteProbe               `json:"probe"`
	Provider   inference.ProviderConnection `json:"provider"`
	Model      inference.Model              `json:"model"`
	Deployment inference.ModelDeployment    `json:"deployment"`
}

type Service struct {
	inference *inference.Service
	secrets   inference.SecretResolver
	client    *http.Client
}

func New(inf *inference.Service, secrets inference.SecretResolver) *Service {
	return &Service{inference: inf, secrets: secrets, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *Service) Connection(ctx context.Context, id string) (inference.ProviderConnection, error) {
	if s.inference == nil {
		return inference.ProviderConnection{}, fmt.Errorf("%w: inference service unavailable", ErrInvalidOnboarding)
	}
	return s.inference.Provider(ctx, id)
}

func (s *Service) RevokeProvider(ctx context.Context, id string, actor *string) (inference.ProviderConnection, error) {
	if s.inference == nil {
		return inference.ProviderConnection{}, fmt.Errorf("%w: inference service unavailable", ErrInvalidOnboarding)
	}
	p, err := s.inference.Provider(ctx, id)
	if err != nil {
		return inference.ProviderConnection{}, err
	}
	if p.Status == inference.ProviderRevoked {
		return p, nil
	}
	return s.inference.SetProviderStatus(ctx, inference.SetProviderStatusCommand{ConnectionID: p.ID, ExpectedRevision: p.Revision, Status: inference.ProviderRevoked, ActorPrincipalID: actor, Reason: "operator revoked provider connection"})
}

func (s *Service) Connections(ctx context.Context, workspaceID *string) ([]inference.ProviderConnection, error) {
	if s.inference == nil {
		return nil, fmt.Errorf("%w: inference service unavailable", ErrInvalidOnboarding)
	}
	return s.inference.Providers(ctx, workspaceID)
}

func normalizeOmniURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "http://127.0.0.1:20128/v1"
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: invalid OmniRoute URL", ErrInvalidOnboarding)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(loopback && u.Scheme == "http") {
		return "", fmt.Errorf("%w: OmniRoute endpoints must use HTTPS except loopback development endpoints", ErrInvalidOnboarding)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: credentials must not be embedded in URL", ErrInvalidOnboarding)
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func (s *Service) addAuth(ctx context.Context, req *http.Request, secretRef *string) error {
	if secretRef == nil {
		return nil
	}
	if s.secrets == nil {
		return fmt.Errorf("secret resolver unavailable")
	}
	v, err := s.secrets.Resolve(ctx, *secretRef)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+v)
	return nil
}
func (s *Service) ProbeOmniRoute(ctx context.Context, baseURL string, secretRef *string) (OmniRouteProbe, error) {
	base, err := normalizeOmniURL(baseURL)
	if err != nil {
		return OmniRouteProbe{}, err
	}
	out := OmniRouteProbe{BaseURL: base}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err := s.addAuth(ctx, req, secretRef); err != nil {
		return out, err
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
		return out, fmt.Errorf("%w: /models returned HTTP %d", ErrProbeFailed, resp.StatusCode)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &models); err != nil {
		return out, fmt.Errorf("%w: invalid /models response: %v", ErrProbeFailed, err)
	}
	for _, m := range models.Data {
		if strings.TrimSpace(m.ID) != "" {
			out.Models = append(out.Models, m.ID)
		}
	}
	out.Reachable = true
	root := strings.TrimSuffix(base, "/v1")
	settingsReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, root+"/api/settings/export-json", nil)
	if err := s.addAuth(ctx, settingsReq, secretRef); err == nil {
		if settingsResp, e := s.client.Do(settingsReq); e == nil {
			defer settingsResp.Body.Close()
			if settingsResp.StatusCode >= 200 && settingsResp.StatusCode < 300 {
				b, _ := io.ReadAll(io.LimitReader(settingsResp.Body, 2<<20))
				var v any
				if json.Unmarshal(b, &v) == nil {
					out.SettingsReadable = true
					out.StrictZeroCostVerified = findStringField(v, "freeAccessPolicy", "strict")
				}
			}
		}
	}
	return out, nil
}
func findStringField(v any, key, want string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == key {
				if s, ok := val.(string); ok && strings.EqualFold(s, want) {
					return true
				}
			}
			if findStringField(val, key, want) {
				return true
			}
		}
	case []any:
		for _, val := range x {
			if findStringField(val, key, want) {
				return true
			}
		}
	}
	return false
}

func (s *Service) AddOmniRoute(ctx context.Context, cmd OmniRouteCommand) (OmniRouteResult, error) {
	if s.inference == nil {
		return OmniRouteResult{}, fmt.Errorf("%w: inference service unavailable", ErrInvalidOnboarding)
	}
	probe, err := s.ProbeOmniRoute(ctx, cmd.BaseURL, cmd.SecretRef)
	if err != nil {
		return OmniRouteResult{Probe: probe}, err
	}
	if cmd.RequireStrictZeroCost && !probe.StrictZeroCostVerified {
		return OmniRouteResult{Probe: probe}, ErrStrictZeroCostNotVerified
	}
	modelRef := strings.TrimSpace(cmd.DefaultModel)
	if modelRef == "" {
		if cmd.RequireStrictZeroCost {
			modelRef = "auto/coding"
		} else {
			modelRef = "auto"
		}
	}
	cost := "provider_managed"
	hard := false
	if probe.StrictZeroCostVerified && cmd.RequireStrictZeroCost {
		cost = "free"
		hard = true
	}
	cfg, _ := json.Marshal(map[string]any{"base_url": probe.BaseURL, "chat_path": "/chat/completions", "models_path": "/models", "data_policy": map[string]any{"max_confidentiality": "public", "allowed_residency": []string{"any"}, "destination_kind": "cloud", "allow_raw_secrets": false}, "scheduling": map[string]any{"cost_class": cost, "hard_zero_incremental_cost": hard, "cost_source": "omniroute_strict_zero_cost_probe"}})
	auth := "none"
	if cmd.SecretRef != nil {
		auth = "bearer"
	}
	p, err := s.inference.RegisterProvider(ctx, inference.RegisterProviderCommand{WorkspaceID: cmd.WorkspaceID, Provider: "omniroute", DisplayName: "OmniRoute", AuthType: auth, SecretRef: cmd.SecretRef, ConnectionJSON: cfg, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return OmniRouteResult{Probe: probe}, err
	}
	p, err = s.inference.SetProviderStatus(ctx, inference.SetProviderStatusCommand{ConnectionID: p.ID, ExpectedRevision: p.Revision, Status: inference.ProviderConnected, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "OmniRoute /v1/models probe succeeded"})
	if err != nil {
		return OmniRouteResult{Probe: probe, Provider: p}, err
	}
	providerName := "omniroute"
	meta, _ := json.Marshal(map[string]any{"virtual_route": true, "onboarding_preset": "omniroute", "strict_zero_cost_verified": probe.StrictZeroCostVerified})
	m, err := s.inference.RegisterModel(ctx, inference.RegisterModelCommand{ProviderName: &providerName, ModelRef: modelRef, ModalitiesJSON: json.RawMessage(`["text"]`), StaticMetadataJSON: meta, TrustState: inference.ModelUserTrusted, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return OmniRouteResult{Probe: probe, Provider: p}, err
	}
	runtime := "openai-compatible"
	dcfg, _ := json.Marshal(map[string]any{"scheduling": map[string]any{"cost_class": cost, "hard_zero_incremental_cost": hard}})
	d, err := s.inference.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: m.ID, ProviderConnectionID: &p.ID, RuntimeName: &runtime, RuntimeConfigJSON: dcfg, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return OmniRouteResult{Probe: probe, Provider: p, Model: m}, err
	}
	d, err = s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: d.ID, ExpectedRevision: d.Revision, Status: inference.DeploymentReady, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "OmniRoute probe succeeded"})
	if err != nil {
		return OmniRouteResult{Probe: probe, Provider: p, Model: m, Deployment: d}, err
	}
	return OmniRouteResult{Probe: probe, Provider: p, Model: m, Deployment: d}, nil
}
