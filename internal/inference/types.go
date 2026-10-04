package inference

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ProviderStatus string

const (
	ProviderConnected      ProviderStatus = "connected"
	ProviderDegraded       ProviderStatus = "degraded"
	ProviderRateLimited    ProviderStatus = "rate_limited"
	ProviderExpired        ProviderStatus = "expired"
	ProviderReauthRequired ProviderStatus = "reauth_required"
	ProviderRevoked        ProviderStatus = "revoked"
	ProviderUnavailable    ProviderStatus = "unavailable"
)

type ModelTrustState string

const (
	ModelTrusted     ModelTrustState = "trusted"
	ModelUserTrusted ModelTrustState = "user_trusted"
	ModelQuarantined ModelTrustState = "quarantined"
	ModelRevoked     ModelTrustState = "revoked"
)

type DeploymentStatus string

const (
	DeploymentDiscovered  DeploymentStatus = "discovered"
	DeploymentQualifying  DeploymentStatus = "qualifying"
	DeploymentReady       DeploymentStatus = "ready"
	DeploymentDegraded    DeploymentStatus = "degraded"
	DeploymentDraining    DeploymentStatus = "draining"
	DeploymentUnavailable DeploymentStatus = "unavailable"
	DeploymentDisabled    DeploymentStatus = "disabled"
)

type ResidencyState string

const (
	ResidencyStopped  ResidencyState = "stopped"
	ResidencyLoading  ResidencyState = "loading"
	ResidencyResident ResidencyState = "resident"
	ResidencyBusy     ResidencyState = "busy"
	ResidencyDraining ResidencyState = "draining"
	ResidencyFailed   ResidencyState = "failed"
)

type ProviderConnection struct {
	ID             string
	WorkspaceID    *string
	Provider       string
	DisplayName    string
	AuthType       string
	SecretRef      *string
	Status         ProviderStatus
	ConnectionJSON json.RawMessage
	RetryAfter     *int64
	Revision       int64
	CreatedAt      int64
	UpdatedAt      int64
}

type Model struct {
	ID                 string
	ProviderName       *string
	ModelRef           string
	Architecture       *string
	RevisionRef        *string
	WeightsHash        *string
	Quantization       *string
	ModalitiesJSON     json.RawMessage
	StaticMetadataJSON json.RawMessage
	TrustState         ModelTrustState
	CreatedAt          int64
	UpdatedAt          int64
}

type ModelDeployment struct {
	ID                    string
	ModelID               string
	NodeID                *string
	ProviderConnectionID  *string
	RuntimeName           *string
	RuntimeVersion        *string
	RuntimeConfigJSON     json.RawMessage
	Status                DeploymentStatus
	ResidencyState        *ResidencyState
	ContextMaxReported    *int64
	ContextMaxVerified    *int64
	DeploymentFingerprint string
	Revision              int64
	DiscoveredAt          int64
	UpdatedAt             int64
}

type RegisterProviderCommand struct {
	WorkspaceID      *string
	Provider         string
	DisplayName      string
	AuthType         string
	SecretRef        *string
	ConnectionJSON   json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type SetProviderStatusCommand struct {
	ConnectionID     string
	ExpectedRevision int64
	Status           ProviderStatus
	RetryAfter       *int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	Reason           string
}

type RegisterModelCommand struct {
	ProviderName       *string
	ModelRef           string
	Architecture       *string
	RevisionRef        *string
	WeightsHash        *string
	Quantization       *string
	ModalitiesJSON     json.RawMessage
	StaticMetadataJSON json.RawMessage
	TrustState         ModelTrustState
	ActorPrincipalID   *string
	RequestID          *string
	TraceID            *string
}

type SetModelTrustCommand struct {
	ModelID            string
	ExpectedTrustState ModelTrustState
	TrustState         ModelTrustState
	ActorPrincipalID   *string
	RequestID          *string
	TraceID            *string
	Reason             string
}

type RegisterDeploymentCommand struct {
	ModelID              string
	NodeID               *string
	ProviderConnectionID *string
	RuntimeName          *string
	RuntimeVersion       *string
	RuntimeConfigJSON    json.RawMessage
	ContextMaxReported   *int64
	ActorPrincipalID     *string
	RequestID            *string
	TraceID              *string
}

type SetDeploymentStatusCommand struct {
	DeploymentID     string
	ExpectedRevision int64
	Status           DeploymentStatus
	ResidencyState   *ResidencyState
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	Reason           string
}

var (
	ErrInvalidCommand    = errors.New("invalid inference catalog command")
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrWorkspaceInactive = errors.New("workspace is not active")
	ErrProviderRevoked   = errors.New("provider connection is revoked")
	ErrModelRevoked      = errors.New("model is revoked")
	ErrModelQuarantined  = errors.New("model is quarantined")
	ErrUnsafeConfig      = errors.New("configuration contains a raw secret-like field")
	ErrInvalidTransition = errors.New("invalid state transition")
)

func ValidProviderStatus(s ProviderStatus) bool {
	switch s {
	case ProviderConnected, ProviderDegraded, ProviderRateLimited, ProviderExpired,
		ProviderReauthRequired, ProviderRevoked, ProviderUnavailable:
		return true
	default:
		return false
	}
}

func ValidModelTrustState(s ModelTrustState) bool {
	switch s {
	case ModelTrusted, ModelUserTrusted, ModelQuarantined, ModelRevoked:
		return true
	default:
		return false
	}
}

func ValidDeploymentStatus(s DeploymentStatus) bool {
	switch s {
	case DeploymentDiscovered, DeploymentQualifying, DeploymentReady, DeploymentDegraded,
		DeploymentDraining, DeploymentUnavailable, DeploymentDisabled:
		return true
	default:
		return false
	}
}

func ValidResidencyState(s ResidencyState) bool {
	switch s {
	case ResidencyStopped, ResidencyLoading, ResidencyResident, ResidencyBusy, ResidencyDraining, ResidencyFailed:
		return true
	default:
		return false
	}
}

func CanDeploymentTransition(from, to DeploymentStatus) bool {
	if from == to || !ValidDeploymentStatus(from) || !ValidDeploymentStatus(to) {
		return false
	}
	switch from {
	case DeploymentDiscovered:
		return to == DeploymentQualifying || to == DeploymentUnavailable || to == DeploymentDisabled
	case DeploymentQualifying:
		return to == DeploymentReady || to == DeploymentDegraded || to == DeploymentUnavailable || to == DeploymentDisabled
	case DeploymentReady:
		return to == DeploymentDegraded || to == DeploymentDraining || to == DeploymentUnavailable || to == DeploymentDisabled
	case DeploymentDegraded:
		return to == DeploymentReady || to == DeploymentDraining || to == DeploymentUnavailable || to == DeploymentDisabled
	case DeploymentDraining:
		return to == DeploymentUnavailable || to == DeploymentDisabled
	case DeploymentUnavailable:
		return to == DeploymentDiscovered || to == DeploymentQualifying || to == DeploymentDisabled
	case DeploymentDisabled:
		return to == DeploymentDiscovered
	default:
		return false
	}
}

func normalizeJSON(raw json.RawMessage, defaultValue string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(defaultValue)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON: %w", err)
	}
	return b, nil
}

func rejectSecretLikeJSON(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	var walk func(any) error
	walk = func(x any) error {
		switch t := x.(type) {
		case map[string]any:
			for k, value := range t {
				key := strings.ToLower(strings.TrimSpace(k))
				switch key {
				case "api_key", "apikey", "access_token", "refresh_token", "token", "password", "secret", "client_secret", "authorization":
					return fmt.Errorf("%w: %s", ErrUnsafeConfig, k)
				}
				if err := walk(value); err != nil {
					return err
				}
			}
		case []any:
			for _, value := range t {
				if err := walk(value); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(v)
}

func deploymentFingerprint(cmd RegisterDeploymentCommand, canonicalConfig json.RawMessage) string {
	payload, _ := json.Marshal(struct {
		ModelID              string          `json:"model_id"`
		NodeID               *string         `json:"node_id,omitempty"`
		ProviderConnectionID *string         `json:"provider_connection_id,omitempty"`
		RuntimeName          *string         `json:"runtime_name,omitempty"`
		RuntimeVersion       *string         `json:"runtime_version,omitempty"`
		RuntimeConfig        json.RawMessage `json:"runtime_config"`
	}{
		ModelID: cmd.ModelID, NodeID: cmd.NodeID, ProviderConnectionID: cmd.ProviderConnectionID,
		RuntimeName: cmd.RuntimeName, RuntimeVersion: cmd.RuntimeVersion, RuntimeConfig: canonicalConfig,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type ProviderDataPolicy struct {
	MaxConfidentiality string   `json:"max_confidentiality"`
	AllowedResidency   []string `json:"allowed_residency"`
	DestinationKind    string   `json:"destination_kind"`
	AllowRawSecrets    bool     `json:"allow_raw_secrets"`
}

func normalizeProviderConnectionJSON(raw json.RawMessage) (json.RawMessage, error) {
	canonical, err := normalizeJSON(raw, "{}")
	if err != nil {
		return nil, err
	}
	if err := rejectSecretLikeJSON(canonical); err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(canonical, &obj); err != nil {
		return nil, err
	}
	if _, ok := obj["data_policy"]; !ok {
		obj["data_policy"] = map[string]any{
			"max_confidentiality": "public",
			"allowed_residency":   []string{"any"},
			"destination_kind":    "cloud",
			"allow_raw_secrets":   false,
		}
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		DataPolicy ProviderDataPolicy `json:"data_policy"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, err
	}
	p := wrapper.DataPolicy
	if p.DestinationKind == "" {
		p.DestinationKind = "cloud"
		obj["data_policy"] = p
		b, err = json.Marshal(obj)
		if err != nil {
			return nil, err
		}
	}
	if p.DestinationKind != "origin_node" && p.DestinationKind != "trusted_node" && p.DestinationKind != "cloud" && p.DestinationKind != "untrusted" {
		return nil, fmt.Errorf("invalid provider destination_kind %q", p.DestinationKind)
	}
	switch p.MaxConfidentiality {
	case "public", "internal", "confidential", "secret":
	default:
		return nil, fmt.Errorf("data_policy.max_confidentiality must be public/internal/confidential/secret")
	}
	if p.AllowRawSecrets {
		return nil, fmt.Errorf("provider data policy cannot allow raw secrets")
	}
	if len(p.AllowedResidency) == 0 {
		return nil, fmt.Errorf("data_policy.allowed_residency is required")
	}
	seen := map[string]bool{}
	for _, r := range p.AllowedResidency {
		if r != "any" && r != "trusted_nodes" && r != "origin_node" {
			return nil, fmt.Errorf("invalid provider residency %q", r)
		}
		if seen[r] {
			return nil, fmt.Errorf("duplicate provider residency %q", r)
		}
		seen[r] = true
	}
	return b, nil
}

func providerDataPolicy(raw json.RawMessage) (ProviderDataPolicy, error) {
	canonical, err := normalizeProviderConnectionJSON(raw)
	if err != nil {
		return ProviderDataPolicy{}, err
	}
	var wrapper struct {
		DataPolicy ProviderDataPolicy `json:"data_policy"`
	}
	if err := json.Unmarshal(canonical, &wrapper); err != nil {
		return ProviderDataPolicy{}, err
	}
	return wrapper.DataPolicy, nil
}

func confidentialityRank(v string) int {
	switch strings.ToLower(v) {
	case "public":
		return 0
	case "internal":
		return 1
	case "confidential":
		return 2
	case "secret":
		return 3
	default:
		return -1
	}
}

func (p ProviderDataPolicy) Allows(confidentiality, residency string) bool {
	if confidentialityRank(confidentiality) < 0 || confidentialityRank(p.MaxConfidentiality) < confidentialityRank(confidentiality) {
		return false
	}
	for _, allowed := range p.AllowedResidency {
		if allowed == strings.ToLower(residency) {
			return true
		}
	}
	return false
}
