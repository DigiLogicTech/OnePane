package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func (s *Service) Invocation(ctx context.Context, id string) (Invocation, error) {
	if strings.TrimSpace(id) == "" {
		return Invocation{}, fmt.Errorf("%w: invocation id required", ErrInvalidCommand)
	}
	return s.invocations.Get(ctx, id)
}

func defaultProposalTypes(tool bool) []agentprotocol.ProposalType {
	out := []agentprotocol.ProposalType{agentprotocol.ProposalDelegate, agentprotocol.ProposalReplan, agentprotocol.ProposalComplete, agentprotocol.ProposalHuman, agentprotocol.ProposalEscalate, agentprotocol.ProposalWait, agentprotocol.ProposalFail}
	if tool {
		out = append([]agentprotocol.ProposalType{agentprotocol.ProposalTool}, out...)
	}
	return out
}

func (s *Service) Invoke(ctx context.Context, cmd InvokeCommand) (InvokeResult, error) {
	if s == nil || s.artifacts == nil || s.transports == nil || strings.TrimSpace(s.localNodeID) == "" {
		return InvokeResult{}, ErrRuntimeExecutionUnavailable
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || strings.TrimSpace(cmd.ConnectionID) == "" || strings.TrimSpace(cmd.Objective) == "" {
		return InvokeResult{}, fmt.Errorf("%w: workspace, principal, connection and objective required", ErrInvalidCommand)
	}
	if cmd.InputLabel.WorkspaceID != cmd.WorkspaceID {
		return InvokeResult{}, fmt.Errorf("%w: input label workspace mismatch", ErrInvalidCommand)
	}
	if err := policy.ValidateDataLabel(cmd.InputLabel); err != nil {
		return InvokeResult{}, fmt.Errorf("%w: input label: %v", ErrInvalidCommand, err)
	}
	c, err := s.repo.Get(ctx, cmd.ConnectionID)
	if err != nil {
		return InvokeResult{}, err
	}
	if len(cmd.Constraints) == 0 {
		cmd.Constraints = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Constraints) {
		return InvokeResult{}, fmt.Errorf("%w: constraints JSON", ErrInvalidCommand)
	}
	if len(cmd.ContextManifest) == 0 {
		cmd.ContextManifest = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.ContextManifest) {
		return InvokeResult{}, fmt.Errorf("%w: context manifest JSON", ErrInvalidCommand)
	}
	permitted := append([]agentprotocol.ProposalType(nil), cmd.PermittedProposalTypes...)
	if len(permitted) == 0 {
		permitted = defaultProposalTypes(c.OperatingMode == GatewayMediated)
	}
	if c.OperatingMode != GatewayMediated {
		for _, p := range permitted {
			if p == agentprotocol.ProposalTool {
				return InvokeResult{}, fmt.Errorf("%w: tool proposals require gateway_mediated mode", ErrInvalidCommand)
			}
		}
	}
	invID, err := s.ids.New("agentinv")
	if err != nil {
		return InvokeResult{}, err
	}
	req := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: invID, WorkspaceID: cmd.WorkspaceID, PrincipalID: cmd.PrincipalID, Role: strings.TrimSpace(cmd.Role), Objective: strings.TrimSpace(cmd.Objective), Constraints: append(json.RawMessage(nil), cmd.Constraints...), Context: append([]agentprotocol.ContextSection(nil), cmd.Context...), ContextManifest: append(json.RawMessage(nil), cmd.ContextManifest...), PermittedProposalTypes: permitted, ToolCallback: c.OperatingMode == GatewayMediated}
	if cmd.TaskID != nil {
		req.TaskID = *cmd.TaskID
	}
	if cmd.AttemptID != nil {
		req.AttemptID = *cmd.AttemptID
	}
	if err := req.Validate(); err != nil {
		return InvokeResult{}, err
	}
	requestJSON, _ := json.Marshal(req)
	now := s.clock.UnixMilli()
	inv := Invocation{ID: invID, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, AttemptID: cmd.AttemptID, PrincipalID: cmd.PrincipalID, ConnectionID: c.ID, Status: InvocationCreated, RequestJSON: requestJSON, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		eligible, err := s.invocations.PrincipalEligibleTx(ctx, tx, cmd.WorkspaceID, cmd.PrincipalID)
		if err != nil {
			return fmt.Errorf("resolve runtime principal: %w", err)
		}
		if !eligible {
			return ErrRuntimeNotSchedulable
		}
		if cmd.TaskID != nil {
			ws, err := s.invocations.TaskWorkspaceTx(ctx, tx, *cmd.TaskID)
			if err != nil {
				return err
			}
			if ws != cmd.WorkspaceID {
				return ErrInvalidCommand
			}
		}
		if cmd.AttemptID != nil {
			if cmd.TaskID == nil {
				return fmt.Errorf("%w: attempt requires task", ErrInvalidCommand)
			}
			taskID, err := s.invocations.AttemptTaskTx(ctx, tx, *cmd.AttemptID)
			if err != nil {
				return err
			}
			if taskID != *cmd.TaskID {
				return fmt.Errorf("%w: attempt/task mismatch", ErrInvalidCommand)
			}
		}
		if err := s.invocations.Insert(ctx, tx, inv); err != nil {
			return err
		}
		if cmd.BudgetReservationID != nil {
			if s.budgets == nil {
				return ErrRuntimeBudgetUnavailable
			}
			if err := s.budgets.BindAgentRuntimeInvocationTx(ctx, tx, *cmd.BudgetReservationID, inv.ID, cmd.WorkspaceID, cmd.TaskID); err != nil {
				return err
			}
		}
		evt, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"invocation_id": inv.ID, "connection_id": inv.ConnectionID, "status": inv.Status, "operating_mode": c.OperatingMode})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &inv.WorkspaceID, Type: "agent_runtime_invocation.created", AggregateType: "agent_runtime_invocation", AggregateID: inv.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	}); err != nil {
		return InvokeResult{}, err
	}

	requiresBudget := runtimeRouteRequiresBudget(c)
	if requiresBudget && cmd.BudgetReservationID == nil {
		return s.finishInvocationError(ctx, inv.ID, InvocationCreated, ErrRuntimeBudgetRequired, cmd)
	}
	if cmd.BudgetReservationID != nil && s.budgets == nil {
		return s.finishInvocationError(ctx, inv.ID, InvocationCreated, ErrRuntimeBudgetUnavailable, cmd)
	}
	outputLabel, transport, authErr := s.authorizeInvocation(c, cmd.InputLabel)
	if authErr != nil {
		if cmd.BudgetReservationID != nil && s.budgets != nil {
			_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
		}
		return s.finishInvocationError(ctx, inv.ID, InvocationCreated, authErr, cmd)
	}
	if err := s.transitionInvocation(ctx, inv.ID, InvocationAuthorized, nil, nil, nil, cmd, "agent_runtime_invocation.authorized", map[string]any{"adapter": c.AdapterName + "@" + c.AdapterVersion, "budget_required": requiresBudget, "budget_reservation_id": cmd.BudgetReservationID}); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return InvokeResult{}, err
	}
	if err := s.transitionInvocation(ctx, inv.ID, InvocationDispatched, nil, nil, nil, cmd, "agent_runtime_invocation.dispatched", nil); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return InvokeResult{}, err
	}
	if err := s.transitionInvocation(ctx, inv.ID, InvocationExecuting, nil, nil, nil, cmd, "agent_runtime_invocation.executing", nil); err != nil {
		s.releaseBudgetBeforeDispatch(ctx, cmd)
		return InvokeResult{}, err
	}
	resp, callErr := transport.Invoke(ctx, c, req, s.secrets)
	if cmd.BudgetReservationID != nil && s.budgets != nil {
		// A transport dispatch attempt may consume external quota even if its
		// final outcome is failed/unknown. Commit conservatively.
		_, _ = s.budgets.Commit(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	}
	if callErr != nil {
		return s.finishInvocationError(ctx, inv.ID, InvocationExecuting, callErr, cmd)
	}
	if err := resp.ValidateFor(req); err != nil {
		return s.finishInvocationError(ctx, inv.ID, InvocationExecuting, &RuntimeTransportError{Code: "invalid_agent_response", OutcomeKnown: true, Err: err}, cmd)
	}
	body, _ := json.Marshal(resp)
	meta, _ := json.Marshal(map[string]any{"agent_runtime_invocation_id": inv.ID, "connection_id": c.ID, "runtime_kind": c.RuntimeKind, "proposal_type": resp.ProposalType})
	creator := cmd.PrincipalID
	a, err := s.artifacts.Create(ctx, artifact.CreateCommand{WorkspaceID: cmd.WorkspaceID, MediaType: "application/vnd.harness.agent-response+json", Label: outputLabel, Status: artifact.StatusActive, Metadata: meta, CreatedBy: &creator, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID}, bytes.NewReader(body))
	if err != nil {
		return s.finishInvocationError(ctx, inv.ID, InvocationExecuting, &RuntimeTransportError{Code: "response_persist_failed", OutcomeKnown: true, Err: err}, cmd)
	}
	completed := s.clock.UnixMilli()
	if err := s.transitionInvocation(ctx, inv.ID, InvocationSucceeded, &a.ID, nil, &completed, cmd, "agent_runtime_invocation.succeeded", map[string]any{"response_artifact_id": a.ID, "proposal_type": resp.ProposalType}); err != nil {
		return InvokeResult{}, err
	}
	stored, err := s.invocations.Get(ctx, inv.ID)
	if err != nil {
		return InvokeResult{}, err
	}
	return InvokeResult{Invocation: stored, Response: &resp}, nil
}

func (s *Service) releaseBudgetBeforeDispatch(ctx context.Context, cmd InvokeCommand) {
	if cmd.BudgetReservationID == nil || s.budgets == nil {
		return
	}
	_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: *cmd.BudgetReservationID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
}

func runtimeRouteRequiresBudget(c Connection) bool {
	var caps struct {
		Scheduling struct {
			CostClass string `json:"cost_class"`
			HardZero  bool   `json:"hard_zero_incremental_cost"`
		} `json:"scheduling"`
	}
	_ = json.Unmarshal(c.CapabilitiesJSON, &caps)
	switch strings.ToLower(strings.TrimSpace(caps.Scheduling.CostClass)) {
	case "local":
		return false
	case "free":
		return !caps.Scheduling.HardZero
	case "included_subscription", "provider_managed", "paid", "unknown", "":
		return true
	default:
		return true
	}
}

func (s *Service) authorizeInvocation(c Connection, input policy.DataLabel) (policy.DataLabel, RuntimeTransport, error) {
	if c.WorkspaceID != nil && *c.WorkspaceID != input.WorkspaceID {
		return policy.DataLabel{}, nil, ErrRuntimeInformationFlow
	}
	e := c.Eligibility()
	if !e.Schedulable {
		return policy.DataLabel{}, nil, fmt.Errorf("%w: %s", ErrRuntimeNotSchedulable, e.Reason)
	}
	var dp DataPolicy
	if err := json.Unmarshal(c.DataPolicyJSON, &dp); err != nil {
		return policy.DataLabel{}, nil, err
	}
	conf, res, _, err := policy.StorageLabel(input)
	if err != nil {
		return policy.DataLabel{}, nil, err
	}
	if !runtimePolicyAllows(dp, conf, res) {
		return policy.DataLabel{}, nil, ErrRuntimeInformationFlow
	}
	if dp.DestinationKind == "trusted_node" {
		return policy.DataLabel{}, nil, ErrTrustedNodeTransportUnavailable
	}
	clearance := runtimeClearance(dp.MaxConfidentiality)
	if clearance == "" {
		return policy.DataLabel{}, nil, ErrRuntimeInformationFlow
	}
	dest := policy.FlowDestination{WorkspaceID: input.WorkspaceID, Kind: policy.DestinationKind(dp.DestinationKind), Clearance: clearance}
	if dest.Kind == policy.DestinationOriginNode {
		if c.NodeID == nil || *c.NodeID != s.localNodeID {
			return policy.DataLabel{}, nil, ErrRuntimeInformationFlow
		}
		dest.NodeID = *c.NodeID
	}
	output, ok := policy.InheritLabels(input)
	if !ok {
		return policy.DataLabel{}, nil, ErrRuntimeInformationFlow
	}
	flow := policy.EvaluateInformationFlow(policy.InformationFlowInput{Source: input, Destination: dest, ProposedDerivedLabel: &output})
	if !flow.Allowed {
		return policy.DataLabel{}, nil, fmt.Errorf("%w: %v", ErrRuntimeInformationFlow, flow.Reasons)
	}
	transport, ok := s.transports.Resolve(c.AdapterName, c.AdapterVersion)
	if !ok {
		return policy.DataLabel{}, nil, ErrAdapterUnavailable
	}
	return flow.EffectiveLabel, transport, nil
}

func runtimePolicyAllows(dp DataPolicy, conf, res string) bool {
	rank := func(v string) int {
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
	if rank(conf) < 0 || rank(dp.MaxConfidentiality) < rank(conf) {
		return false
	}
	for _, r := range dp.AllowedResidency {
		if r == strings.ToLower(res) {
			return true
		}
	}
	return false
}
func runtimeClearance(v string) policy.Confidentiality {
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

func (s *Service) transitionInvocation(ctx context.Context, id string, to InvocationStatus, responseArtifact, errorCode *string, completedAt *int64, cmd InvokeCommand, eventType string, extra map[string]any) error {
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		current, err := s.invocations.GetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.invocations.Transition(ctx, tx, current, to, responseArtifact, errorCode, completedAt, s.clock.UnixMilli()); err != nil {
			return err
		}
		evt, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload := map[string]any{"invocation_id": id, "from": current.Status, "to": to, "revision": current.Revision + 1}
		for k, v := range extra {
			payload[k] = v
		}
		if errorCode != nil {
			payload["error_code"] = *errorCode
		}
		b, _ := json.Marshal(payload)
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &cmd.WorkspaceID, Type: eventType, AggregateType: "agent_runtime_invocation", AggregateID: id, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: b, OccurredAt: s.clock.UnixMilli()})
	})
}

func (s *Service) finishInvocationError(ctx context.Context, id string, from InvocationStatus, cause error, cmd InvokeCommand) (InvokeResult, error) {
	status := InvocationFailed
	code := "runtime_denied"
	var te *RuntimeTransportError
	if errors.As(cause, &te) {
		if te.Code != "" {
			code = te.Code
		}
		if !te.OutcomeKnown {
			status = InvocationUnknown
		}
	} else {
		switch {
		case errors.Is(cause, ErrRuntimeInformationFlow):
			code = "information_flow_denied"
		case errors.Is(cause, ErrRuntimeNotSchedulable):
			code = "runtime_not_schedulable"
		case errors.Is(cause, ErrTrustedNodeTransportUnavailable):
			code = "trusted_node_transport_unavailable"
		case errors.Is(cause, ErrAdapterUnavailable):
			code = "adapter_unavailable"
		case errors.Is(cause, ErrRuntimeBudgetRequired):
			code = "budget_required"
		case errors.Is(cause, ErrRuntimeBudgetUnavailable):
			code = "budget_unavailable"
		}
	}
	completed := s.clock.UnixMilli()
	if err := s.transitionInvocation(ctx, id, status, nil, &code, &completed, cmd, "agent_runtime_invocation."+string(status), nil); err != nil {
		return InvokeResult{}, err
	}
	stored, err := s.invocations.Get(ctx, id)
	if err != nil {
		return InvokeResult{}, err
	}
	return InvokeResult{Invocation: stored}, cause
}
