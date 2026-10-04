package nodefederation

import "encoding/json"

const ProtocolVersion = "onepane-federation-v1"

type DiscoveryAnnouncement struct {
	Protocol            string `json:"protocol"`
	NodeID              string `json:"node_id"`
	Name                string `json:"name"`
	IdentityFingerprint string `json:"identity_fingerprint"`
	TLSFingerprint      string `json:"tls_fingerprint"`
	FederationPort      int    `json:"federation_port"`
	SentAt              int64  `json:"sent_at"`
}

type Pairing struct {
	ID                 string `json:"id"`
	PeerNodeID         string `json:"peer_node_id"`
	Direction          string `json:"direction"`
	Status             string `json:"status"`
	LocalConfirmed     bool   `json:"local_confirmed"`
	PeerConfirmed      bool   `json:"peer_confirmed"`
	PeerTLSFingerprint string `json:"peer_tls_fingerprint"`
	ExpiresAt          int64  `json:"expires_at"`
	PairedAt           *int64 `json:"paired_at,omitempty"`
	Revision           int64  `json:"revision"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type NodeView struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	Local               bool            `json:"local"`
	IdentityFingerprint string          `json:"identity_fingerprint"`
	TrustState          string          `json:"trust_state"`
	TrustZone           *string         `json:"trust_zone,omitempty"`
	EndpointJSON        json.RawMessage `json:"endpoint,omitempty"`
	CapabilitiesJSON    json.RawMessage `json:"capabilities"`
	LastSeenAt          *int64          `json:"last_seen_at,omitempty"`
	Revision            int64           `json:"revision"`
}

type ComputePool struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"` // cpu | gpu
	Name           string  `json:"name"`
	Backend        string  `json:"backend,omitempty"`
	CapacityBytes  int64   `json:"capacity_bytes,omitempty"`
	AvailableBytes int64   `json:"available_bytes,omitempty"`
	UtilizationPct float64 `json:"utilization_pct,omitempty"`
}

type NodeComputeState struct {
	Status                string        `json:"status"` // available | busy | draining | maintenance
	CPUUsagePct           float64       `json:"cpu_usage_pct,omitempty"`
	MemoryTotalBytes      int64         `json:"memory_total_bytes,omitempty"`
	MemoryAvailableBytes  int64         `json:"memory_available_bytes,omitempty"`
	StorageAvailableBytes int64         `json:"storage_available_bytes,omitempty"`
	Pools                 []ComputePool `json:"pools,omitempty"`
}

type DeploymentCompute struct {
	Mode      string   `json:"mode,omitempty"` // cpu | gpu | hybrid
	Backend   string   `json:"backend,omitempty"`
	DeviceIDs []string `json:"device_ids,omitempty"`
	RAMBytes  int64    `json:"ram_bytes,omitempty"`
	VRAMBytes int64    `json:"vram_bytes,omitempty"`
	Cached    bool     `json:"cached,omitempty"`
	Resident  bool     `json:"resident,omitempty"`
}

type ModelCapability struct {
	RemoteDeploymentID string            `json:"deployment_id"`
	ModelRef           string            `json:"model_ref"`
	ProviderName       *string           `json:"provider_name,omitempty"`
	Architecture       *string           `json:"architecture,omitempty"`
	RevisionRef        *string           `json:"revision_ref,omitempty"`
	WeightsHash        *string           `json:"weights_hash,omitempty"`
	Quantization       *string           `json:"quantization,omitempty"`
	ModalitiesJSON     json.RawMessage   `json:"modalities_json"`
	RuntimeName        *string           `json:"runtime_name,omitempty"`
	RuntimeVersion     *string           `json:"runtime_version,omitempty"`
	ContextMax         int64             `json:"context_max"`
	Qualification      string            `json:"qualification"`
	ProtocolLevel      string            `json:"protocol_level"`
	Capabilities       []string          `json:"capabilities"`
	Roles              []string          `json:"roles"`
	ToolUseAllowed     bool              `json:"tool_use_allowed"`
	Compute            DeploymentCompute `json:"compute,omitempty"`
}

type CapabilityManifest struct {
	Protocol   string            `json:"protocol"`
	NodeID     string            `json:"node_id"`
	Sequence   int64             `json:"sequence"`
	Generated  int64             `json:"generated_at"`
	Models     []ModelCapability `json:"models"`
	AgentCount int               `json:"agent_runtime_count"`
	Compute    NodeComputeState  `json:"compute"`
}

type PairRequest struct {
	Protocol            string `json:"protocol"`
	PairingID           string `json:"pairing_id"`
	NodeID              string `json:"node_id"`
	Name                string `json:"name"`
	IdentityFingerprint string `json:"identity_fingerprint"`
	TLSFingerprint      string `json:"tls_fingerprint"`
	CertificatePEM      string `json:"certificate_pem"`
	Endpoint            string `json:"endpoint"`
	PairingToken        string `json:"pairing_token"`
	PairingCode         string `json:"pairing_code"`
	ExpiresAt           int64  `json:"expires_at"`
}

type PairConfirm struct {
	Protocol     string `json:"protocol"`
	PairingID    string `json:"pairing_id"`
	NodeID       string `json:"node_id"`
	PairingToken string `json:"pairing_token"`
}

type Heartbeat struct {
	Protocol string             `json:"protocol"`
	NodeID   string             `json:"node_id"`
	Manifest CapabilityManifest `json:"manifest"`
}

type RemoteInferenceRequest struct {
	Protocol           string          `json:"protocol"`
	RequestID          string          `json:"request_id"`
	DeploymentID       string          `json:"deployment_id"`
	RequestJSON        json.RawMessage `json:"request_json"`
	ContextManifest    json.RawMessage `json:"context_manifest,omitempty"`
	ClassificationJSON json.RawMessage `json:"classification_json,omitempty"`
}

type RemoteInferenceResponse struct {
	ResponseJSON json.RawMessage `json:"response_json,omitempty"`
	UsageJSON    json.RawMessage `json:"usage_json,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	OutcomeKnown bool            `json:"outcome_known"`
}

type ModelManagementGrant struct {
	PeerNodeID          string  `json:"peer_node_id"`
	Enabled             bool    `json:"enabled"`
	AllowCatalogInstall bool    `json:"allow_catalog_install"`
	UpdatedBy           *string `json:"updated_by,omitempty"`
	Revision            int64   `json:"revision"`
	CreatedAt           int64   `json:"created_at"`
	UpdatedAt           int64   `json:"updated_at"`
}

type RemoteModelRecommendRequest struct {
	UseCase             string `json:"use_case"`
	ContextTokens       int64  `json:"context_tokens"`
	Limit               int    `json:"limit"`
	MinimumFit          string `json:"minimum_fit"`
	PreferGPU           bool   `json:"prefer_gpu"`
	PlacementPreference string `json:"placement_preference,omitempty"`
}

type RemoteModelRecommendResponse struct {
	HardwareProfile any `json:"hardware_profile"`
	Recommendations any `json:"recommendations"`
}

type RemoteModelInstallRequest struct {
	ModelRef            string `json:"model_ref"`
	Quantization        string `json:"quantization"`
	UseCase             string `json:"use_case"`
	ContextTokens       int64  `json:"context_tokens"`
	RoleName            string `json:"role_name"`
	PreferGPU           bool   `json:"prefer_gpu"`
	PlacementPreference string `json:"placement_preference,omitempty"`
}

type ComputePolicy struct {
	NodeID              string          `json:"node_id"`
	Enabled             bool            `json:"enabled"`
	IdleOnly            bool            `json:"idle_only"`
	AvailabilityJSON    json.RawMessage `json:"availability"`
	LimitsJSON          json.RawMessage `json:"limits"`
	AllowModelDownloads bool            `json:"allow_model_downloads"`
	RuntimeInstallation string          `json:"runtime_installation"`
	ProjectScope        string          `json:"project_scope"`
	AllowedProjectsJSON json.RawMessage `json:"allowed_projects"`
	UpdatedBy           *string         `json:"updated_by,omitempty"`
	Revision            int64           `json:"revision"`
	CreatedAt           int64           `json:"created_at"`
	UpdatedAt           int64           `json:"updated_at"`
}

type SetComputePolicyCommand struct {
	NodeID              string
	Enabled             bool
	IdleOnly            bool
	AvailabilityJSON    json.RawMessage
	LimitsJSON          json.RawMessage
	AllowModelDownloads bool
	RuntimeInstallation string
	ProjectScope        string
	AllowedProjectsJSON json.RawMessage
	Actor               string
}
