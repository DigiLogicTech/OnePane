package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func (s *Service) Request(ctx context.Context, id string) (InferenceRequest, error) {
	if strings.TrimSpace(id) == "" {
		return InferenceRequest{}, fmt.Errorf("%w: request id required", ErrInvalidCommand)
	}
	return s.requests.Get(ctx, id)
}

func runtimePriority(metadata json.RawMessage) string {
	var v map[string]any
	if json.Unmarshal(metadata, &v) == nil {
		for _, key := range []string{"runtime_priority", "scheduling_class", "priority"} {
			if raw, ok := v[key].(string); ok {
				switch strings.ToLower(strings.TrimSpace(raw)) {
				case "interactive", "user_interactive":
					return "interactive"
				case "high":
					return "high"
				case "background", "batch":
					return "background"
				}
			}
		}
	}
	return "normal"
}

func canonicalObject(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	b, err := normalizeJSON(raw, fallback)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("JSON object required")
	}
	return b, nil
}

func (s *Service) Execute(ctx context.Context, cmd ExecuteCommand) (InferenceRequest, error) {
	if s == nil || s.artifacts == nil || s.transports == nil || strings.TrimSpace(s.localNodeID) == "" {
		return InferenceRequest{}, ErrExecutionUnavailable
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || strings.TrimSpace(cmd.DeploymentID) == "" {
		return InferenceRequest{}, fmt.Errorf("%w: workspace, principal and deployment required", ErrInvalidCommand)
	}
	if cmd.SessionID != nil {
		return InferenceRequest{}, fmt.Errorf("%w: inference sessions are introduced with the Agent Runtime milestone", ErrInvalidCommand)
	}
	if cmd.InputLabel.WorkspaceID != cmd.WorkspaceID {
		return InferenceRequest{}, fmt.Errorf("%w: input label workspace mismatch", ErrInvalidCommand)
	}
	if err := policy.ValidateDataLabel(cmd.InputLabel); err != nil {
		return InferenceRequest{}, fmt.Errorf("%w: input label: %v", ErrInvalidCommand, err)
	}
	capability, err := canonicalObject(cmd.CapabilityJSON, "{}")
	if err != nil {
		return InferenceRequest{}, fmt.Errorf("%w: capability_json: %v", ErrInvalidCommand, err)
	}
	manifest, err := canonicalObject(cmd.ContextManifestJSON, "{}")
	if err != nil {
		return InferenceRequest{}, fmt.Errorf("%w: context_manifest_json: %v", ErrInvalidCommand, err)
	}
	metadata, err := canonicalObject(cmd.ClassificationMetadata, "{}")
	if err != nil {
		return InferenceRequest{}, fmt.Errorf("%w: classification metadata: %v", ErrInvalidCommand, err)
	}
	requestJSON, err := canonicalObject(cmd.RequestJSON, "{}")
	if err != nil {
		return InferenceRequest{}, fmt.Errorf("%w: request_json: %v", ErrInvalidCommand, err)
	}

	d0, err := s.repo.Deployment(ctx, cmd.DeploymentID)
	if err != nil {
		return InferenceRequest{}, err
	}
	outputLabel, ok := policy.InheritLabels(cmd.InputLabel)
	if !ok {
		return InferenceRequest{}, fmt.Errorf("%w: cannot derive output label", ErrInformationFlow)
	}
	classification, _ := json.Marshal(map[string]any{"input_label": cmd.InputLabel, "output_label": outputLabel, "metadata": json.RawMessage(metadata)})
	requestID, err := s.ids.New("infer")
	if err != nil {
		return InferenceRequest{}, err
	}
	now := s.clock.UnixMilli()
	deploymentID := d0.ID
	q := InferenceRequest{ID: requestID, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, PrincipalID: cmd.PrincipalID, DeploymentID: &deploymentID, ProviderConnectionID: d0.ProviderConnectionID, Status: RequestCreated, CapabilityJSON: capability, ContextManifestJSON: manifest, ClassificationJSON: classification, RequestJSON: requestJSON, CreatedAt: now, UpdatedAt: now}
	if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		eligible, err := s.requests.PrincipalEligibleTx(ctx, tx, cmd.WorkspaceID, cmd.PrincipalID)
		if err != nil {
			return fmt.Errorf("resolve inference principal: %w", err)
		}
		if !eligible {
			return ErrPrincipalIneligible
		}
		if cmd.TaskID != nil {
			ws, err := s.requests.TaskWorkspaceTx(ctx, tx, *cmd.TaskID)
			if err != nil {
				return fmt.Errorf("resolve inference task: %w", err)
			}
			if ws != cmd.WorkspaceID {
				return ErrTaskWorkspace
			}
		}
		if err := s.requests.Insert(ctx, tx, q); err != nil {
			return err
		}
		if cmd.BudgetReservationID != nil {
			if s.budgets == nil {
				return ErrBudgetUnavailable
			}
			if err := s.budgets.BindInferenceRequestTx(ctx, tx, *cmd.BudgetReservationID, q.ID, cmd.WorkspaceID, cmd.TaskID); err != nil {
				return err
			}
		}
		evt, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"inference_request_id": q.ID, "deployment_id": d0.ID, "status": q.Status})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &q.WorkspaceID, Type: "inference_request.created", AggregateType: "inference_request", AggregateID: q.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	}); err != nil {
		return InferenceRequest{}, err
	}

	d, m, p, transport, effectiveOutputLabel, err := s.resolveExecution(ctx, cmd.WorkspaceID, cmd.DeploymentID, cmd.InputLabel)
	if err != nil {
		return s.finishPreDispatchError(ctx, q.ID, err, cmd)
	}
	var runtimeLease RuntimeScheduleLease
	if s.runtimeCoordinator != nil {
		runtimeLease, err = s.runtimeCoordinator.Acquire(ctx, RuntimeScheduleRequest{
			WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, InferenceRequestID: q.ID,
			Deployment: d, Priority: runtimePriority(metadata),
		})
		if err != nil {
			return s.finishPreDispatchError(ctx, q.ID, fmt.Errorf("runtime scheduling: %w", err), cmd)
		}
		_ = runtimeLease.ReportBoundary(ctx, "unsafe")
		defer func() {
			_ = runtimeLease.ReportBoundary(context.Background(), "complete")
			runtimeLease.Release(context.Background(), nil)
		}()
	}
	requiresBudget := inferenceRouteRequiresBudget(d, p)
	if requiresBudget && cmd.BudgetReservationID == nil {
		return s.finishPreDispatchError(ctx, q.ID, ErrBudgetRequired, cmd)
	}
	if cmd.BudgetReservationID != nil && s.budgets == nil {
		return s.finishPreDispatchError(ctx, q.ID, ErrBudgetUnavailable, cmd)
	}
	outputLabel = effectiveOutputLabel
	executionNode := d.NodeID
	if err := s.transitionRequest(ctx, q.ID, RequestCreated, RequestRouted, d.ProviderConnectionID, strptrlocal(s.localNodeID), executionNode, jsonFields{}, cmd, "inference_request.routed", map[string]any{"transport": selectTransportKey(d, p), "budget_required": requiresBudget}); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return s.requests.Get(ctx, q.ID)
	}
	from := RequestRouted
	if cmd.BudgetReservationID != nil {
		if err := s.transitionRequest(ctx, q.ID, RequestRouted, RequestBudgetReserved, nil, nil, nil, jsonFields{}, cmd, "inference_request.budget_reserved", map[string]any{"budget_reservation_id": *cmd.BudgetReservationID}); err != nil {
			s.releaseBudgetBeforeDispatch(ctx, cmd)
			return s.requests.Get(ctx, q.ID)
		}
		from = RequestBudgetReserved
	}
	if err := s.transitionRequest(ctx, q.ID, from, RequestDispatched, nil, nil, nil, jsonFields{}, cmd, "inference_request.dispatched", nil); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return s.requests.Get(ctx, q.ID)
	}
	if err := s.transitionRequest(ctx, q.ID, RequestDispatched, RequestExecuting, nil, nil, nil, jsonFields{}, cmd, "inference_request.executing", nil); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return s.requests.Get(ctx, q.ID)
	}

	result, dispatchErr := transport.Dispatch(ctx, DispatchRequest{RequestID: q.ID, Model: m, Deployment: d, Provider: p, RequestJSON: requestJSON}, s.secrets)
	if dispatchErr != nil {
		if cmd.BudgetReservationID != nil && s.budgets != nil {
			// Once transport dispatch was attempted, account conservatively: the
			// provider/subscription may have consumed quota even when the outcome
			// is unknown or failed.
			_, _ = s.budgets.Commit(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		}
		return s.finishTransportError(ctx, q.ID, p, dispatchErr, cmd)
	}
	if !json.Valid(result.ResponseJSON) {
		if cmd.BudgetReservationID != nil && s.budgets != nil {
			_, _ = s.budgets.Commit(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		}
		return s.finishTransportError(ctx, q.ID, p, &TransportError{Code: "invalid_transport_result", OutcomeKnown: true}, cmd)
	}
	if cmd.BudgetReservationID != nil && s.budgets != nil {
		if _, err := s.budgets.CommitFromUsage(ctx, *cmd.BudgetReservationID, result.UsageJSON, cmd.ActorPrincipalID); err != nil {
			return s.finishTransportError(ctx, q.ID, p, &TransportError{Code: "budget_commit_failed", OutcomeKnown: true, Err: err}, cmd)
		}
	}

	artifactMeta, _ := json.Marshal(map[string]any{"inference_request_id": q.ID, "deployment_id": d.ID, "model_id": m.ID, "provider_connection_id": d.ProviderConnectionID, "transport": selectTransportKey(d, p)})
	creator := cmd.PrincipalID
	a, err := s.artifacts.Create(ctx, artifact.CreateCommand{WorkspaceID: cmd.WorkspaceID, MediaType: "application/json", Label: outputLabel, Status: artifact.StatusActive, Metadata: artifactMeta, CreatedBy: &creator, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID}, bytes.NewReader(result.ResponseJSON))
	if err != nil {
		return s.finishTransportError(ctx, q.ID, p, &TransportError{Code: "response_persist_failed", OutcomeKnown: true, Err: err}, cmd)
	}
	completed := s.clock.UnixMilli()
	fields := jsonFields{ResponseArtifactID: &a.ID, UsageJSON: result.UsageJSON, CompletedAt: &completed}
	if err := s.transitionRequest(ctx, q.ID, RequestExecuting, RequestSucceeded, nil, nil, nil, fields, cmd, "inference_request.succeeded", map[string]any{"response_artifact_id": a.ID}); err != nil {
		return s.requests.Get(ctx, q.ID)
	}
	return s.requests.Get(ctx, q.ID)
}

func (s *Service) resolveExecution(ctx context.Context, workspaceID, deploymentID string, input policy.DataLabel) (ModelDeployment, Model, *ProviderConnection, Transport, policy.DataLabel, error) {
	d, err := s.repo.Deployment(ctx, deploymentID)
	if err != nil {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, err
	}
	if d.Status != DeploymentReady {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrDeploymentNotReady
	}
	m, err := s.repo.Model(ctx, d.ModelID)
	if err != nil {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, err
	}
	if m.TrustState == ModelRevoked {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrModelRevoked
	}
	if m.TrustState == ModelQuarantined {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrModelQuarantined
	}
	isRemoteNode := d.NodeID != nil && *d.NodeID != s.localNodeID
	if isRemoteNode {
		trust, err := s.repo.NodeTrust(ctx, *d.NodeID)
		if err != nil || trust != "paired" {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrRemoteNodeUnsupported
		}
	}
	output, ok := policy.InheritLabels(input)
	if !ok {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrInformationFlow
	}
	var p *ProviderConnection
	var dest policy.FlowDestination
	if d.ProviderConnectionID != nil {
		pv, err := s.repo.Provider(ctx, *d.ProviderConnectionID)
		if err != nil {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, err
		}
		if pv.Status != ProviderConnected && pv.Status != ProviderDegraded {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, fmt.Errorf("%w: provider status %s", ErrTransportUnavailable, pv.Status)
		}
		pd, err := providerDataPolicy(pv.ConnectionJSON)
		if err != nil {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, fmt.Errorf("provider data policy: %w", err)
		}
		conf, res, _, err := policy.StorageLabel(input)
		if err != nil {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, err
		}
		if !pd.Allows(conf, res) {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrInformationFlow
		}
		clearance := storageConfidentiality(pd.MaxConfidentiality)
		if clearance == "" {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrInformationFlow
		}
		dest = policy.FlowDestination{WorkspaceID: workspaceID, Kind: policy.DestinationKind(pd.DestinationKind), Clearance: clearance}
		if dest.Kind == policy.DestinationOriginNode || dest.Kind == policy.DestinationTrustedNode {
			if d.NodeID == nil {
				return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrInformationFlow
			}
			dest.NodeID = *d.NodeID
		}
		if dest.Kind == policy.DestinationOriginNode && dest.NodeID != s.localNodeID {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrInformationFlow
		}
		p = &pv
	} else {
		if d.NodeID == nil {
			return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, ErrRemoteNodeUnsupported
		}
		if isRemoteNode {
			dest = policy.FlowDestination{WorkspaceID: workspaceID, Kind: policy.DestinationTrustedNode, NodeID: *d.NodeID, Clearance: policy.ConfidentialitySecret}
		} else {
			dest = policy.FlowDestination{WorkspaceID: workspaceID, Kind: policy.DestinationOriginNode, NodeID: s.localNodeID, Clearance: policy.ConfidentialitySecret}
		}
	}
	flow := policy.EvaluateInformationFlow(policy.InformationFlowInput{Source: input, Destination: dest, ProposedDerivedLabel: &output, VerificationEstablished: false})
	if !flow.Allowed {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, fmt.Errorf("%w: %v", ErrInformationFlow, flow.Reasons)
	}
	key := selectTransportKey(d, p)
	transport, ok := s.transports.Resolve(key)
	if !ok {
		return ModelDeployment{}, Model{}, nil, nil, policy.DataLabel{}, fmt.Errorf("%w: %s", ErrTransportUnavailable, key)
	}
	return d, m, p, transport, flow.EffectiveLabel, nil
}

func (s *Service) releaseBudgetBeforeDispatch(ctx context.Context, cmd ExecuteCommand) {
	if cmd.BudgetReservationID == nil || s.budgets == nil {
		return
	}
	_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
}

func inferenceRouteRequiresBudget(d ModelDeployment, p *ProviderConnection) bool {
	// Local deployments are zero incremental cost. Provider-backed routes are
	// protected unless they explicitly prove a hard-zero route. Deployment
	// runtime scheduling metadata may override the provider classification.
	type sched struct {
		CostClass string `json:"cost_class"`
		HardZero  bool   `json:"hard_zero_incremental_cost"`
	}
	type wrapped struct {
		Scheduling sched `json:"scheduling"`
	}
	var effective sched
	if p != nil {
		var c wrapped
		if json.Unmarshal(p.ConnectionJSON, &c) == nil {
			effective = c.Scheduling
		}
	}
	var dc wrapped
	if json.Unmarshal(d.RuntimeConfigJSON, &dc) == nil && strings.TrimSpace(dc.Scheduling.CostClass) != "" {
		effective = dc.Scheduling
	}
	if p == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(effective.CostClass)) {
	case "local":
		return false
	case "free":
		return !effective.HardZero
	case "included_subscription", "provider_managed", "paid", "unknown", "":
		return true
	default:
		return true
	}
}

func storageConfidentiality(v string) policy.Confidentiality {
	switch strings.ToLower(v) {
	case "public":
		return policy.ConfidentialityPublic
	case "internal":
		return policy.ConfidentialityInternal
	case "confidential":
		return policy.ConfidentialityConfidential
	case "secret":
		return policy.ConfidentialitySecret
	default:
		return ""
	}
}
func strptrlocal(v string) *string { return &v }

func (s *Service) transitionRequest(ctx context.Context, id string, from, to RequestStatus, provider, origin, execution *string, fields jsonFields, cmd ExecuteCommand, eventType string, extra map[string]any) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := s.requests.Transition(ctx, tx, id, from, to, now, origin, execution, provider, fields); err != nil {
			return err
		}
		evt, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload := map[string]any{"inference_request_id": id, "from": from, "to": to}
		for k, v := range extra {
			payload[k] = v
		}
		if fields.ErrorCode != nil {
			payload["error_code"] = *fields.ErrorCode
		}
		b, _ := json.Marshal(payload)
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &cmd.WorkspaceID, Type: eventType, AggregateType: "inference_request", AggregateID: id, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: b, OccurredAt: now})
	})
}

func (s *Service) finishPreDispatchError(ctx context.Context, id string, cause error, cmd ExecuteCommand) (InferenceRequest, error) {
	if cmd.BudgetReservationID != nil && s.budgets != nil {
		_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	}
	code := "pre_dispatch_denied"
	switch {
	case errors.Is(cause, ErrInformationFlow):
		code = "information_flow_denied"
	case errors.Is(cause, ErrDeploymentNotReady):
		code = "deployment_not_ready"
	case errors.Is(cause, ErrModelQuarantined):
		code = "model_quarantined"
	case errors.Is(cause, ErrModelRevoked):
		code = "model_revoked"
	case errors.Is(cause, ErrRemoteNodeUnsupported):
		code = "remote_node_unsupported"
	case errors.Is(cause, ErrTransportUnavailable):
		code = "transport_unavailable"
	case errors.Is(cause, ErrBudgetRequired):
		code = "budget_required"
	case errors.Is(cause, ErrBudgetUnavailable):
		code = "budget_unavailable"
	}
	completed := s.clock.UnixMilli()
	fields := jsonFields{ErrorCode: &code, CompletedAt: &completed}
	if err := s.transitionRequest(ctx, id, RequestCreated, RequestFailed, nil, nil, nil, fields, cmd, "inference_request.failed", map[string]any{"phase": "pre_dispatch"}); err != nil {
		r, getErr := s.requests.Get(ctx, id)
		if getErr != nil {
			return InferenceRequest{}, err
		}
		return r, err
	}
	r, err := s.requests.Get(ctx, id)
	if err != nil {
		return InferenceRequest{}, err
	}
	return r, cause
}

func (s *Service) finishTransportError(ctx context.Context, id string, p *ProviderConnection, dispatchErr error, cmd ExecuteCommand) (InferenceRequest, error) {
	status := RequestUnknown
	code := "transport_error"
	var te *TransportError
	if errors.As(dispatchErr, &te) {
		if strings.TrimSpace(te.Code) != "" {
			code = te.Code
		}
		if te.HTTPStatus == 429 {
			status = RequestRateLimited
		} else if te.OutcomeKnown {
			status = RequestFailed
		}
	}
	completed := s.clock.UnixMilli()
	fields := jsonFields{ErrorCode: &code, CompletedAt: &completed}
	if err := s.transitionRequest(ctx, id, RequestExecuting, status, nil, nil, nil, fields, cmd, "inference_request."+string(status), nil); err != nil {
		r, getErr := s.requests.Get(ctx, id)
		if getErr != nil {
			return InferenceRequest{}, err
		}
		return r, err
	}
	if status == RequestRateLimited && p != nil && te != nil {
		_, _ = s.SetProviderStatus(ctx, SetProviderStatusCommand{ConnectionID: p.ID, ExpectedRevision: p.Revision, Status: ProviderRateLimited, RetryAfter: te.RetryAfterMS, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "inference transport returned 429"})
	}
	r, err := s.requests.Get(ctx, id)
	if err != nil {
		return InferenceRequest{}, err
	}
	return r, dispatchErr
}

// DispatchFederatedLocal executes a bounded inference request on behalf of a
// paired node. It accepts only a ready deployment physically owned by this node
// and never provider-backed/cloud deployments, preventing recursive federation
// and preventing a peer from consuming this node's cloud credentials.
func (s *Service) DispatchFederatedLocal(ctx context.Context, deploymentID, requestID string, requestJSON json.RawMessage) (DispatchResult, error) {
	if s == nil || s.transports == nil || strings.TrimSpace(s.localNodeID) == "" {
		return DispatchResult{}, ErrExecutionUnavailable
	}
	if strings.TrimSpace(deploymentID) == "" || strings.TrimSpace(requestID) == "" || !json.Valid(requestJSON) {
		return DispatchResult{}, fmt.Errorf("%w: invalid federated inference request", ErrInvalidCommand)
	}
	d, err := s.repo.Deployment(ctx, deploymentID)
	if err != nil {
		return DispatchResult{}, err
	}
	if d.Status != DeploymentReady && d.Status != DeploymentDegraded {
		return DispatchResult{}, ErrDeploymentNotReady
	}
	if d.NodeID == nil || *d.NodeID != s.localNodeID || d.ProviderConnectionID != nil {
		return DispatchResult{}, ErrRemoteNodeUnsupported
	}
	m, err := s.repo.Model(ctx, d.ModelID)
	if err != nil {
		return DispatchResult{}, err
	}
	if m.TrustState == ModelRevoked || m.TrustState == ModelQuarantined {
		return DispatchResult{}, ErrModelRevoked
	}
	key := selectTransportKey(d, nil)
	if key == "remote-node" {
		return DispatchResult{}, ErrRemoteNodeUnsupported
	}
	transport, ok := s.transports.Resolve(key)
	if !ok {
		return DispatchResult{}, fmt.Errorf("%w: %s", ErrTransportUnavailable, key)
	}
	return transport.Dispatch(ctx, DispatchRequest{RequestID: requestID, Model: m, Deployment: d, RequestJSON: requestJSON}, s.secrets)
}
