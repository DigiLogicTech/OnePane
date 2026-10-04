package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type policyEvaluator interface {
	EvaluateAuthority(context.Context, policy.AuthorityInput) policy.Decision
}

type authorityConsumer interface {
	Consume(context.Context, authority.ConsumeCommand) (authority.Lease, error)
}

type Gateway struct {
	store           invocationStore
	policy          policyEvaluator
	authority       authorityConsumer
	registry        *Registry
	mutationPermits MutationPermitValidator
	sandboxAdapters map[string]struct{}
	ids             id.Generator
	clock           clock.Clock
}

func NewGateway(db *sql.DB, tx storage.Transactor, p policyEvaluator, a authorityConsumer, registry *Registry, clk clock.Clock) *Gateway {
	return &Gateway{store: newSQLInvocationStore(db, tx), policy: p, authority: a, registry: registry, sandboxAdapters: map[string]struct{}{}, ids: id.Generator{}, clock: clk}
}

func newGateway(store invocationStore, p policyEvaluator, a authorityConsumer, registry *Registry, clk clock.Clock) *Gateway {
	return &Gateway{store: store, policy: p, authority: a, registry: registry, sandboxAdapters: map[string]struct{}{}, ids: id.Generator{}, clock: clk}
}

func (g *Gateway) EnableSandboxAdapter(adapterID, adapterVersion string) {
	if g == nil || strings.TrimSpace(adapterID) == "" || strings.TrimSpace(adapterVersion) == "" {
		return
	}
	if g.sandboxAdapters == nil {
		g.sandboxAdapters = map[string]struct{}{}
	}
	g.sandboxAdapters[strings.TrimSpace(adapterID)+"@"+strings.TrimSpace(adapterVersion)] = struct{}{}
}

func (g *Gateway) SetMutationPermitValidator(v MutationPermitValidator) {
	if g != nil {
		g.mutationPermits = v
	}
}

func (g *Gateway) Definition(toolID, version string) (Definition, error) {
	if g == nil || g.registry == nil {
		return Definition{}, ErrToolNotFound
	}
	b, err := g.registry.Resolve(strings.TrimSpace(toolID), strings.TrimSpace(version))
	if err != nil {
		return Definition{}, err
	}
	return b.Definition, nil
}

func (g *Gateway) Get(ctx context.Context, invocationID string) (Invocation, error) {
	if g == nil || g.store == nil || strings.TrimSpace(invocationID) == "" {
		return Invocation{}, fmt.Errorf("%w: invocation id is required", ErrInvalidCommand)
	}
	return g.store.Get(ctx, invocationID)
}

func (g *Gateway) Invoke(ctx context.Context, cmd InvokeCommand) (Invocation, error) {
	if g == nil || g.store == nil || g.policy == nil || g.authority == nil || g.registry == nil || g.clock == nil {
		return Invocation{}, fmt.Errorf("%w: gateway dependencies are unavailable", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" ||
		strings.TrimSpace(cmd.LeaseID) == "" || strings.TrimSpace(cmd.ToolID) == "" ||
		strings.TrimSpace(cmd.ToolVersion) == "" || strings.TrimSpace(cmd.ResourceRef) == "" {
		return Invocation{}, fmt.Errorf("%w: workspace, principal, lease, tool/version and resource are required", ErrInvalidCommand)
	}
	canonicalInput, err := canonicalJSON(cmd.Input)
	if err != nil {
		return Invocation{}, fmt.Errorf("%w: input must be one valid JSON value: %v", ErrInvalidCommand, err)
	}
	binding, err := g.registry.Resolve(strings.TrimSpace(cmd.ToolID), strings.TrimSpace(cmd.ToolVersion))
	if err != nil {
		return Invocation{}, err
	}

	invocationID, err := g.ids.New("toolinv")
	if err != nil {
		return Invocation{}, err
	}
	now := g.clock.UnixMilli()
	resource := strings.TrimSpace(cmd.ResourceRef)
	actor := cmd.ActorPrincipalID
	if actor == nil {
		actor = &cmd.PrincipalID
	}
	meta := EventMeta{ActorPrincipalID: actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID}
	inv := Invocation{
		ID: invocationID, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, AttemptID: cmd.AttemptID,
		PrincipalID: cmd.PrincipalID, OperationID: cmd.OperationID,
		ToolID: binding.Definition.ID, ToolVersion: binding.Definition.Version,
		AdapterID: binding.Definition.AdapterID, AdapterVersion: binding.Definition.AdapterVersion,
		Mode: binding.Definition.Mode, ResourceRef: &resource, InputHash: inputHash(canonicalInput),
		Status: StatusCreated, CreatedAt: now, UpdatedAt: now,
	}
	if err := g.store.Create(ctx, inv, meta); err != nil {
		return Invocation{}, err
	}

	taskID := ""
	if cmd.TaskID != nil {
		taskID = *cmd.TaskID
	}
	decision := g.policy.EvaluateAuthority(ctx, policy.AuthorityInput{
		LeaseID: cmd.LeaseID, WorkspaceID: cmd.WorkspaceID, PrincipalID: cmd.PrincipalID,
		TaskID: taskID, CapabilityID: binding.Definition.CapabilityID, Action: binding.Definition.Mode,
		ResourceRef: resource, Risk: binding.Definition.Risk,
		MinimumVerification: binding.Definition.MinimumVerification, MinimumApproval: binding.Definition.MinimumApproval,
	})
	if !decision.Allowed {
		reasons := make([]string, 0, len(decision.Reasons))
		for _, r := range decision.Reasons {
			reasons = append(reasons, string(r))
		}
		return g.fail(ctx, inv, meta, "policy_denied", "policy denied: "+strings.Join(reasons, ","), ErrPolicyDenied)
	}

	if err := g.store.ValidateExecutionContext(ctx, cmd.WorkspaceID, cmd.PrincipalID, cmd.TaskID, cmd.AttemptID); err != nil {
		return g.fail(ctx, inv, meta, "execution_context_invalid", err.Error(), fmt.Errorf("%w: %v", ErrExecutionContext, err))
	}
	if decision.RequiredApproval != policy.ApprovalNone {
		return g.fail(ctx, inv, meta, "approval_required", "required approval: "+string(decision.RequiredApproval), ErrApprovalRequired)
	}
	switch binding.Definition.Mode {
	case authority.ActionMutate, authority.ActionExternalSend:
		if g.mutationPermits == nil || cmd.OperationID == nil || strings.TrimSpace(*cmd.OperationID) == "" || strings.TrimSpace(cmd.ExecutionPermit) == "" {
			return g.fail(ctx, inv, meta, "operation_coordinator_required", "mutating/external-send tools require an OperationCoordinator execution permit", ErrOperationCoordinatorRequired)
		}
		if err := g.mutationPermits.ValidateAndConsume(ctx, MutationPermitCheck{
			OperationID: *cmd.OperationID, Permit: cmd.ExecutionPermit, WorkspaceID: cmd.WorkspaceID,
			TaskID: cmd.TaskID, AttemptID: cmd.AttemptID, PrincipalID: cmd.PrincipalID, LeaseID: cmd.LeaseID,
			ToolID: binding.Definition.ID, ToolVersion: binding.Definition.Version,
			AdapterID: binding.Definition.AdapterID, AdapterVersion: binding.Definition.AdapterVersion,
			ResourceRef: resource, InputHash: inv.InputHash,
		}); err != nil {
			return g.fail(ctx, inv, meta, "execution_permit_invalid", err.Error(), ErrOperationCoordinatorRequired)
		}
	case authority.ActionExecuteSandboxed:
		key := binding.Definition.AdapterID + "@" + binding.Definition.AdapterVersion
		if _, ok := g.sandboxAdapters[key]; !ok {
			return g.fail(ctx, inv, meta, "sandbox_runner_required", "sandboxed execution requires an explicitly trusted sandbox boundary adapter", ErrSandboxRunnerRequired)
		}
	}

	if _, err := g.authority.Consume(ctx, authority.ConsumeCommand{
		LeaseID: cmd.LeaseID, ExpectedRevision: decision.LeaseRevision, Uses: 1,
		ActorPrincipalID: actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	}); err != nil {
		return g.fail(ctx, inv, meta, "authority_consume_failed", err.Error(), fmt.Errorf("%w: %v", ErrAuthorityConsumption, err))
	}

	if err := g.transitionDetails(ctx, &inv, meta, StatusAuthorized, nil, nil, nil, map[string]any{
		"lease_id": cmd.LeaseID, "lease_revision": decision.LeaseRevision,
		"policy_revision":       decision.PolicyRevision,
		"required_verification": decision.RequiredVerification, "required_approval": decision.RequiredApproval,
	}); err != nil {
		return inv, err
	}
	if err := g.transition(ctx, &inv, meta, StatusRunning, nil, nil, nil); err != nil {
		return inv, err
	}

	result, callErr := callAdapter(ctx, binding.Adapter, AdapterRequest{
		InvocationID: inv.ID, WorkspaceID: inv.WorkspaceID, TaskID: inv.TaskID, AttemptID: inv.AttemptID,
		ToolID: inv.ToolID, ToolVersion: inv.ToolVersion, ResourceRef: resource, Input: canonicalInput,
	})
	if callErr != nil {
		status, code := StatusFailed, "adapter_failed"
		if errors.Is(callErr, context.DeadlineExceeded) {
			status, code = StatusTimedOut, "deadline_exceeded"
		}
		if errors.Is(callErr, context.Canceled) {
			status, code = StatusCancelled, "cancelled"
		}
		summary := boundedSummary(callErr.Error())
		if err := g.transition(ctx, &inv, meta, status, &summary, nil, &code); err != nil {
			return inv, err
		}
		final, _ := g.store.Get(ctx, inv.ID)
		var known KnownAdapterFailure
		unknown := !errors.As(callErr, &known)
		return final, &AdapterExecutionError{Cause: callErr, UnknownOutcome: unknown}
	}

	resultJSON := result.Result
	if len(resultJSON) == 0 {
		resultJSON = json.RawMessage(`{}`)
	}
	canonicalResult, err := canonicalJSON(resultJSON)
	if err != nil {
		return g.failFrom(ctx, inv, meta, StatusRunning, "adapter_invalid_result", err.Error(), ErrAdapterInvalidResult)
	}
	summary := boundedSummary(result.Summary)
	if err := g.transition(ctx, &inv, meta, StatusSucceeded, &summary, canonicalResult, nil); err != nil {
		return inv, err
	}
	return g.store.Get(ctx, inv.ID)
}

func (g *Gateway) transition(ctx context.Context, inv *Invocation, meta EventMeta, to Status, summary *string, result json.RawMessage, code *string) error {
	return g.transitionDetails(ctx, inv, meta, to, summary, result, code, nil)
}

func (g *Gateway) transitionDetails(ctx context.Context, inv *Invocation, meta EventMeta, to Status, summary *string, result json.RawMessage, code *string, details map[string]any) error {
	tr := Transition{InvocationID: inv.ID, From: inv.Status, To: to, Summary: summary, Result: result, ErrorCode: code, At: g.clock.UnixMilli(), Details: details}
	if err := g.store.Transition(ctx, tr, meta); err != nil {
		return err
	}
	inv.Status, inv.UpdatedAt = to, tr.At
	if to == StatusRunning && inv.StartedAt == nil {
		v := tr.At
		inv.StartedAt = &v
	}
	if to == StatusSucceeded || to == StatusFailed || to == StatusTimedOut || to == StatusCancelled || to == StatusInterrupted {
		v := tr.At
		inv.EndedAt = &v
	}
	return nil
}

func (g *Gateway) fail(ctx context.Context, inv Invocation, meta EventMeta, code, summary string, cause error) (Invocation, error) {
	return g.failFrom(ctx, inv, meta, StatusCreated, code, summary, cause)
}

func (g *Gateway) failFrom(ctx context.Context, inv Invocation, meta EventMeta, from Status, code, summary string, cause error) (Invocation, error) {
	inv.Status = from
	summary = boundedSummary(summary)
	if err := g.transition(ctx, &inv, meta, StatusFailed, &summary, nil, &code); err != nil {
		return inv, err
	}
	final, err := g.store.Get(ctx, inv.ID)
	if err != nil {
		return inv, err
	}
	return final, cause
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func inputHash(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boundedSummary(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 2048 {
		return string(r[:2048])
	}
	return s
}

func callAdapter(ctx context.Context, adapter Adapter, req AdapterRequest) (res AdapterResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("adapter panic: %v", r)
		}
	}()
	return adapter.Invoke(ctx, req)
}
