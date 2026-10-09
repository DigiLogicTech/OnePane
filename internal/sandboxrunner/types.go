package sandboxrunner

import (
	"context"
	"errors"
)

const (
	AdapterID        = "builtin.sandbox_runner"
	AdapterVersion   = "1"
	CapabilityManage = "project.runtime.manage"

	ToolRuntimeEnsure  = "project.runtime.ensure"
	ToolRuntimeStop    = "project.runtime.stop"
	ToolAppPull        = "project.app.pull"
	ToolAppEnsure      = "project.app.ensure"
	ToolAppStop        = "project.app.stop"
	ToolRuntimeInspect = "project.runtime.inspect"
	ToolAppInspect     = "project.app.inspect"
	ToolImageInspect   = "project.image.inspect"
	ToolAppExec        = "project.app.exec"
	ToolAppGitInspect  = "project.app.git.inspect"
	ToolAppFileInspect = "project.app.files.inspect"
	ToolAppFileEdit    = "project.app.files.edit"
	ToolAppGitMutate   = "project.app.git.mutate"
	CapabilityObserve  = "project.runtime.observe"
	CapabilityExecute  = "project.app.execute"
)

type EngineProfile struct {
	Kind       string `json:"kind"`
	Executable string `json:"executable"`
	Rootless   bool   `json:"rootless"`
}

type ResourceLimits struct {
	CPUMillis int `json:"cpu_millis"`
	MemoryMB  int `json:"memory_mb"`
	PIDs      int `json:"pids"`
}

type PortSpec struct {
	InternalPort int    `json:"internal_port"`
	Protocol     string `json:"protocol,omitempty"`
}

type PortState struct {
	InternalPort int    `json:"internal_port"`
	Protocol     string `json:"protocol,omitempty"`
	HostIP       string `json:"host_ip,omitempty"`
	HostPort     int    `json:"host_port,omitempty"`
}

type NetworkState struct {
	Name      string `json:"name"`
	ID        string `json:"id,omitempty"`
	RuntimeID string `json:"runtime_id,omitempty"`
	Internal  bool   `json:"internal"`
}

type ContainerSpec struct {
	RuntimeID           string
	NetworkInternal     bool
	ApplicationID       string
	Image               string
	WorkspacePath       string
	Command             []string
	WorkingDir          string
	Environment         map[string]string
	EnvironmentIdentity map[string]string
	Ports               []PortSpec
	Limits              ResourceLimits
}

type MountState struct {
	Type        string `json:"type,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
}

type ContainerState struct {
	Name              string       `json:"name"`
	ID                string       `json:"id,omitempty"`
	Status            string       `json:"status"`
	SpecHash          string       `json:"spec_hash,omitempty"`
	RuntimeID         string       `json:"runtime_id,omitempty"`
	ApplicationID     string       `json:"application_id,omitempty"`
	Image             string       `json:"image,omitempty"`
	ReadOnlyRootFS    bool         `json:"read_only_rootfs"`
	NetworkMode       string       `json:"network_mode,omitempty"`
	Privileged        bool         `json:"privileged"`
	CapAdd            []string     `json:"cap_add,omitempty"`
	CapDrop           []string     `json:"cap_drop,omitempty"`
	SecurityOpt       []string     `json:"security_opt,omitempty"`
	Mounts            []MountState `json:"mounts,omitempty"`
	Ports             []PortState  `json:"ports,omitempty"`
	NetworkInternal   bool         `json:"network_internal"`
	IsolationVerified bool         `json:"isolation_verified"`
}

type ImageState struct {
	Reference string   `json:"reference"`
	Digests   []string `json:"digests,omitempty"`
}

// ExecResult represents the program's exit status, not the container engine's
// status. A nonzero exit code is a successfully observed command failure and
// remains available to the agent for a fix/test/retry loop.
type ExecResult struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}

type SecretResolver interface {
	ResolveWorkspaceLogical(context.Context, string, string) (value, recordID string, version int64, err error)
}

type Engine interface {
	Probe(context.Context) (EngineProfile, error)
	EnsureNetwork(context.Context, string, bool) (NetworkState, error)
	InspectNetwork(context.Context, string) (NetworkState, error)
	PullImage(context.Context, string) (ImageState, error)
	InspectImage(context.Context, string) (ImageState, error)
	EnsureContainer(context.Context, ContainerSpec) (ContainerState, error)
	StopContainer(context.Context, string, string) (ContainerState, error)
	InspectContainer(context.Context, string, string) (ContainerState, error)
	ListRuntime(context.Context, string) ([]ContainerState, error)
	ExecContainer(context.Context, string, string, []string) (ExecResult, error)
	StopRuntime(context.Context, string) ([]ContainerState, error)
}

var (
	ErrEngineUnavailable      = errors.New("no acceptable rootless container engine is available")
	ErrRootlessRequired       = errors.New("container engine must operate rootless")
	ErrInvalidInput           = errors.New("invalid sandbox runner input")
	ErrSecretBrokerRequired   = errors.New("secret bindings require Secret Broker")
	ErrNetworkBackendRequired = errors.New("networked project applications require the proxy-only sandbox network backend")
)
