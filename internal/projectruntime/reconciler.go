package projectruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

var (
	ErrInvalidCommand               = errors.New("invalid project runtime reconciliation command")
	ErrIndependentObserverRequired  = errors.New("project runtime verification requires an independent observer principal")
	ErrUnsupportedBackend           = errors.New("project runtime backend is not supported by the active reconciler")
	ErrUnsupportedApplicationSource = errors.New("application source is declared but not executable by the current sandbox backend")
	ErrPostcondition                = errors.New("sandbox mutation postcondition was not independently satisfied")
	ErrRecoveryRequired             = errors.New("sandbox operation has an unknown outcome and requires reconciliation before retry")
)

type workspaceService interface {
	Runtime(context.Context, string) (projectworkspace.ProjectRuntime, error)
	Project(context.Context, string) (projectworkspace.Project, error)
	ListApplications(context.Context, string) ([]projectworkspace.Application, error)
	ListEndpoints(context.Context, string) ([]projectworkspace.Endpoint, error)
	Application(context.Context, string) (projectworkspace.Application, error)
	ApplyRuntimeObservation(context.Context, projectworkspace.ApplyRuntimeObservationCommand) (projectworkspace.ProjectRuntime, error)
	ApplyApplicationObservation(context.Context, projectworkspace.ApplyApplicationObservationCommand) (projectworkspace.Application, error)
	ApplyEndpointRoute(context.Context, projectworkspace.ApplyEndpointRouteCommand) (projectworkspace.EndpointRoute, error)
}

type operationCoordinator interface {
	Prepare(context.Context, operation.PrepareCommand) (operation.Operation, error)
	Execute(context.Context, operation.ExecuteCommand) (operation.Operation, error)
	BlockUnknown(context.Context, operation.RecoveryCommand) (operation.Operation, error)
	AcceptReconciledOutcome(context.Context, operation.RecoveryCommand) (operation.Operation, error)
	CommitVerified(context.Context, operation.CommitCommand) (operation.Operation, error)
}

type toolGateway interface {
	Invoke(context.Context, tool.InvokeCommand) (tool.Invocation, error)
}

type observationService interface {
	Record(context.Context, observation.RecordCommand) (observation.Observation, error)
	VerifyIntegrity(context.Context, string) error
}

type verificationService interface {
	Create(context.Context, verification.CreateCommand) (verification.Verification, error)
	Resolve(context.Context, verification.ResolveCommand) (verification.Verification, error)
}

type Reconciler struct {
	projects      workspaceService
	operations    operationCoordinator
	tools         toolGateway
	observations  observationService
	verifications verificationService
}

func New(projects workspaceService, operations operationCoordinator, tools toolGateway, observations observationService, verifications verificationService) *Reconciler {
	return &Reconciler{projects: projects, operations: operations, tools: tools, observations: observations, verifications: verifications}
}

type ReconcileCommand struct {
	RuntimeID           string
	TaskID              *string
	AttemptID           *string
	MutationPrincipalID string
	MutationLeaseID     string
	ObserverPrincipalID string
	ObserverLeaseID     string
	ActorPrincipalID    *string
	RequestID           *string
	TraceID             *string
}

type Result struct {
	Runtime        projectworkspace.ProjectRuntime  `json:"runtime"`
	Applications   []projectworkspace.Application   `json:"applications"`
	EndpointRoutes []projectworkspace.EndpointRoute `json:"endpoint_routes,omitempty"`
	Operations     []string                         `json:"operations"`
	Observations   []string                         `json:"observations"`
	Verifications  []string                         `json:"verifications"`
}

type mutationPlan struct {
	ToolID          string
	InspectToolID   string
	ResourceRef     string
	SubjectRef      string
	Input           json.RawMessage
	InspectInput    json.RawMessage
	DesiredState    json.RawMessage
	Precondition    json.RawMessage
	Reconciliation  json.RawMessage
	Compensation    json.RawMessage
	IdempotencyKey  string
	ObservationType string
	Postcondition   func(json.RawMessage) bool
}

type evidence struct {
	operation    operation.Operation
	observation  observation.Observation
	verification verification.Verification
}

func (r *Reconciler) Reconcile(ctx context.Context, cmd ReconcileCommand) (Result, error) {
	if r == nil || r.projects == nil || r.operations == nil || r.tools == nil || r.observations == nil || r.verifications == nil ||
		strings.TrimSpace(cmd.RuntimeID) == "" || strings.TrimSpace(cmd.MutationPrincipalID) == "" || strings.TrimSpace(cmd.MutationLeaseID) == "" ||
		strings.TrimSpace(cmd.ObserverPrincipalID) == "" || strings.TrimSpace(cmd.ObserverLeaseID) == "" {
		return Result{}, ErrInvalidCommand
	}
	if cmd.MutationPrincipalID == cmd.ObserverPrincipalID {
		return Result{}, ErrIndependentObserverRequired
	}
	runtime, err := r.projects.Runtime(ctx, cmd.RuntimeID)
	if err != nil {
		return Result{}, err
	}
	if runtime.Backend != "sandbox_runner" || runtime.IsolationMode != projectworkspace.IsolationSandboxedContainer {
		return Result{}, ErrUnsupportedBackend
	}
	project, err := r.projects.Project(ctx, runtime.ProjectID)
	if err != nil {
		return Result{}, err
	}
	result := Result{Runtime: runtime}

	var runtimePlan mutationPlan
	switch runtime.DesiredState {
	case projectworkspace.RuntimeDesiredRunning:
		runtimePlan = runtimeEnsurePlan(runtime)
	case projectworkspace.RuntimeDesiredStopped, projectworkspace.RuntimeDesiredSuspended:
		runtimePlan = runtimeStopPlan(runtime)
	default:
		return Result{}, ErrInvalidCommand
	}
	ev, err := r.applyAndVerify(ctx, project.WorkspaceID, cmd, runtimePlan)
	if err != nil {
		return result, err
	}
	result.Operations = append(result.Operations, ev.operation.ID)
	result.Observations = append(result.Observations, ev.observation.ID)
	result.Verifications = append(result.Verifications, ev.verification.ID)
	runtimeStatus := projectworkspace.RuntimeRunning
	if runtime.DesiredState == projectworkspace.RuntimeDesiredStopped {
		runtimeStatus = projectworkspace.RuntimeStopped
	} else if runtime.DesiredState == projectworkspace.RuntimeDesiredSuspended {
		runtimeStatus = projectworkspace.RuntimeSuspended
	}
	runtime, err = r.projects.Runtime(ctx, runtime.ID)
	if err != nil {
		return result, err
	}
	runtime, err = r.projects.ApplyRuntimeObservation(ctx, projectworkspace.ApplyRuntimeObservationCommand{RuntimeID: runtime.ID, ExpectedRevision: runtime.Revision, Status: runtimeStatus, ObservationID: ev.observation.ID, VerificationID: ev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return result, err
	}
	result.Runtime = runtime

	apps, err := r.projects.ListApplications(ctx, runtime.ID)
	if err != nil {
		return result, err
	}
	endpoints, err := r.projects.ListEndpoints(ctx, runtime.ID)
	if err != nil {
		return result, err
	}
	for _, app := range apps {
		if app.SourceKind != projectworkspace.AppOCIImage {
			return result, fmt.Errorf("%w: %s (%s)", ErrUnsupportedApplicationSource, app.ID, app.SourceKind)
		}
		if runtime.DesiredState != projectworkspace.RuntimeDesiredRunning {
			plan := appStopPlan(runtime, app)
			aev, e := r.applyAndVerify(ctx, project.WorkspaceID, cmd, plan)
			if e != nil {
				return result, e
			}
			result.Operations = append(result.Operations, aev.operation.ID)
			result.Observations = append(result.Observations, aev.observation.ID)
			result.Verifications = append(result.Verifications, aev.verification.ID)
			cur, e := r.projects.Application(ctx, app.ID)
			if e != nil {
				return result, e
			}
			cur, e = r.projects.ApplyApplicationObservation(ctx, projectworkspace.ApplyApplicationObservationCommand{ApplicationID: cur.ID, ExpectedRevision: cur.Revision, Status: projectworkspace.AppStopped, ObservationID: aev.observation.ID, VerificationID: aev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
			if e != nil {
				return result, e
			}
			result.Applications = append(result.Applications, cur)
			continue
		}
		switch app.DesiredState {
		case projectworkspace.AppDesiredRunning:
			pull := appPullPlan(runtime, app)
			pev, e := r.applyAndVerify(ctx, project.WorkspaceID, cmd, pull)
			if e != nil {
				return result, e
			}
			result.Operations = append(result.Operations, pev.operation.ID)
			result.Observations = append(result.Observations, pev.observation.ID)
			result.Verifications = append(result.Verifications, pev.verification.ID)
			ensure := appEnsurePlan(runtime, app, endpointSpecsForApp(endpoints, app.ID))
			aev, e := r.applyAndVerify(ctx, project.WorkspaceID, cmd, ensure)
			if e != nil {
				return result, e
			}
			result.Operations = append(result.Operations, aev.operation.ID)
			result.Observations = append(result.Observations, aev.observation.ID)
			result.Verifications = append(result.Verifications, aev.verification.ID)
			cur, e := r.projects.Application(ctx, app.ID)
			if e != nil {
				return result, e
			}
			cur, e = r.projects.ApplyApplicationObservation(ctx, projectworkspace.ApplyApplicationObservationCommand{ApplicationID: cur.ID, ExpectedRevision: cur.Revision, Status: projectworkspace.AppRunning, ObservationID: aev.observation.ID, VerificationID: aev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
			if e != nil {
				return result, e
			}
			routes, e := r.publishVerifiedEndpointRoutes(ctx, cmd, cur, endpoints, aev)
			if e != nil {
				return result, e
			}
			result.EndpointRoutes = append(result.EndpointRoutes, routes...)
			result.Applications = append(result.Applications, cur)
		case projectworkspace.AppDesiredInstalled:
			pev, e := r.applyAndVerify(ctx, project.WorkspaceID, cmd, appPullPlan(runtime, app))
			if e != nil {
				return result, e
			}
			result.Operations = append(result.Operations, pev.operation.ID)
			result.Observations = append(result.Observations, pev.observation.ID)
			result.Verifications = append(result.Verifications, pev.verification.ID)
			cur, e := r.projects.Application(ctx, app.ID)
			if e != nil {
				return result, e
			}
			cur, e = r.projects.ApplyApplicationObservation(ctx, projectworkspace.ApplyApplicationObservationCommand{ApplicationID: cur.ID, ExpectedRevision: cur.Revision, Status: projectworkspace.AppInstalled, ObservationID: pev.observation.ID, VerificationID: pev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
			if e != nil {
				return result, e
			}
			result.Applications = append(result.Applications, cur)
		case projectworkspace.AppDesiredStopped:
			aev, e := r.applyAndVerify(ctx, project.WorkspaceID, cmd, appStopPlan(runtime, app))
			if e != nil {
				return result, e
			}
			result.Operations = append(result.Operations, aev.operation.ID)
			result.Observations = append(result.Observations, aev.observation.ID)
			result.Verifications = append(result.Verifications, aev.verification.ID)
			cur, e := r.projects.Application(ctx, app.ID)
			if e != nil {
				return result, e
			}
			cur, e = r.projects.ApplyApplicationObservation(ctx, projectworkspace.ApplyApplicationObservationCommand{ApplicationID: cur.ID, ExpectedRevision: cur.Revision, Status: projectworkspace.AppStopped, ObservationID: aev.observation.ID, VerificationID: aev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
			if e != nil {
				return result, e
			}
			result.Applications = append(result.Applications, cur)
		case projectworkspace.AppDesiredRemoved:
			return result, fmt.Errorf("%w: removal backend not yet enabled for %s", ErrUnsupportedApplicationSource, app.ID)
		default:
			return result, ErrInvalidCommand
		}
	}
	return result, nil
}

func (r *Reconciler) publishVerifiedEndpointRoutes(ctx context.Context, cmd ReconcileCommand, app projectworkspace.Application, endpoints []projectworkspace.Endpoint, ev evidence) ([]projectworkspace.EndpointRoute, error) {
	var inspected struct {
		Container sandboxrunner.ContainerState `json:"container"`
	}
	if err := json.Unmarshal(ev.observation.Value, &inspected); err != nil {
		return nil, ErrPostcondition
	}
	if inspected.Container.ApplicationID != app.ID || inspected.Container.Status != "running" || !inspected.Container.IsolationVerified || strings.TrimSpace(inspected.Container.SpecHash) == "" {
		return nil, ErrPostcondition
	}
	ports := map[int]sandboxrunner.PortState{}
	for _, p := range inspected.Container.Ports {
		if p.HostIP != "127.0.0.1" || p.HostPort < 1 || p.HostPort > 65535 {
			return nil, ErrPostcondition
		}
		ports[p.InternalPort] = p
	}
	out := []projectworkspace.EndpointRoute{}
	for _, endpoint := range endpoints {
		if endpoint.ApplicationID == nil || *endpoint.ApplicationID != app.ID || endpoint.DesiredState != "enabled" {
			continue
		}
		p, ok := ports[endpoint.InternalPort]
		if !ok {
			return nil, fmt.Errorf("%w: verified container omitted endpoint %s port %d", ErrPostcondition, endpoint.ID, endpoint.InternalPort)
		}
		route, err := r.projects.ApplyEndpointRoute(ctx, projectworkspace.ApplyEndpointRouteCommand{EndpointID: endpoint.ID, ApplicationID: app.ID, ApplicationRevision: app.Revision, HostIP: p.HostIP, HostPort: p.HostPort, TransportProtocol: "tcp", ContainerSpecHash: inspected.Container.SpecHash, ObservationID: ev.observation.ID, VerificationID: ev.verification.ID, ActorPrincipalID: cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		if err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, nil
}

func (r *Reconciler) applyAndVerify(ctx context.Context, workspaceID string, cmd ReconcileCommand, plan mutationPlan) (evidence, error) {
	op, err := r.operations.Prepare(ctx, operation.PrepareCommand{WorkspaceID: workspaceID, TaskID: cmd.TaskID, AttemptID: cmd.AttemptID, PrincipalID: cmd.MutationPrincipalID, CapabilityLeaseID: cmd.MutationLeaseID, IdempotencyKey: plan.IdempotencyKey, ToolID: plan.ToolID, ToolVersion: "1", ResourceRef: plan.ResourceRef, Input: plan.Input, DesiredState: plan.DesiredState, Precondition: plan.Precondition, Reconciliation: plan.Reconciliation, Compensation: plan.Compensation, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return evidence{}, err
	}
	recovering := false
	if op.State == operation.StateExecuting {
		return evidence{}, ErrRecoveryRequired
	}
	if op.State == operation.StateUnknownOutcome {
		op, err = r.operations.BlockUnknown(ctx, operation.RecoveryCommand{OperationID: op.ID, ExpectedRevision: op.Revision, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		if err != nil {
			return evidence{}, err
		}
	}
	if op.State == operation.StateBlockedUnknownOutcome {
		recovering = true
	}
	if op.State == operation.StatePrepared {
		op, err = r.operations.Execute(ctx, operation.ExecuteCommand{OperationID: op.ID, ExpectedRevision: op.Revision, LeaseID: cmd.MutationLeaseID, Input: plan.Input, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		if err != nil {
			if errors.Is(err, operation.ErrUnknownOutcome) {
				return evidence{}, ErrRecoveryRequired
			}
			return evidence{}, err
		}
	}
	if op.State != operation.StateObserving && op.State != operation.StateCommitted && op.State != operation.StateBlockedUnknownOutcome {
		return evidence{}, fmt.Errorf("%w: operation state %s", ErrPostcondition, op.State)
	}
	inv, err := r.tools.Invoke(ctx, tool.InvokeCommand{WorkspaceID: workspaceID, TaskID: cmd.TaskID, PrincipalID: cmd.ObserverPrincipalID, LeaseID: cmd.ObserverLeaseID, ToolID: plan.InspectToolID, ToolVersion: "1", ResourceRef: plan.ResourceRef, Input: plan.InspectInput, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return evidence{}, err
	}
	if inv.Status != tool.StatusSucceeded || !json.Valid(inv.Result) {
		return evidence{}, ErrPostcondition
	}
	adapterID, adapterVersion := sandboxrunner.AdapterID, sandboxrunner.AdapterVersion
	obs, err := r.observations.Record(ctx, observation.RecordCommand{WorkspaceID: workspaceID, SubjectRef: plan.SubjectRef, ObservationType: plan.ObservationType, ProbeToolID: plan.InspectToolID, ProbeToolVersion: "1", SourcePrincipalID: &cmd.ObserverPrincipalID, AdapterID: &adapterID, AdapterVersion: &adapterVersion, Value: inv.Result, Label: policy.DataLabel{WorkspaceID: workspaceID, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUnverifiedDerived}, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return evidence{}, err
	}
	if err := r.observations.VerifyIntegrity(ctx, obs.ID); err != nil {
		return evidence{}, err
	}

	required := policy.VerificationV2
	if op.RequiredVerification != nil && verification.LevelRank(*op.RequiredVerification) > verification.LevelRank(required) {
		required = *op.RequiredVerification
	}
	spec, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "probe_tool_id": plan.InspectToolID, "predicate": string(plan.DesiredState)})
	v, err := r.verifications.Create(ctx, verification.CreateCommand{WorkspaceID: workspaceID, TaskID: cmd.TaskID, OperationID: &op.ID, SubjectRef: plan.SubjectRef, RequiredLevel: required, Spec: spec, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return evidence{}, err
	}
	passed := plan.Postcondition(inv.Result)
	status := verification.StatusFail
	achieved := policy.VerificationV2
	if passed && verification.LevelRank(achieved) >= verification.LevelRank(required) {
		status = verification.StatusPass
	}
	vr, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "integrity_hash": obs.IntegrityHash, "postcondition_satisfied": passed, "tool_invocation_id": inv.ID})
	v, err = r.verifications.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: status, AchievedLevel: &achieved, Result: vr, VerifiedBy: cmd.ObserverPrincipalID, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return evidence{}, err
	}
	if !passed || v.Status != verification.StatusPass {
		return evidence{operation: op, observation: obs, verification: v}, ErrPostcondition
	}
	if recovering {
		op, err = r.operations.AcceptReconciledOutcome(ctx, operation.RecoveryCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: v.ID, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		if err != nil {
			return evidence{}, err
		}
	}
	if op.State == operation.StateObserving {
		op, err = r.operations.CommitVerified(ctx, operation.CommitCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: v.ID, ActorPrincipalID: &cmd.ObserverPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		if err != nil {
			return evidence{}, err
		}
	}
	return evidence{operation: op, observation: obs, verification: v}, nil
}

func runtimeEnsurePlan(r projectworkspace.ProjectRuntime) mutationPlan {
	input := mustJSON(map[string]any{"runtime_id": r.ID, "network_policy": rawOrObject(r.NetworkPolicyJSON), "action": "ensure"})
	expectInternal := runtimeNetworkInternal(r.NetworkPolicyJSON)
	return mutationPlan{ToolID: sandboxrunner.ToolRuntimeEnsure, InspectToolID: sandboxrunner.ToolRuntimeInspect, ResourceRef: "project_runtime:" + r.ID, SubjectRef: "project_runtime:" + r.ID, Input: input, InspectInput: mustJSON(map[string]any{"runtime_id": r.ID}), DesiredState: mustJSON(map[string]any{"workspace_exists": true, "rootless": true, "network_internal": expectInternal}), Reconciliation: mustJSON(map[string]any{"tool": sandboxrunner.ToolRuntimeInspect}), IdempotencyKey: fmt.Sprintf("project-runtime:%s:rev:%d:ensure", r.ID, r.Revision), ObservationType: "project_runtime_state", Postcondition: func(raw json.RawMessage) bool { return runtimeWorkspacePresent(raw, expectInternal) }}
}
func runtimeStopPlan(r projectworkspace.ProjectRuntime) mutationPlan {
	input := mustJSON(map[string]any{"runtime_id": r.ID, "action": "stop"})
	return mutationPlan{ToolID: sandboxrunner.ToolRuntimeStop, InspectToolID: sandboxrunner.ToolRuntimeInspect, ResourceRef: "project_runtime:" + r.ID, SubjectRef: "project_runtime:" + r.ID, Input: input, InspectInput: mustJSON(map[string]any{"runtime_id": r.ID}), DesiredState: mustJSON(map[string]any{"all_containers_stopped": true}), Reconciliation: mustJSON(map[string]any{"tool": sandboxrunner.ToolRuntimeInspect}), IdempotencyKey: fmt.Sprintf("project-runtime:%s:rev:%d:stop", r.ID, r.Revision), ObservationType: "project_runtime_state", Postcondition: runtimeStopped}
}
func appPullPlan(r projectworkspace.ProjectRuntime, a projectworkspace.Application) mutationPlan {
	input := mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID, "image": a.SourceRef, "action": "pull"})
	inspect := mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID, "image": a.SourceRef})
	return mutationPlan{ToolID: sandboxrunner.ToolAppPull, InspectToolID: sandboxrunner.ToolImageInspect, ResourceRef: "project_runtime:" + r.ID, SubjectRef: "project_app:" + a.ID, Input: input, InspectInput: inspect, DesiredState: mustJSON(map[string]any{"image_present": a.SourceRef}), Reconciliation: mustJSON(map[string]any{"tool": sandboxrunner.ToolImageInspect}), IdempotencyKey: fmt.Sprintf("project-app:%s:rev:%d:pull", a.ID, a.Revision), ObservationType: "project_application_image", Postcondition: func(raw json.RawMessage) bool { return imagePresent(raw, a.SourceRef) }}
}
func appEnsurePlan(r projectworkspace.ProjectRuntime, a projectworkspace.Application, endpoints []sandboxrunner.PortSpec) mutationPlan {
	input := mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID, "image": a.SourceRef, "runtime_spec": rawOrObject(a.RuntimeSpecJSON), "resource_limits": rawOrObject(r.ResourceLimitsJSON), "environment_bindings": rawOrObject(a.EnvironmentBindingsJSON), "network_policy": rawOrObject(r.NetworkPolicyJSON), "endpoints": endpoints, "action": "ensure"})
	return mutationPlan{ToolID: sandboxrunner.ToolAppEnsure, InspectToolID: sandboxrunner.ToolAppInspect, ResourceRef: "project_runtime:" + r.ID, SubjectRef: "project_app:" + a.ID, Input: input, InspectInput: mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID}), DesiredState: mustJSON(map[string]any{"status": "running", "isolation_verified": true}), Reconciliation: mustJSON(map[string]any{"tool": sandboxrunner.ToolAppInspect}), Compensation: mustJSON(map[string]any{"tool": sandboxrunner.ToolAppStop, "runtime_id": r.ID, "application_id": a.ID}), IdempotencyKey: fmt.Sprintf("project-app:%s:rev:%d:ensure", a.ID, a.Revision), ObservationType: "project_application_state", Postcondition: appRunningIsolated}
}
func appStopPlan(r projectworkspace.ProjectRuntime, a projectworkspace.Application) mutationPlan {
	input := mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID, "action": "stop"})
	return mutationPlan{ToolID: sandboxrunner.ToolAppStop, InspectToolID: sandboxrunner.ToolAppInspect, ResourceRef: "project_runtime:" + r.ID, SubjectRef: "project_app:" + a.ID, Input: input, InspectInput: mustJSON(map[string]any{"runtime_id": r.ID, "application_id": a.ID}), DesiredState: mustJSON(map[string]any{"status": []string{"stopped", "exited", "absent"}}), Reconciliation: mustJSON(map[string]any{"tool": sandboxrunner.ToolAppInspect}), IdempotencyKey: fmt.Sprintf("project-app:%s:rev:%d:stop", a.ID, a.Revision), ObservationType: "project_application_state", Postcondition: appStopped}
}

func runtimeNetworkInternal(raw json.RawMessage) bool {
	var v struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	return v.Mode != "external"
}
func runtimeWorkspacePresent(raw json.RawMessage, expectInternal bool) bool {
	var v struct {
		WorkspaceExists bool                        `json:"workspace_exists"`
		Network         sandboxrunner.NetworkState  `json:"network"`
		Engine          sandboxrunner.EngineProfile `json:"engine"`
	}
	return json.Unmarshal(raw, &v) == nil && v.WorkspaceExists && v.Engine.Rootless && v.Network.Internal == expectInternal
}
func runtimeStopped(raw json.RawMessage) bool {
	var v struct {
		Containers []sandboxrunner.ContainerState `json:"containers"`
		Engine     sandboxrunner.EngineProfile    `json:"engine"`
	}
	if json.Unmarshal(raw, &v) != nil || !v.Engine.Rootless {
		return false
	}
	for _, c := range v.Containers {
		if c.Status == "running" || c.Status == "starting" || c.Status == "restarting" {
			return false
		}
	}
	return true
}
func appRunningIsolated(raw json.RawMessage) bool {
	var v struct {
		Container sandboxrunner.ContainerState `json:"container"`
		Engine    sandboxrunner.EngineProfile  `json:"engine"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Engine.Rootless && v.Container.Status == "running" && v.Container.IsolationVerified
}
func appStopped(raw json.RawMessage) bool {
	var v struct {
		Container sandboxrunner.ContainerState `json:"container"`
		Engine    sandboxrunner.EngineProfile  `json:"engine"`
	}
	if json.Unmarshal(raw, &v) != nil || !v.Engine.Rootless {
		return false
	}
	switch v.Container.Status {
	case "stopped", "exited", "absent", "created":
		return true
	}
	return false
}
func imagePresent(raw json.RawMessage, ref string) bool {
	var v struct {
		Image  sandboxrunner.ImageState    `json:"image"`
		Engine sandboxrunner.EngineProfile `json:"engine"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Engine.Rootless && v.Image.Reference == ref
}
func endpointSpecsForApp(endpoints []projectworkspace.Endpoint, appID string) []sandboxrunner.PortSpec {
	seen := map[int]bool{}
	var out []sandboxrunner.PortSpec
	for _, e := range endpoints {
		if e.ApplicationID == nil || *e.ApplicationID != appID || e.DesiredState != "enabled" {
			continue
		}
		if seen[e.InternalPort] {
			continue
		}
		seen[e.InternalPort] = true
		out = append(out, sandboxrunner.PortSpec{InternalPort: e.InternalPort, Protocol: "tcp"})
	}
	return out
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func rawOrObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return map[string]any{}
	}
	return v
}
