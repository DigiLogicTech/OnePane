package sandboxrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type Adapter struct {
	dataDir string
	engine  Engine
	secrets SecretResolver
}

func NewAdapter(dataDir string, engine Engine, resolvers ...SecretResolver) *Adapter {
	if engine == nil {
		engine = NewCLIEngine()
	}
	a := &Adapter{dataDir: dataDir, engine: engine}
	if len(resolvers) > 0 {
		a.secrets = resolvers[0]
	}
	return a
}
func (a *Adapter) SetSecretResolver(r SecretResolver) {
	if a != nil {
		a.secrets = r
	}
}

func (a *Adapter) ID() string      { return AdapterID }
func (a *Adapter) Version() string { return AdapterVersion }

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type baseInput struct {
	Action              string          `json:"action"`
	Path                string          `json:"path,omitempty"`
	RuntimeID           string          `json:"runtime_id"`
	ApplicationID       string          `json:"application_id,omitempty"`
	Image               string          `json:"image,omitempty"`
	RuntimeSpec         json.RawMessage `json:"runtime_spec,omitempty"`
	ResourceLimits      json.RawMessage `json:"resource_limits,omitempty"`
	EnvironmentBindings json.RawMessage `json:"environment_bindings,omitempty"`
	NetworkPolicy       json.RawMessage `json:"network_policy,omitempty"`
	Command             []string        `json:"command,omitempty"`
	Message             string          `json:"message,omitempty"`
	TimeoutSeconds      int             `json:"timeout_seconds,omitempty"`
	Endpoints           []PortSpec      `json:"endpoints,omitempty"`
}
type runtimeSpec struct {
	Command    []string `json:"command"`
	WorkingDir string   `json:"working_dir"`
}
type envBinding struct {
	SecretRef string  `json:"secret_ref,omitempty"`
	Literal   *string `json:"literal,omitempty"`
}
type networkPolicy struct {
	Mode    string `json:"mode"`
	Ingress string `json:"ingress"`
	Egress  []any  `json:"egress"`
}

func Register(reg *tool.Registry, adapter *Adapter) error {
	defs := []tool.Definition{
		{ID: ToolRuntimeEnsure, Version: "1", CapabilityID: CapabilityManage, Mode: authority.ActionMutate, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone},
		{ID: ToolRuntimeStop, Version: "1", CapabilityID: CapabilityManage, Mode: authority.ActionMutate, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppPull, Version: "1", CapabilityID: CapabilityManage, Mode: authority.ActionMutate, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppEnsure, Version: "1", CapabilityID: CapabilityManage, Mode: authority.ActionMutate, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppStop, Version: "1", CapabilityID: CapabilityManage, Mode: authority.ActionMutate, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone},
		{ID: ToolRuntimeInspect, Version: "1", CapabilityID: CapabilityObserve, Mode: authority.ActionObserve, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppInspect, Version: "1", CapabilityID: CapabilityObserve, Mode: authority.ActionObserve, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolImageInspect, Version: "1", CapabilityID: CapabilityObserve, Mode: authority.ActionObserve, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppExec, Version: "1", CapabilityID: CapabilityExecute, Mode: authority.ActionExecuteSandboxed, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppGitInspect, Version: "1", CapabilityID: CapabilityExecute, Mode: authority.ActionExecuteSandboxed, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppFileInspect, Version: "1", CapabilityID: CapabilityExecute, Mode: authority.ActionExecuteSandboxed, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
		{ID: ToolAppGitMutate, Version: "1", CapabilityID: CapabilityExecute, Mode: authority.ActionExecuteSandboxed, AdapterID: AdapterID, AdapterVersion: AdapterVersion, Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone},
	}
	for _, d := range defs {
		if err := reg.Register(d, adapter); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) Invoke(ctx context.Context, req tool.AdapterRequest) (tool.AdapterResult, error) {
	if a == nil || a.engine == nil || strings.TrimSpace(a.dataDir) == "" {
		return tool.AdapterResult{}, tool.KnownFailure(ErrEngineUnavailable)
	}
	var in baseInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("%w: %v", ErrInvalidInput, err))
	}
	if !safeID.MatchString(in.RuntimeID) {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("%w: runtime_id", ErrInvalidInput))
	}
	profile, err := a.engine.Probe(ctx)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	if !profile.Rootless {
		return tool.AdapterResult{}, tool.KnownFailure(ErrRootlessRequired)
	}
	workspace, workspaceExists, err := managedWorkspacePath(a.dataDir, in.RuntimeID, false)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	switch req.ToolID {
	case ToolRuntimeInspect:
		states, err := a.engine.ListRuntime(ctx, in.RuntimeID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		network, err := a.engine.InspectNetwork(ctx, in.RuntimeID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "workspace_path": workspace, "workspace_exists": workspaceExists, "containers": states, "network": network, "engine": profile}, "project runtime observed")
	case ToolAppInspect:
		if !safeID.MatchString(in.ApplicationID) {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		state, err := a.engine.InspectContainer(ctx, in.RuntimeID, in.ApplicationID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		state.IsolationVerified = state.IsolationVerified && workspaceExists &&
			state.RuntimeID == in.RuntimeID && state.ApplicationID == in.ApplicationID &&
			state.SpecHash != "" && exactWorkspaceMount(state, workspace)
		network, err := a.engine.InspectNetwork(ctx, in.RuntimeID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "application_id": in.ApplicationID, "container": state, "network": network, "engine": profile}, "sandboxed application observed")
	case ToolImageInspect:
		if !validImage(in.Image) {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		state, err := a.engine.InspectImage(ctx, in.Image)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "image": state, "engine": profile}, "application image observed")
	case ToolAppGitMutate:
		if !safeID.MatchString(in.ApplicationID)||len(in.Command)!=0||in.Image!=""||
			in.TimeoutSeconds<0||in.TimeoutSeconds>120{
			return tool.AdapterResult{},tool.KnownFailure(ErrInvalidInput)
		}
		command,err:=gitMutateCommand(in.Action,in.Message)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if !workspaceExists{
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: Workspace not provisioned",ErrInvalidInput))
		}
		state,err:=a.engine.InspectContainer(ctx,in.RuntimeID,in.ApplicationID)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if state.Status!="running"||!state.IsolationVerified||
			state.RuntimeID!=in.RuntimeID||state.ApplicationID!=in.ApplicationID||
			state.SpecHash==""||!exactWorkspaceMount(state,workspace){
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: Git mutation requires verified Workspace sandbox",ErrInvalidInput))
		}
		timeout:=in.TimeoutSeconds
		if timeout==0{timeout=30}
		execCtx,cancel:=context.WithTimeout(ctx,time.Duration(timeout)*time.Second)
		defer cancel()
		observed,err:=a.engine.ExecContainer(execCtx,in.RuntimeID,in.ApplicationID,command)
		if execCtx.Err()!=nil{return tool.AdapterResult{},execCtx.Err()}
		if err!=nil{return tool.AdapterResult{},err}
		success:=observed.ExitCode==0
		summary:="Workspace Git "+in.Action+" completed; independent Task verification remains required"
		if !success{summary=fmt.Sprintf("Workspace Git %s exited with code %d",in.Action,observed.ExitCode)}
		return result(map[string]any{
			"runtime_id":in.RuntimeID,"application_id":in.ApplicationID,
			"action":in.Action,"succeeded":success,"result":observed,
			"container":state,"engine":profile,
		},summary)
	case ToolAppFileInspect:
		if !safeID.MatchString(in.ApplicationID)||len(in.Command)!=0||in.Image!=""||
			in.TimeoutSeconds<0||in.TimeoutSeconds>60{
			return tool.AdapterResult{},tool.KnownFailure(ErrInvalidInput)
		}
		command,err:=fileInspectCommand(in.Action,in.Path)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if !workspaceExists{
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: Workspace has not been provisioned",ErrInvalidInput))
		}
		state,err:=a.engine.InspectContainer(ctx,in.RuntimeID,in.ApplicationID)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if state.Status!="running"||!state.IsolationVerified||
			state.RuntimeID!=in.RuntimeID||state.ApplicationID!=in.ApplicationID||
			state.SpecHash==""||!exactWorkspaceMount(state,workspace){
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: file preview requires verified Workspace sandbox",ErrInvalidInput))
		}
		if in.Action=="preview_text"{
			if err:=verifyWorkspacePreviewPath(workspace,in.Path);err!=nil{
				return tool.AdapterResult{},tool.KnownFailure(err)
			}
		}
		timeout:=in.TimeoutSeconds
		if timeout==0{timeout=15}
		execCtx,cancel:=context.WithTimeout(ctx,time.Duration(timeout)*time.Second)
		defer cancel()
		observed,err:=a.engine.ExecContainer(execCtx,in.RuntimeID,in.ApplicationID,command)
		if execCtx.Err()!=nil{return tool.AdapterResult{},execCtx.Err()}
		if err!=nil{return tool.AdapterResult{},err}
		success:=observed.ExitCode==0
		summary:="Workspace file "+in.Action+" observed"
		if !success{summary=fmt.Sprintf("Workspace file %s exited with code %d",in.Action,observed.ExitCode)}
		return result(map[string]any{
			"runtime_id":in.RuntimeID,"application_id":in.ApplicationID,
			"action":in.Action,"path":in.Path,"preview_only":true,
			"content_limit_bytes":65536,"succeeded":success,
			"result":observed,"container":state,"engine":profile,
		},summary)
	case ToolAppGitInspect:
		if !safeID.MatchString(in.ApplicationID)||len(in.Command)!=0||in.Image!=""||in.TimeoutSeconds<0||in.TimeoutSeconds>120 {
			return tool.AdapterResult{},tool.KnownFailure(ErrInvalidInput)
		}
		command,err:=gitInspectCommand(in.Action)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if !workspaceExists {
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: Workspace has not been provisioned",ErrInvalidInput))
		}
		state,err:=a.engine.InspectContainer(ctx,in.RuntimeID,in.ApplicationID)
		if err!=nil{return tool.AdapterResult{},tool.KnownFailure(err)}
		if state.Status!="running"||!state.IsolationVerified||
			state.RuntimeID!=in.RuntimeID||state.ApplicationID!=in.ApplicationID||
			state.SpecHash==""||!exactWorkspaceMount(state,workspace){
			return tool.AdapterResult{},tool.KnownFailure(fmt.Errorf("%w: Git inspection requires verified Workspace sandbox",ErrInvalidInput))
		}
		timeout:=in.TimeoutSeconds
		if timeout==0{timeout=30}
		execCtx,cancel:=context.WithTimeout(ctx,time.Duration(timeout)*time.Second)
		defer cancel()
		observed,err:=a.engine.ExecContainer(execCtx,in.RuntimeID,in.ApplicationID,command)
		if execCtx.Err()!=nil{return tool.AdapterResult{},execCtx.Err()}
		if err!=nil{return tool.AdapterResult{},err}
		success:=observed.ExitCode==0
		summary:="Workspace Git "+in.Action+" inspected"
		if !success{summary=fmt.Sprintf("Workspace Git %s exited with code %d",in.Action,observed.ExitCode)}
		return result(map[string]any{
			"runtime_id":in.RuntimeID,"application_id":in.ApplicationID,
			"action":in.Action,"succeeded":success,"result":observed,
			"container":state,"engine":profile,
		},summary)
	case ToolAppExec:
		// A Task may take longer on local CPU, but cannot hold a sandbox exec
		// indefinitely. The caller's shorter cancellation deadline still wins.
		if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 7200 {
			return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("%w: command timeout must be 1..7200 seconds", ErrInvalidInput))
		}
		if in.TimeoutSeconds == 0 { in.TimeoutSeconds = 900 }
		if !safeID.MatchString(in.ApplicationID) || len(in.Command) == 0 || len(in.Command) > 128 {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		for _, arg := range in.Command {
			if strings.ContainsRune(arg, '\x00') {
				return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
			}
		}
		state, err := a.engine.InspectContainer(ctx, in.RuntimeID, in.ApplicationID)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		if state.Status != "running" || !state.IsolationVerified || !workspaceExists ||
			state.RuntimeID != in.RuntimeID || state.ApplicationID != in.ApplicationID ||
			state.SpecHash == "" || !exactWorkspaceMount(state, workspace) {
			return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("%w: application is not a verified running sandbox", ErrInvalidInput))
		}
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
		defer cancel()
		execResult, err := a.engine.ExecContainer(execCtx, in.RuntimeID, in.ApplicationID, in.Command)
		// Return the cancellation cause (rather than an engine-specific
		// "signal: killed") so the Gateway records timed_out/cancelled.
		if execCtx.Err() != nil {
			return tool.AdapterResult{}, execCtx.Err()
		}
		if err != nil {
			return tool.AdapterResult{}, err
		}
		// The transport/tool invocation completed even if compilation/tests
		// failed. Preserve the nonzero exit code and diagnostic output so the
		// agent can diagnose and retry. Never label a failed build as successful.
		succeeded := execResult.ExitCode == 0
		summary := "sandboxed application command completed successfully"
		if !succeeded {
			summary = fmt.Sprintf("sandboxed application command exited with code %d", execResult.ExitCode)
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "application_id": in.ApplicationID,
			"command": in.Command, "timeout_seconds": in.TimeoutSeconds,
			"succeeded": succeeded, "result": execResult, "container": state, "engine": profile}, summary)
	case ToolRuntimeEnsure:
		workspace, workspaceExists, err = managedWorkspacePath(a.dataDir, in.RuntimeID, true)
		if err != nil || !workspaceExists {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		networkInternal, err := networkInternalFromPolicy(in.NetworkPolicy)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		network, err := a.engine.EnsureNetwork(ctx, in.RuntimeID, networkInternal)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		if network.Internal != networkInternal || network.RuntimeID != in.RuntimeID {
			return tool.AdapterResult{}, tool.KnownFailure(ErrNetworkBackendRequired)
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "workspace_path": workspace, "network": network, "engine": profile, "status": "defined"}, "rootless project runtime workspace and project network prepared")
	case ToolRuntimeStop:
		states, err := a.engine.StopRuntime(ctx, in.RuntimeID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "containers": states, "engine": profile}, "project runtime containers stopped")
	case ToolAppPull:
		if !safeID.MatchString(in.ApplicationID) || !validImage(in.Image) {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		state, err := a.engine.PullImage(ctx, in.Image)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "application_id": in.ApplicationID, "image": state, "engine": profile}, "application image pulled")
	case ToolAppEnsure:
		if !safeID.MatchString(in.ApplicationID) || !validImage(in.Image) {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		workspace, workspaceExists, err = managedWorkspacePath(a.dataDir, in.RuntimeID, true)
		if err != nil || !workspaceExists {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		spec, err := decodeRuntimeSpec(in.RuntimeSpec)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		limits, err := decodeLimits(in.ResourceLimits)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		env, identities, err := a.decodeEnv(ctx, req.WorkspaceID, in.EnvironmentBindings)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		networkInternal, err := networkInternalFromPolicy(in.NetworkPolicy)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		if err := validatePorts(in.Endpoints); err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		network, err := a.engine.EnsureNetwork(ctx, in.RuntimeID, networkInternal)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		if network.Internal != networkInternal || network.RuntimeID != in.RuntimeID {
			return tool.AdapterResult{}, tool.KnownFailure(ErrNetworkBackendRequired)
		}
		state, err := a.engine.EnsureContainer(ctx, ContainerSpec{RuntimeID: in.RuntimeID, NetworkInternal: networkInternal, ApplicationID: in.ApplicationID, Image: in.Image, WorkspacePath: workspace, Command: spec.Command, WorkingDir: spec.WorkingDir, Environment: env, EnvironmentIdentity: identities, Ports: in.Endpoints, Limits: limits})
		for k := range env {
			env[k] = ""
		}
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "application_id": in.ApplicationID, "container": state, "network": network, "engine": profile}, "sandboxed application reconciled")
	case ToolAppStop:
		if !safeID.MatchString(in.ApplicationID) {
			return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
		}
		state, err := a.engine.StopContainer(ctx, in.RuntimeID, in.ApplicationID)
		if err != nil {
			return tool.AdapterResult{}, err
		}
		return result(map[string]any{"runtime_id": in.RuntimeID, "application_id": in.ApplicationID, "container": state, "engine": profile}, "sandboxed application stopped")
	default:
		return tool.AdapterResult{}, tool.KnownFailure(ErrInvalidInput)
	}
}
func result(v any, summary string) (tool.AdapterResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	return tool.AdapterResult{Summary: summary, Result: b}, nil
}
func validImage(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && !strings.ContainsAny(v, " \t\r\n\x00") && !strings.Contains(v, "://") && !strings.HasPrefix(v, "/")
}
func decodeRuntimeSpec(raw json.RawMessage) (runtimeSpec, error) {
	var v runtimeSpec
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("%w: runtime spec", ErrInvalidInput)
	}
	if v.WorkingDir != "" && !strings.HasPrefix(v.WorkingDir, "/") {
		return v, fmt.Errorf("%w: working_dir", ErrInvalidInput)
	}
	for _, x := range v.Command {
		if strings.ContainsRune(x, '\x00') {
			return v, ErrInvalidInput
		}
	}
	return v, nil
}
func decodeLimits(raw json.RawMessage) (ResourceLimits, error) {
	v := ResourceLimits{CPUMillis: 2000, MemoryMB: 2048, PIDs: 256}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &v); err != nil {
			return v, ErrInvalidInput
		}
	}
	if v.CPUMillis < 100 || v.CPUMillis > 16000 || v.MemoryMB < 64 || v.MemoryMB > 65536 || v.PIDs < 16 || v.PIDs > 4096 {
		return v, ErrInvalidInput
	}
	return v, nil
}
func (a *Adapter) decodeEnv(ctx context.Context, workspaceID string, raw json.RawMessage) (map[string]string, map[string]string, error) {
	values := map[string]string{}
	identities := map[string]string{}
	if len(raw) == 0 {
		return values, identities, nil
	}
	var m map[string]envBinding
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, ErrInvalidInput
	}
	for k, b := range m {
		if !safeEnvName(k) {
			return nil, nil, ErrInvalidInput
		}
		if b.SecretRef != "" {
			if a.secrets == nil {
				return nil, nil, ErrSecretBrokerRequired
			}
			logical := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(b.SecretRef), "secret:"))
			if logical == "" || logical == b.SecretRef || strings.TrimSpace(workspaceID) == "" {
				return nil, nil, ErrInvalidInput
			}
			value, recordID, version, err := a.secrets.ResolveWorkspaceLogical(ctx, workspaceID, logical)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: resolve %s", ErrSecretBrokerRequired, logical)
			}
			if strings.ContainsAny(value, "\x00\r\n") {
				return nil, nil, fmt.Errorf("%w: multiline environment secrets require a file-secret binding", ErrInvalidInput)
			}
			values[k] = value
			identities[k] = fmt.Sprintf("vault:%s:v%d", recordID, version)
			continue
		}
		if b.Literal == nil || strings.ContainsAny(*b.Literal, "\x00\r\n") {
			return nil, nil, ErrInvalidInput
		}
		values[k] = *b.Literal
		identities[k] = "literal:" + shortHash(*b.Literal)
	}
	return values, identities, nil
}

func safeEnvName(v string) bool {
	if v == "" {
		return false
	}
	for i, r := range v {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func networkInternalFromPolicy(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return true, nil
	}
	var n networkPolicy
	if err := json.Unmarshal(raw, &n); err != nil {
		return true, ErrInvalidInput
	}
	if n.Ingress != "" && n.Ingress != "proxy_only" {
		return true, ErrNetworkBackendRequired
	}
	switch n.Mode {
	case "", "deny_by_default":
		if len(n.Egress) > 0 {
			return true, ErrNetworkBackendRequired
		}
		return true, nil
	case "external":
		// External mode deliberately grants the project runtime outbound access
		// through its dedicated rootless bridge. It remains non-privileged,
		// read-only-rootfs, capability-dropped and loopback-only for published
		// ingress. Fine-grained destination filtering belongs to the governed
		// egress proxy and is not represented as if it were enforced here.
		return false, nil
	default:
		return true, ErrNetworkBackendRequired
	}
}

func validatePorts(ports []PortSpec) error {
	seen := map[string]bool{}
	for _, p := range ports {
		proto := strings.ToLower(strings.TrimSpace(p.Protocol))
		if proto == "" {
			proto = "tcp"
		}
		if p.InternalPort < 1 || p.InternalPort > 65535 || (proto != "tcp" && proto != "udp") {
			return ErrInvalidInput
		}
		key := fmt.Sprintf("%d/%s", p.InternalPort, proto)
		if seen[key] {
			return ErrInvalidInput
		}
		seen[key] = true
	}
	return nil
}

func shortHash(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:8])
}
