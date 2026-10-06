package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Status string

const (
	StatusRegistered     Status = "registered"
	StatusConnected      Status = "connected"
	StatusDegraded       Status = "degraded"
	StatusReauthRequired Status = "reauth_required"
	StatusUnavailable    Status = "unavailable"
	StatusDisabled       Status = "disabled"
	StatusRevoked        Status = "revoked"
)

type TrustState string

const (
	TrustUntrusted      TrustState = "untrusted"
	TrustUserTrusted    TrustState = "user_trusted"
	TrustTrustedAdapter TrustState = "trusted_adapter"
	TrustRevoked        TrustState = "revoked"
)

type OperatingMode string

const (
	// ProposalOnly lets the external harness reason/orchestrate but it must
	// return proposals to the origin harness. It receives no ToolGateway lease.
	ProposalOnly OperatingMode = "proposal_only"
	// GatewayMediated is the future callback mode: tool requests made by the
	// external runtime are returned to the origin ToolGateway for authorization.
	GatewayMediated OperatingMode = "gateway_mediated"
	// Unmanaged acknowledges an opaque external harness that may have its own
	// tools/state. It is never eligible for autonomous scheduling by v0.1.
	Unmanaged OperatingMode = "unmanaged"
)

type Connection struct {
	ID               string
	WorkspaceID      *string
	NodeID           *string
	RuntimeKind      string
	DisplayName      string
	AdapterName      string
	AdapterVersion   string
	EndpointJSON     json.RawMessage
	AuthType         string
	SecretRef        *string
	Status           Status
	TrustState       TrustState
	OperatingMode    OperatingMode
	ProtocolJSON     json.RawMessage
	CapabilitiesJSON json.RawMessage
	DataPolicyJSON   json.RawMessage
	Revision         int64
	CreatedAt        int64
	UpdatedAt        int64
}

type DataPolicy struct {
	MaxConfidentiality string   `json:"max_confidentiality"`
	AllowedResidency   []string `json:"allowed_residency"`
	DestinationKind    string   `json:"destination_kind"`
	AllowRawSecrets    bool     `json:"allow_raw_secrets"`
}

type RegisterCommand struct {
	WorkspaceID      *string
	NodeID           *string
	RuntimeKind      string
	DisplayName      string
	AdapterName      string
	AdapterVersion   string
	EndpointJSON     json.RawMessage
	AuthType         string
	SecretRef        *string
	TrustState       TrustState
	OperatingMode    OperatingMode
	ProtocolJSON     json.RawMessage
	CapabilitiesJSON json.RawMessage
	DataPolicyJSON   json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type SetStatusCommand struct {
	ConnectionID     string
	ExpectedRevision int64
	Status           Status
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	Reason           string
}

type Eligibility struct {
	Schedulable  bool
	ToolCallback bool
	OutputTrust  string
	Reason       string
}

var (
	ErrInvalidCommand     = errors.New("invalid agent runtime command")
	ErrRevisionConflict   = errors.New("revision conflict")
	ErrInvalidTransition  = errors.New("invalid agent runtime state transition")
	ErrWorkspaceInactive  = errors.New("workspace is not active")
	ErrAdapterUnavailable = errors.New("agent runtime adapter is unavailable")
	ErrUnsafeConfig       = errors.New("configuration contains a raw secret-like field")
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusRegistered, StatusConnected, StatusDegraded, StatusReauthRequired,
		StatusUnavailable, StatusDisabled, StatusRevoked:
		return true
	default:
		return false
	}
}

func ValidTrustState(s TrustState) bool {
	switch s {
	case TrustUntrusted, TrustUserTrusted, TrustTrustedAdapter, TrustRevoked:
		return true
	default:
		return false
	}
}

func ValidOperatingMode(m OperatingMode) bool {
	switch m {
	case ProposalOnly, GatewayMediated, Unmanaged:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if from == to || !ValidStatus(from) || !ValidStatus(to) || from == StatusRevoked {
		return false
	}
	if to == StatusRevoked || to == StatusDisabled {
		return true
	}
	switch from {
	case StatusRegistered:
		return to == StatusConnected || to == StatusUnavailable || to == StatusReauthRequired
	case StatusConnected:
		return to == StatusDegraded || to == StatusUnavailable || to == StatusReauthRequired
	case StatusDegraded:
		return to == StatusConnected || to == StatusUnavailable || to == StatusReauthRequired
	case StatusReauthRequired:
		return to == StatusConnected || to == StatusUnavailable
	case StatusUnavailable:
		return to == StatusConnected || to == StatusDegraded || to == StatusReauthRequired
	case StatusDisabled:
		return to == StatusRegistered
	default:
		return false
	}
}

func (c Connection) Eligibility() Eligibility {
	if c.Status != StatusConnected && c.Status != StatusDegraded {
		return Eligibility{OutputTrust: "unverified_derived", Reason: "runtime is not connected"}
	}
	if c.TrustState == TrustRevoked || c.Status == StatusRevoked {
		return Eligibility{OutputTrust: "unverified_derived", Reason: "runtime is revoked"}
	}
	if c.TrustState == TrustUntrusted {
		return Eligibility{OutputTrust: "unverified_derived", Reason: "runtime is not trusted for autonomous dispatch"}
	}
	switch c.OperatingMode {
	case ProposalOnly:
		return Eligibility{Schedulable: true, ToolCallback: false, OutputTrust: "unverified_derived"}
	case GatewayMediated:
		return Eligibility{Schedulable: true, ToolCallback: true, OutputTrust: "unverified_derived"}
	case Unmanaged:
		return Eligibility{Schedulable: false, ToolCallback: false, OutputTrust: "untrusted_content", Reason: "unmanaged runtimes are excluded from autonomous dispatch"}
	default:
		return Eligibility{OutputTrust: "unverified_derived", Reason: "invalid operating mode"}
	}
}

func canonicalJSON(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(fallback)
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

func normalizeDataPolicy(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"untrusted","allow_raw_secrets":false}`)
	}
	canonical, err := canonicalJSON(raw, "{}")
	if err != nil {
		return nil, err
	}
	var p DataPolicy
	if err := json.Unmarshal(canonical, &p); err != nil {
		return nil, err
	}
	switch p.MaxConfidentiality {
	case "public", "internal", "confidential", "secret":
	default:
		return nil, fmt.Errorf("max_confidentiality must be public/internal/confidential/secret")
	}
	if p.DestinationKind == "" {
		p.DestinationKind = "untrusted"
	}
	if p.DestinationKind != "origin_node" && p.DestinationKind != "trusted_node" && p.DestinationKind != "cloud" && p.DestinationKind != "untrusted" {
		return nil, fmt.Errorf("invalid destination_kind %q", p.DestinationKind)
	}
	if p.AllowRawSecrets {
		return nil, fmt.Errorf("raw secrets cannot be enabled for external runtimes")
	}
	if len(p.AllowedResidency) == 0 {
		return nil, fmt.Errorf("allowed_residency is required")
	}
	seen := map[string]bool{}
	for _, r := range p.AllowedResidency {
		if r != "any" && r != "trusted_nodes" && r != "origin_node" {
			return nil, fmt.Errorf("invalid residency %q", r)
		}
		if seen[r] {
			return nil, fmt.Errorf("duplicate residency %q", r)
		}
		seen[r] = true
	}
	return json.Marshal(p)
}
