package projectworkspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type IsolationMode string
type RuntimeDesiredState string
type RuntimeStatus string
type AppSourceKind string
type AppDesiredState string
type AppStatus string
type EndpointExposure string
type ProposalKind string
type ProposalStatus string
type ActionKind string

type Project struct {
	ID                 string          `json:"id"`
	WorkspaceID        string          `json:"workspace_id"`
	Name               string          `json:"name"`
	Description        *string         `json:"description,omitempty"`
	Status             string          `json:"status"`
	ProjectPolicyJSON  json.RawMessage `json:"project_policy"`
	IndexingConfigJSON json.RawMessage `json:"indexing_config"`
	Revision           int64           `json:"revision"`
	CreatedBy          string          `json:"created_by"`
	CreatedAt          int64           `json:"created_at"`
	UpdatedAt          int64           `json:"updated_at"`
}

type ProjectRuntime struct {
	ID                      string              `json:"id"`
	ProjectID               string              `json:"project_id"`
	ProjectWorkspaceID      *string             `json:"project_workspace_id,omitempty"`
	NodeID                  *string             `json:"node_id,omitempty"`
	IsolationMode           IsolationMode       `json:"isolation_mode"`
	Backend                 string              `json:"backend"`
	DesiredState            RuntimeDesiredState `json:"desired_state"`
	Status                  RuntimeStatus       `json:"status"`
	RuntimeSpecJSON         json.RawMessage     `json:"runtime_spec"`
	ResourceLimitsJSON      json.RawMessage     `json:"resource_limits"`
	NetworkPolicyJSON       json.RawMessage     `json:"network_policy"`
	FilesystemPolicyJSON    json.RawMessage     `json:"filesystem_policy"`
	EnvironmentBindingsJSON json.RawMessage     `json:"environment_bindings"`
	Revision                int64               `json:"revision"`
	CreatedBy               string              `json:"created_by"`
	CreatedAt               int64               `json:"created_at"`
	UpdatedAt               int64               `json:"updated_at"`
}

type Application struct {
	ID                      string          `json:"id"`
	ProjectRuntimeID        string          `json:"project_runtime_id"`
	Name                    string          `json:"name"`
	SourceKind              AppSourceKind   `json:"source_kind"`
	SourceRef               string          `json:"source_ref"`
	VersionRef              *string         `json:"version_ref,omitempty"`
	InstallSpecJSON         json.RawMessage `json:"install_spec"`
	RuntimeSpecJSON         json.RawMessage `json:"runtime_spec"`
	EnvironmentBindingsJSON json.RawMessage `json:"environment_bindings"`
	DesiredState            AppDesiredState `json:"desired_state"`
	Status                  AppStatus       `json:"status"`
	Trust                   string          `json:"trust"`
	Revision                int64           `json:"revision"`
	CreatedBy               string          `json:"created_by"`
	CreatedAt               int64           `json:"created_at"`
	UpdatedAt               int64           `json:"updated_at"`
}

type Endpoint struct {
	ID               string           `json:"id"`
	ProjectRuntimeID string           `json:"project_runtime_id"`
	ApplicationID    *string          `json:"application_id,omitempty"`
	Name             string           `json:"name"`
	Protocol         string           `json:"protocol"`
	InternalPort     int              `json:"internal_port"`
	Exposure         EndpointExposure `json:"exposure"`
	PathPrefix       *string          `json:"path_prefix,omitempty"`
	DesiredState     string           `json:"desired_state"`
	Status           string           `json:"status"`
	ExternalURL      *string          `json:"external_url,omitempty"`
	Revision         int64            `json:"revision"`
	CreatedBy        string           `json:"created_by"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
}

// EndpointRoute is trusted derived state. It is never supplied by an API
// caller: reconciliation may publish it only after a V2+ verification of the
// corresponding project_app observation proves a loopback port mapping.
type EndpointRoute struct {
	EndpointID          string `json:"endpoint_id"`
	ProjectRuntimeID    string `json:"project_runtime_id"`
	ApplicationID       string `json:"application_id"`
	HostIP              string `json:"host_ip"`
	HostPort            int    `json:"host_port"`
	TransportProtocol   string `json:"transport_protocol"`
	ObservationID       string `json:"observation_id"`
	VerificationID      string `json:"verification_id"`
	ApplicationRevision int64  `json:"application_revision"`
	EndpointRevision    int64  `json:"endpoint_revision"`
	ContainerSpecHash   string `json:"container_spec_hash"`
	Status              string `json:"status"`
	UpdatedAt           int64  `json:"updated_at"`
}

type IngressRoute struct {
	WorkspaceID string        `json:"workspace_id"`
	ProjectID   string        `json:"project_id"`
	Endpoint    Endpoint      `json:"endpoint"`
	Route       EndpointRoute `json:"route"`
}

type ChangeProposal struct {
	ID               string          `json:"id"`
	ProjectID        string          `json:"project_id"`
	ProjectRuntimeID *string         `json:"project_runtime_id,omitempty"`
	TaskID           *string         `json:"task_id,omitempty"`
	ProposalKind     ProposalKind    `json:"proposal_kind"`
	Status           ProposalStatus  `json:"status"`
	Summary          string          `json:"summary"`
	BaseRevision     *int64          `json:"base_revision,omitempty"`
	PatchArtifactID  *string         `json:"patch_artifact_id,omitempty"`
	MetadataJSON     json.RawMessage `json:"metadata"`
	Revision         int64           `json:"revision"`
	ProposedBy       string          `json:"proposed_by"`
	ReviewedBy       *string         `json:"reviewed_by,omitempty"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
	ReviewedAt       *int64          `json:"reviewed_at,omitempty"`
}

type RoutineBinding struct {
	ID               string          `json:"id"`
	ProjectID        string          `json:"project_id"`
	ProjectRuntimeID string          `json:"project_runtime_id"`
	ApplicationID    *string         `json:"application_id,omitempty"`
	RoutineID        string          `json:"routine_id"`
	ActionKind       ActionKind      `json:"action_kind"`
	ActionRef        string          `json:"action_ref"`
	ActionSpecJSON   json.RawMessage `json:"action_spec"`
	Status           string          `json:"status"`
	Revision         int64           `json:"revision"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
}

type CreateProjectCommand struct {
	WorkspaceID, Name, Description, CreatedBy string
	ProjectPolicyJSON, IndexingConfigJSON     json.RawMessage
	RequestID, TraceID                        *string
}

type UpdateProjectPolicyCommand struct {
	ProjectID          string
	ExpectedRevision   int64
	ProjectPolicyJSON  json.RawMessage
	ActorPrincipalID   string
	RequestID, TraceID *string
}

type ArchiveProjectCommand struct {
	ProjectID          string
	ExpectedRevision   int64
	ActorPrincipalID   string
	RequestID, TraceID *string
}

type CreateRuntimeCommand struct {
	ProjectID                                                    string
	ProjectWorkspaceID                                           *string
	NodeID                                                       *string
	IsolationMode                                                IsolationMode
	DesiredState                                                 RuntimeDesiredState
	RuntimeSpecJSON, ResourceLimitsJSON, EnvironmentBindingsJSON json.RawMessage
	CreatedBy                                                    string
	RequestID, TraceID                                           *string
}

type SetRuntimeDesiredStateCommand struct {
	RuntimeID          string
	ExpectedRevision   int64
	DesiredState       RuntimeDesiredState
	ActorPrincipalID   string
	RequestID, TraceID *string
}

type UpdateRuntimePolicyCommand struct {
	RuntimeID            string
	ExpectedRevision     int64
	NetworkPolicyJSON    json.RawMessage
	FilesystemPolicyJSON json.RawMessage
	ActorPrincipalID     string
	RequestID, TraceID   *string
}

type ApplyRuntimeObservationCommand struct {
	RuntimeID          string
	ExpectedRevision   int64
	Status             RuntimeStatus
	ObservationID      string
	VerificationID     string
	ActorPrincipalID   string
	RequestID, TraceID *string
}

type ApplyApplicationObservationCommand struct {
	ApplicationID      string
	ExpectedRevision   int64
	Status             AppStatus
	ObservationID      string
	VerificationID     string
	ActorPrincipalID   string
	RequestID, TraceID *string
}

type ApplyEndpointRouteCommand struct {
	EndpointID          string
	ApplicationID       string
	ApplicationRevision int64
	HostIP              string
	HostPort            int
	TransportProtocol   string
	ContainerSpecHash   string
	ObservationID       string
	VerificationID      string
	ActorPrincipalID    string
	RequestID, TraceID  *string
}

type DeclareApplicationCommand struct {
	RuntimeID, Name                                           string
	SourceKind                                                AppSourceKind
	SourceRef                                                 string
	VersionRef                                                *string
	InstallSpecJSON, RuntimeSpecJSON, EnvironmentBindingsJSON json.RawMessage
	DesiredState                                              AppDesiredState
	CreatedBy                                                 string
	RequestID, TraceID                                        *string
}

type DeclareEndpointCommand struct {
	RuntimeID          string
	ApplicationID      *string
	Name, Protocol     string
	InternalPort       int
	Exposure           EndpointExposure
	PathPrefix         *string
	CreatedBy          string
	RequestID, TraceID *string
}

type ProposeChangeCommand struct {
	ProjectID          string
	RuntimeID, TaskID  *string
	Kind               ProposalKind
	Summary            string
	BaseRevision       *int64
	PatchArtifactID    *string
	MetadataJSON       json.RawMessage
	ProposedBy         string
	RequestID, TraceID *string
}

type ReviewChangeCommand struct {
	ProposalID         string
	ExpectedRevision   int64
	Decision           ProposalStatus
	ReviewedBy         string
	RequestID, TraceID *string
}

type BindRoutineCommand struct {
	ProjectID, RuntimeID string
	ApplicationID        *string
	RoutineID            string
	ActionKind           ActionKind
	ActionRef            string
	ActionSpecJSON       json.RawMessage
	CreatedBy            string
	RequestID, TraceID   *string
}

const (
	IsolationSandboxedContainer IsolationMode       = "sandboxed_container"
	IsolationMicroVM            IsolationMode       = "microvm"
	RuntimeDesiredStopped       RuntimeDesiredState = "stopped"
	RuntimeDesiredRunning       RuntimeDesiredState = "running"
	RuntimeDesiredSuspended     RuntimeDesiredState = "suspended"
	RuntimeDefined              RuntimeStatus       = "defined"
	RuntimeProvisioning         RuntimeStatus       = "provisioning"
	RuntimeStopped              RuntimeStatus       = "stopped"
	RuntimeStarting             RuntimeStatus       = "starting"
	RuntimeRunning              RuntimeStatus       = "running"
	RuntimeDegraded             RuntimeStatus       = "degraded"
	RuntimeFailed               RuntimeStatus       = "failed"
	RuntimeSuspended            RuntimeStatus       = "suspended"
	RuntimeDeleting             RuntimeStatus       = "deleting"
	AppOCIImage                 AppSourceKind       = "oci_image"
	AppGit                      AppSourceKind       = "git"
	AppPackage                  AppSourceKind       = "package"
	AppArtifact                 AppSourceKind       = "artifact"
	AppCompose                  AppSourceKind       = "compose"
	AppDesiredInstalled         AppDesiredState     = "installed"
	AppDesiredRunning           AppDesiredState     = "running"
	AppDesiredStopped           AppDesiredState     = "stopped"
	AppDesiredRemoved           AppDesiredState     = "removed"
	AppDeclared                 AppStatus           = "declared"
	AppInstalling               AppStatus           = "installing"
	AppInstalled                AppStatus           = "installed"
	AppStarting                 AppStatus           = "starting"
	AppRunning                  AppStatus           = "running"
	AppStopped                  AppStatus           = "stopped"
	AppDegraded                 AppStatus           = "degraded"
	AppFailed                   AppStatus           = "failed"
	AppRemoving                 AppStatus           = "removing"
	AppRemoved                  AppStatus           = "removed"
	ExposurePreview             EndpointExposure    = "preview"
	ExposureWorkspace           EndpointExposure    = "workspace"
	ExposurePrivate             EndpointExposure    = "private"
	ExposurePublic              EndpointExposure    = "public"
	ProposalCode                ProposalKind        = "code"
	ProposalContent             ProposalKind        = "content"
	ProposalRuntimeConfig       ProposalKind        = "runtime_config"
	ProposalApplicationInstall  ProposalKind        = "application_install"
	ProposalApplicationUpdate   ProposalKind        = "application_update"
	ProposalApplicationRemove   ProposalKind        = "application_remove"
	ProposalEndpoint            ProposalKind        = "endpoint"
	ProposalRoutineBinding      ProposalKind        = "routine_binding"
	ProposalDraft               ProposalStatus      = "draft"
	ProposalProposed            ProposalStatus      = "proposed"
	ProposalReviewing           ProposalStatus      = "reviewing"
	ProposalApproved            ProposalStatus      = "approved"
	ProposalRejected            ProposalStatus      = "rejected"
	ProposalApplied             ProposalStatus      = "applied"
	ProposalSuperseded          ProposalStatus      = "superseded"
	ProposalCancelled           ProposalStatus      = "cancelled"
	ActionAppCommand            ActionKind          = "app_command"
	ActionHTTPRequest           ActionKind          = "http_request"
	ActionToolCapability        ActionKind          = "tool_capability"
)

var (
	ErrInvalidCommand      = errors.New("invalid project workspace command")
	ErrRevisionConflict    = errors.New("revision conflict")
	ErrWorkspaceInactive   = errors.New("workspace is not active")
	ErrProjectInactive     = errors.New("project is not active")
	ErrPrincipalIneligible = errors.New("principal is not eligible in workspace")
	ErrUnsafeSandboxSpec   = errors.New("sandbox specification attempts to weaken platform isolation")
	ErrCrossWorkspace      = errors.New("cross-workspace project binding denied")
	ErrInvalidTransition   = errors.New("invalid project workspace transition")
)

func validIsolation(v IsolationMode) bool {
	return v == IsolationSandboxedContainer || v == IsolationMicroVM
}
func validRuntimeDesired(v RuntimeDesiredState) bool {
	return v == RuntimeDesiredStopped || v == RuntimeDesiredRunning || v == RuntimeDesiredSuspended
}
func validRuntimeStatus(v RuntimeStatus) bool {
	switch v {
	case RuntimeDefined, RuntimeProvisioning, RuntimeStopped, RuntimeStarting, RuntimeRunning, RuntimeDegraded, RuntimeFailed, RuntimeSuspended, RuntimeDeleting:
		return true
	}
	return false
}
func validAppStatus(v AppStatus) bool {
	switch v {
	case AppDeclared, AppInstalling, AppInstalled, AppStarting, AppRunning, AppStopped, AppDegraded, AppFailed, AppRemoving, AppRemoved:
		return true
	}
	return false
}
func validAppSource(v AppSourceKind) bool {
	switch v {
	case AppOCIImage, AppGit, AppPackage, AppArtifact, AppCompose:
		return true
	}
	return false
}
func validAppDesired(v AppDesiredState) bool {
	switch v {
	case AppDesiredInstalled, AppDesiredRunning, AppDesiredStopped, AppDesiredRemoved:
		return true
	}
	return false
}
func validExposure(v EndpointExposure) bool {
	switch v {
	case ExposurePreview, ExposureWorkspace, ExposurePrivate, ExposurePublic:
		return true
	}
	return false
}
func validProposalKind(v ProposalKind) bool {
	switch v {
	case ProposalCode, ProposalContent, ProposalRuntimeConfig, ProposalApplicationInstall, ProposalApplicationUpdate, ProposalApplicationRemove, ProposalEndpoint, ProposalRoutineBinding:
		return true
	}
	return false
}
func validActionKind(v ActionKind) bool {
	return v == ActionAppCommand || v == ActionHTTPRequest || v == ActionToolCapability
}

func canonicalJSON(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(fallback)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validateSourceRef(kind AppSourceKind, ref string) (string, error) {
	v := strings.TrimSpace(ref)
	if v == "" || strings.ContainsAny(v, "\r\n\x00") {
		return "", fmt.Errorf("%w: invalid application source_ref", ErrInvalidCommand)
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "file:") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\\`) || (len(v) >= 3 && v[1] == ':' && (v[2] == '\\' || v[2] == '/')) {
		return "", fmt.Errorf("%w: host-local application sources are forbidden", ErrUnsafeSandboxSpec)
	}
	switch kind {
	case AppGit:
		if !(strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") || strings.HasPrefix(lower, "git://") || strings.HasPrefix(v, "git@")) {
			return "", fmt.Errorf("%w: git source must be a remote repository", ErrInvalidCommand)
		}
	case AppArtifact, AppCompose:
		if !strings.HasPrefix(v, "artifact:") || strings.TrimSpace(strings.TrimPrefix(v, "artifact:")) == "" {
			return "", fmt.Errorf("%w: %s source must use artifact:<id>", ErrInvalidCommand, kind)
		}
	case AppPackage:
		if strings.Contains(v, "..") || strings.ContainsAny(v, `\`) {
			return "", fmt.Errorf("%w: package source must not contain filesystem traversal", ErrUnsafeSandboxSpec)
		}
	case AppOCIImage:
		if strings.Contains(v, " ") || strings.Contains(lower, "://") {
			return "", fmt.Errorf("%w: invalid OCI image reference", ErrInvalidCommand)
		}
	default:
		return "", fmt.Errorf("%w: unsupported source kind", ErrInvalidCommand)
	}
	return v, nil
}

func artifactIDFromSource(ref string) string {
	return strings.TrimSpace(strings.TrimPrefix(ref, "artifact:"))
}

func validateSafeSpec(raw json.RawMessage) (json.RawMessage, error) {
	c, err := canonicalJSON(raw, "{}")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JSON: %v", ErrInvalidCommand, err)
	}
	var v any
	_ = json.Unmarshal(c, &v)
	forbidden := map[string]bool{"privileged": true, "hostnetwork": true, "host_network": true, "hostpid": true, "host_pid": true, "hostipc": true, "host_ipc": true, "docker_socket": true, "host_path": true, "bind_mount": true, "binds": true, "devices": true, "device_passthrough": true, "cap_add": true}
	var walk func(any) error
	walk = func(x any) error {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				lk := strings.ToLower(strings.TrimSpace(k))
				if forbidden[lk] {
					return fmt.Errorf("%w: forbidden field %q", ErrUnsafeSandboxSpec, k)
				}
				if err := walk(val); err != nil {
					return err
				}
			}
		case []any:
			for _, val := range t {
				if err := walk(val); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(v); err != nil {
		return nil, err
	}
	return c, nil
}

func validateEnvironmentBindings(raw json.RawMessage) (json.RawMessage, error) {
	c, err := canonicalJSON(raw, "{}")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid environment bindings: %v", ErrInvalidCommand, err)
	}
	var m map[string]any
	if err := json.Unmarshal(c, &m); err != nil {
		return nil, fmt.Errorf("%w: environment bindings must be an object", ErrInvalidCommand)
	}
	for name, v := range m {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%w: blank environment name", ErrInvalidCommand)
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: environment %s must use a binding object", ErrInvalidCommand, name)
		}
		for k := range obj {
			if k != "secret_ref" && k != "literal" {
				return nil, fmt.Errorf("%w: environment %s field %q not allowed", ErrInvalidCommand, name, k)
			}
		}
		_, hasSecret := obj["secret_ref"]
		lit, hasLiteral := obj["literal"]
		if hasSecret == hasLiteral {
			return nil, fmt.Errorf("%w: environment %s must set exactly one of secret_ref or literal", ErrInvalidCommand, name)
		}
		if hasSecret {
			s, ok := obj["secret_ref"].(string)
			if !ok || !strings.HasPrefix(strings.TrimSpace(s), "secret:") {
				return nil, fmt.Errorf("%w: environment %s secret_ref must use secret: reference", ErrInvalidCommand, name)
			}
		}
		if hasLiteral {
			value, ok := lit.(string)
			if !ok {
				return nil, fmt.Errorf("%w: environment %s literal must be a string", ErrInvalidCommand, name)
			}
			if looksSensitiveEnvironmentName(name) || looksSensitiveLiteral(value) {
				return nil, fmt.Errorf("%w: environment %s looks secret-bearing; use secret_ref", ErrInvalidCommand, name)
			}
		}
	}
	return c, nil
}

func looksSensitiveEnvironmentName(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	for _, token := range []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY"} {
		if strings.Contains(u, token) {
			return true
		}
	}
	return false
}

func looksSensitiveLiteral(value string) bool {
	v := strings.TrimSpace(value)
	prefixes := []string{"sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-", "Bearer ", "-----BEGIN PRIVATE KEY-----"}
	for _, p := range prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}
func defaultResourceLimits() json.RawMessage {
	return json.RawMessage(`{"cpu_millis":2000,"memory_mb":2048,"pids":256}`)
}
func defaultNetworkPolicy() json.RawMessage {
	return json.RawMessage(`{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}`)
}
func defaultFilesystemPolicy() json.RawMessage {
	return json.RawMessage(`{"root":"ephemeral","project_workspace":{"mount":"/workspace","mode":"read_write"},"host_mounts":[],"docker_socket":false,"device_passthrough":false,"no_new_privileges":true}`)
}
