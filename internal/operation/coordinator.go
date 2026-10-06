package operation

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
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/resourcecoord"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type policyEvaluator interface {
	EvaluateAuthority(context.Context, policy.AuthorityInput) policy.Decision
}
type mutationToolGateway interface {
	Definition(string, string) (tool.Definition, error)
	Invoke(context.Context, tool.InvokeCommand) (tool.Invocation, error)
}
type eventAppender interface {
	Append(context.Context, storage.Tx, event.Event) error
}
type approvalConsumer interface {
	ConsumeTx(context.Context, storage.Tx, string, string) error
}

type Coordinator struct {
	tx              storage.Transactor
	repo            repository
	events          eventAppender
	policy          policyEvaluator
	tools           mutationToolGateway
	gate            *MutationGate
	ids             id.Generator
	clock           clock.Clock
	approvals       approvalConsumer
	globalResources *resourcecoord.Service
}

func NewCoordinator(db *sql.DB, tx storage.Transactor, p policyEvaluator, tools mutationToolGateway, gate *MutationGate, clk clock.Clock) *Coordinator {
	return &Coordinator{tx: tx, repo: newSQLRepository(db), events: event.Store{}, policy: p, tools: tools, gate: gate, ids: id.Generator{}, clock: clk}
}
func (c *Coordinator) SetApprovalConsumer(a approvalConsumer)                { c.approvals = a }
func (c *Coordinator) SetGlobalResourceCoordinator(v *resourcecoord.Service) { c.globalResources = v }

func (c *Coordinator) Get(ctx context.Context, id string) (Operation, error) {
	if c == nil || strings.TrimSpace(id) == "" {
		return Operation{}, ErrInvalidCommand
	}
	return c.repo.Get(ctx, id)
}

func (c *Coordinator) Prepare(ctx context.Context, cmd PrepareCommand) (Operation, error) {
	if c == nil || c.tx == nil || c.repo == nil || c.events == nil || c.policy == nil || c.tools == nil || c.gate == nil || c.clock == nil {
		return Operation{}, fmt.Errorf("%w: coordinator dependencies unavailable", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || strings.TrimSpace(cmd.CapabilityLeaseID) == "" ||
		strings.TrimSpace(cmd.IdempotencyKey) == "" || strings.TrimSpace(cmd.ToolID) == "" || strings.TrimSpace(cmd.ToolVersion) == "" || strings.TrimSpace(cmd.ResourceRef) == "" {
		return Operation{}, ErrInvalidCommand
	}
	if cmd.CompensatesOperationID != nil {
		original, e := c.repo.Get(ctx, *cmd.CompensatesOperationID)
		if e != nil {
			return Operation{}, e
		}
		if original.WorkspaceID != cmd.WorkspaceID || original.State != StateCommitted || len(original.Compensation) == 0 {
			return Operation{}, ErrInvalidCommand
		}
	}
	input, err := canonicalJSON(cmd.Input)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: input: %v", ErrInvalidCommand, err)
	}
	desired, err := canonicalOrDefault(cmd.DesiredState)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: desired state: %v", ErrInvalidCommand, err)
	}
	pre, err := canonicalOrDefault(cmd.Precondition)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: precondition: %v", ErrInvalidCommand, err)
	}
	recon, err := canonicalOrDefault(cmd.Reconciliation)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: reconciliation: %v", ErrInvalidCommand, err)
	}
	var comp json.RawMessage
	if len(cmd.Compensation) > 0 {
		comp, err = canonicalJSON(cmd.Compensation)
		if err != nil {
			return Operation{}, fmt.Errorf("%w: compensation: %v", ErrInvalidCommand, err)
		}
	}
	def, err := c.tools.Definition(cmd.ToolID, cmd.ToolVersion)
	if err != nil {
		return Operation{}, err
	}
	if def.Mode != authority.ActionMutate && def.Mode != authority.ActionExternalSend {
		return Operation{}, ErrMutationToolRequired
	}

	inputHash := hashJSON(input)
	if existing, e := c.repo.ByIdempotency(ctx, cmd.WorkspaceID, cmd.IdempotencyKey); e == nil {
		if !sameIntent(existing, cmd, inputHash) {
			return existing, ErrIdempotencyConflict
		}
		return existing, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return Operation{}, fmt.Errorf("lookup idempotency key: %w", e)
	}

	opID, err := c.ids.New("op")
	if err != nil {
		return Operation{}, err
	}
	evtID, err := c.ids.New("evt")
	if err != nil {
		return Operation{}, err
	}
	now := c.clock.UnixMilli()
	o := Operation{ID: opID, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, AttemptID: cmd.AttemptID, PrincipalID: cmd.PrincipalID, CompensatesOperationID: cmd.CompensatesOperationID,
		IdempotencyKey: cmd.IdempotencyKey, State: StateProposed, CapabilityID: def.CapabilityID, ResourceRef: strings.TrimSpace(cmd.ResourceRef),
		ToolID: def.ID, ToolVersion: def.Version, AdapterID: def.AdapterID, AdapterVersion: def.AdapterVersion, DesiredState: desired, Precondition: pre,
		Reconciliation: recon, Compensation: comp, PolicyRevision: policy.BuiltinPolicyRevision, InputHash: inputHash, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := c.repo.Insert(ctx, tx, o); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"operation_id": o.ID, "task_id": o.TaskID, "attempt_id": o.AttemptID, "state": o.State, "idempotency_key": o.IdempotencyKey, "capability_id": o.CapabilityID, "resource_ref": o.ResourceRef, "tool_id": o.ToolID, "tool_version": o.ToolVersion, "input_hash": o.InputHash})
		return c.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &o.WorkspaceID, Type: "operation.proposed", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Operation{}, err
	}

	taskID := ""
	if o.TaskID != nil {
		taskID = *o.TaskID
	}
	decision := c.policy.EvaluateAuthority(ctx, policy.AuthorityInput{LeaseID: cmd.CapabilityLeaseID, WorkspaceID: o.WorkspaceID, PrincipalID: o.PrincipalID, TaskID: taskID,
		CapabilityID: o.CapabilityID, Action: def.Mode, ResourceRef: o.ResourceRef, Risk: def.Risk, MinimumVerification: def.MinimumVerification, MinimumApproval: def.MinimumApproval})
	if !decision.Allowed {
		_ = c.transitionWithDetails(ctx, o.ID, o.Revision, StateProposed, StateDenied, cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID, map[string]any{"reasons": decision.Reasons})
		denied, _ := c.repo.Get(ctx, o.ID)
		return denied, ErrPolicyDenied
	}
	authEvt, _ := c.ids.New("evt")
	now = c.clock.UnixMilli()
	err = c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := c.repo.SetAuthorization(ctx, tx, o.ID, o.Revision, decision.PolicyRevision, cmd.CapabilityLeaseID, decision.LeaseRevision, string(decision.RequiredVerification), string(decision.RequiredApproval), now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": StateProposed, "to": StateAuthorized, "lease_id": cmd.CapabilityLeaseID, "lease_revision": decision.LeaseRevision, "policy_revision": decision.PolicyRevision, "required_verification": decision.RequiredVerification, "required_approval": decision.RequiredApproval})
		return c.events.Append(ctx, tx, event.Event{ID: authEvt, WorkspaceID: &o.WorkspaceID, Type: "operation.authorized", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Operation{}, err
	}
	o, err = c.repo.Get(ctx, o.ID)
	if err != nil {
		return Operation{}, err
	}
	if decision.RequiredApproval != policy.ApprovalNone {
		return o, ErrApprovalRequired
	}

	ttl := cmd.ResourceLeaseTTLMillis
	if ttl == 0 {
		ttl = 5 * 60 * 1000
	}
	if ttl < 10_000 || ttl > 60*60*1000 {
		return o, fmt.Errorf("%w: resource lease ttl must be 10s..1h", ErrInvalidCommand)
	}
	leaseID, err := c.ids.New("rlease")
	if err != nil {
		return o, err
	}
	prepEvt, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	now = c.clock.UnixMilli()
	rl := ResourceLease{ID: leaseID, WorkspaceID: o.WorkspaceID, TaskID: o.TaskID, ResourceRef: o.ResourceRef, LeaseMode: "exclusive_mutation", Status: "active", AcquiredAt: now, ExpiresAt: now + ttl, Revision: 1}
	var globalLease *resourcecoord.Lease
	if c.globalResources != nil {
		gl, e := c.globalResources.AcquireWrite(ctx, o.ResourceRef, resourcecoord.Owner{WorkspaceID: o.WorkspaceID, TaskID: o.TaskID}, time.Duration(ttl)*time.Millisecond)
		if e != nil {
			return o, e
		}
		globalLease = &gl
	}
	releaseGlobalOnError := true
	defer func() {
		if releaseGlobalOnError && globalLease != nil {
			_ = c.globalResources.Release(context.Background(), globalLease.ID)
		}
	}()
	err = c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if active, e := c.repo.ActiveResourceLeaseForResource(ctx, tx, o.WorkspaceID, o.ResourceRef); e != nil {
			return e
		} else if active != nil {
			return ErrResourceBusy
		}
		if err := c.repo.AcquireResourceLease(ctx, tx, rl); err != nil {
			return err
		}
		var globalLeaseID *string
		if globalLease != nil {
			globalLeaseID = &globalLease.ID
		}
		if err := c.repo.SetResourceLease(ctx, tx, o.ID, o.Revision, rl.ID, globalLeaseID, now); err != nil {
			return err
		}
		lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "resource_ref": rl.ResourceRef, "expires_at": rl.ExpiresAt, "operation_id": o.ID})
		if err := c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &o.WorkspaceID, Type: "resource_lease.acquired", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: lp, OccurredAt: now}); err != nil {
			return err
		}
		pp, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": StateAuthorized, "to": StatePrepared, "resource_lease_id": rl.ID})
		return c.events.Append(ctx, tx, event.Event{ID: prepEvt, WorkspaceID: &o.WorkspaceID, Type: "operation.prepared", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: pp, OccurredAt: now})
	})
	if err != nil {
		return o, err
	}
	releaseGlobalOnError = false
	return c.repo.Get(ctx, o.ID)
}

type compensationSpec struct {
	ToolID         string          `json:"tool_id"`
	ToolVersion    string          `json:"tool_version"`
	ResourceRef    string          `json:"resource_ref"`
	Input          json.RawMessage `json:"input"`
	DesiredState   json.RawMessage `json:"desired_state"`
	Precondition   json.RawMessage `json:"precondition"`
	Reconciliation json.RawMessage `json:"reconciliation"`
}

// Compensate creates a new, fully governed Operation linked to a previously
// committed Operation. It does not execute privileged rollback code directly.
func (c *Coordinator) Compensate(ctx context.Context, cmd CompensateCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OriginalOperationID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || strings.TrimSpace(cmd.CapabilityLeaseID) == "" || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return Operation{}, ErrInvalidCommand
	}
	original, err := c.repo.Get(ctx, cmd.OriginalOperationID)
	if err != nil {
		return Operation{}, err
	}
	if original.State != StateCommitted || len(original.Compensation) == 0 {
		return Operation{}, ErrInvalidTransition
	}
	var spec compensationSpec
	if err := json.Unmarshal(original.Compensation, &spec); err != nil {
		return Operation{}, fmt.Errorf("%w: invalid stored compensation: %v", ErrInvalidCommand, err)
	}
	if strings.TrimSpace(spec.ToolID) == "" || strings.TrimSpace(spec.ToolVersion) == "" || strings.TrimSpace(spec.ResourceRef) == "" || len(spec.Input) == 0 {
		return Operation{}, ErrInvalidCommand
	}
	if spec.ResourceRef != original.ResourceRef {
		return Operation{}, fmt.Errorf("%w: compensation must target original resource", ErrInvalidCommand)
	}
	return c.Prepare(ctx, PrepareCommand{WorkspaceID: original.WorkspaceID, TaskID: original.TaskID, AttemptID: original.AttemptID, PrincipalID: cmd.PrincipalID, CapabilityLeaseID: cmd.CapabilityLeaseID, IdempotencyKey: "compensate:" + original.ID + ":" + cmd.IdempotencyKey, ToolID: spec.ToolID, ToolVersion: spec.ToolVersion, ResourceRef: spec.ResourceRef, Input: spec.Input, DesiredState: spec.DesiredState, Precondition: spec.Precondition, Reconciliation: spec.Reconciliation, CompensatesOperationID: &original.ID, ResourceLeaseTTLMillis: cmd.ResourceLeaseTTLMillis, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
}

func (c *Coordinator) ContinueApproved(ctx context.Context, cmd ContinueApprovedCommand) (Operation, error) {
	if c == nil || c.approvals == nil || strings.TrimSpace(cmd.OperationID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ApprovalID) == "" {
		return Operation{}, ErrInvalidCommand
	}
	ttl := cmd.ResourceLeaseTTLMillis
	if ttl == 0 {
		ttl = 5 * 60 * 1000
	}
	if ttl < 10_000 || ttl > 60*60*1000 {
		return Operation{}, fmt.Errorf("%w: resource lease ttl must be 10s..1h", ErrInvalidCommand)
	}
	leaseID, err := c.ids.New("rlease")
	if err != nil {
		return Operation{}, err
	}
	prepEvt, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	now := c.clock.UnixMilli()
	preOp, preErr := c.repo.Get(ctx, cmd.OperationID)
	if preErr != nil {
		return Operation{}, preErr
	}
	var globalLease *resourcecoord.Lease
	if c.globalResources != nil {
		gl, e := c.globalResources.AcquireWrite(ctx, preOp.ResourceRef, resourcecoord.Owner{WorkspaceID: preOp.WorkspaceID, TaskID: preOp.TaskID}, time.Duration(ttl)*time.Millisecond)
		if e != nil {
			return Operation{}, e
		}
		globalLease = &gl
	}
	releaseGlobalOnError := true
	defer func() {
		if releaseGlobalOnError && globalLease != nil {
			_ = c.globalResources.Release(context.Background(), globalLease.ID)
		}
	}()
	err = c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		o, e := c.repo.GetTx(ctx, tx, cmd.OperationID)
		if e != nil {
			return e
		}
		if o.Revision != cmd.ExpectedRevision || o.State != StateAuthorized {
			return ErrRevisionConflict
		}
		if o.RequiredApproval == nil || *o.RequiredApproval == policy.ApprovalNone {
			return ErrInvalidCommand
		}
		if e := c.approvals.ConsumeTx(ctx, tx, cmd.ApprovalID, o.ID); e != nil {
			return e
		}
		if active, e := c.repo.ActiveResourceLeaseForResource(ctx, tx, o.WorkspaceID, o.ResourceRef); e != nil {
			return e
		} else if active != nil {
			return ErrResourceBusy
		}
		rl := ResourceLease{ID: leaseID, WorkspaceID: o.WorkspaceID, TaskID: o.TaskID, ResourceRef: o.ResourceRef, LeaseMode: "exclusive_mutation", Status: "active", AcquiredAt: now, ExpiresAt: now + ttl, Revision: 1}
		if e := c.repo.AcquireResourceLease(ctx, tx, rl); e != nil {
			return e
		}
		var globalLeaseID *string
		if globalLease != nil {
			globalLeaseID = &globalLease.ID
		}
		if e := c.repo.SetResourceLease(ctx, tx, o.ID, o.Revision, rl.ID, globalLeaseID, now); e != nil {
			return e
		}
		lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "resource_ref": rl.ResourceRef, "expires_at": rl.ExpiresAt, "operation_id": o.ID, "approval_id": cmd.ApprovalID})
		if e := c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &o.WorkspaceID, Type: "resource_lease.acquired", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: lp, OccurredAt: now}); e != nil {
			return e
		}
		pp, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": StateAuthorized, "to": StatePrepared, "resource_lease_id": rl.ID, "approval_id": cmd.ApprovalID})
		return c.events.Append(ctx, tx, event.Event{ID: prepEvt, WorkspaceID: &o.WorkspaceID, Type: "operation.prepared", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: pp, OccurredAt: now})
	})
	if err != nil {
		return Operation{}, err
	}
	releaseGlobalOnError = false
	return c.repo.Get(ctx, cmd.OperationID)
}

func (c *Coordinator) Execute(ctx context.Context, cmd ExecuteCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.LeaseID) == "" {
		return Operation{}, ErrInvalidCommand
	}
	input, err := canonicalJSON(cmd.Input)
	if err != nil {
		return Operation{}, ErrInvalidCommand
	}
	inputHash := hashJSON(input)
	o, err := c.repo.Get(ctx, cmd.OperationID)
	if err != nil {
		return Operation{}, err
	}
	if o.Revision != cmd.ExpectedRevision {
		return o, ErrRevisionConflict
	}
	if o.State != StatePrepared {
		return o, ErrInvalidTransition
	}
	if o.CapabilityLeaseID == nil || *o.CapabilityLeaseID != cmd.LeaseID || o.InputHash != inputHash {
		return o, ErrInvalidCommand
	}
	if err := c.transitionWithDetails(ctx, o.ID, o.Revision, StatePrepared, StateExecuting, cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID, nil); err != nil {
		return o, err
	}
	o, err = c.repo.Get(ctx, o.ID)
	if err != nil {
		return Operation{}, err
	}
	permit, err := c.gate.Mint(ctx, o.ID)
	if err != nil {
		_ = c.finishKnownFailure(ctx, o, "execution_permit_failed", cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID)
		final, _ := c.repo.Get(ctx, o.ID)
		return final, err
	}
	inv, callErr := c.tools.Invoke(ctx, tool.InvokeCommand{WorkspaceID: o.WorkspaceID, TaskID: o.TaskID, AttemptID: o.AttemptID, PrincipalID: o.PrincipalID, LeaseID: cmd.LeaseID, OperationID: &o.ID, ToolID: o.ToolID, ToolVersion: o.ToolVersion, ResourceRef: o.ResourceRef, Input: input, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, ExecutionPermit: permit})
	if callErr != nil {
		if inv.StartedAt != nil {
			_ = c.transitionWithDetails(ctx, o.ID, o.Revision, StateExecuting, StateUnknownOutcome, cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID, map[string]any{"tool_invocation_id": inv.ID, "error": callErr.Error()})
			final, _ := c.repo.Get(ctx, o.ID)
			return final, fmt.Errorf("%w: %v", ErrUnknownOutcome, callErr)
		}
		_ = c.finishKnownFailure(ctx, o, "tool_pre_dispatch_failed", cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID)
		final, _ := c.repo.Get(ctx, o.ID)
		return final, callErr
	}
	if inv.Status != tool.StatusSucceeded {
		return o, ErrUnknownOutcome
	}
	receiptJSON, _ := json.Marshal(map[string]any{"tool_invocation_id": inv.ID, "summary": inv.Summary, "result": json.RawMessage(inv.Result)})
	rcID, _ := c.ids.New("receipt")
	receiptHash := hashJSON(receiptJSON)
	now := c.clock.UnixMilli()
	evtID, _ := c.ids.New("evt")
	err = c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		current, e := c.repo.GetTx(ctx, tx, o.ID)
		if e != nil {
			return e
		}
		if current.Revision != o.Revision || current.State != StateExecuting {
			return ErrRevisionConflict
		}
		if e := c.repo.InsertReceipt(ctx, tx, Receipt{ID: rcID, OperationID: o.ID, ReceiptType: "tool_result", Receipt: receiptJSON, IntegrityHash: receiptHash, CreatedAt: now}); e != nil {
			return e
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision, StateExecuting, StateObserving, now); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"operation_id": o.ID, "task_id": o.TaskID, "attempt_id": o.AttemptID, "resource_ref": o.ResourceRef, "tool_id": o.ToolID, "tool_version": o.ToolVersion, "from": StateExecuting, "to": StateObserving, "tool_invocation_id": inv.ID, "receipt_id": rcID, "receipt_hash": receiptHash})
		return c.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &o.WorkspaceID, Type: "operation.observing", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return o, err
	}
	return c.repo.Get(ctx, o.ID)
}

func (c *Coordinator) CommitVerified(ctx context.Context, cmd CommitCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || cmd.ExpectedRevision < 1 {
		return Operation{}, ErrInvalidCommand
	}
	now := c.clock.UnixMilli()
	verifiedEvt, _ := c.ids.New("evt")
	committedEvt, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	var oGlobalLeaseID *string
	err := c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		o, e := c.repo.GetTx(ctx, tx, cmd.OperationID)
		if e != nil {
			return e
		}
		oGlobalLeaseID = o.GlobalResourceLeaseID
		if o.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if o.State != StateObserving {
			return ErrInvalidTransition
		}
		v, e := c.repo.VerificationForOperation(ctx, tx, o.ID, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.Status != "pass" || v.AchievedLevel == nil {
			return ErrVerificationRequired
		}
		required := policy.VerificationV2
		if o.RequiredVerification != nil {
			required = *o.RequiredVerification
		}
		achieved := policy.VerificationLevel(*v.AchievedLevel)
		if verification.LevelRank(achieved) < verification.LevelRank(required) {
			return ErrVerificationInsufficient
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision, StateObserving, StateVerified, now); e != nil {
			return e
		}
		vp, _ := json.Marshal(map[string]any{"operation_id": o.ID, "task_id": o.TaskID, "attempt_id": o.AttemptID, "resource_ref": o.ResourceRef, "tool_id": o.ToolID, "from": StateObserving, "to": StateVerified, "verification_id": v.ID, "required_level": required, "achieved_level": achieved})
		if e := c.events.Append(ctx, tx, event.Event{ID: verifiedEvt, WorkspaceID: &o.WorkspaceID, Type: "operation.verified", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: vp, OccurredAt: now}); e != nil {
			return e
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision+1, StateVerified, StateCommitted, now); e != nil {
			return e
		}
		cp, _ := json.Marshal(map[string]any{"operation_id": o.ID, "task_id": o.TaskID, "attempt_id": o.AttemptID, "resource_ref": o.ResourceRef, "tool_id": o.ToolID, "from": StateVerified, "to": StateCommitted, "verification_id": v.ID})
		if e := c.events.Append(ctx, tx, event.Event{ID: committedEvt, WorkspaceID: &o.WorkspaceID, Type: "operation.committed", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: cp, OccurredAt: now}); e != nil {
			return e
		}
		if o.ResourceLeaseID != nil {
			rl, e := c.repo.ResourceLeaseTx(ctx, tx, *o.ResourceLeaseID)
			if e != nil {
				return e
			}
			if rl.Status == "active" {
				if e := c.repo.ReleaseResourceLease(ctx, tx, rl.ID, rl.Revision, now); e != nil {
					return e
				}
				lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "operation_id": o.ID, "reason": "operation_committed"})
				if e := c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &o.WorkspaceID, Type: "resource_lease.released", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: lp, OccurredAt: now}); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		return Operation{}, err
	}
	c.releaseGlobalResourceLease(ctx, Operation{GlobalResourceLeaseID: oGlobalLeaseID})
	return c.repo.Get(ctx, cmd.OperationID)
}

func (c *Coordinator) Abort(ctx context.Context, cmd AbortCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || cmd.ExpectedRevision < 1 {
		return Operation{}, ErrInvalidCommand
	}
	now := c.clock.UnixMilli()
	evtID, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	var oGlobalLeaseID *string
	err := c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		o, e := c.repo.GetTx(ctx, tx, cmd.OperationID)
		if e != nil {
			return e
		}
		oGlobalLeaseID = o.GlobalResourceLeaseID
		if o.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if o.State != StateProposed && o.State != StateAuthorized && o.State != StatePrepared {
			return ErrInvalidTransition
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision, o.State, StateAborted, now); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": o.State, "to": StateAborted, "reason": strings.TrimSpace(cmd.Reason)})
		if e := c.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &o.WorkspaceID, Type: "operation.aborted", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); e != nil {
			return e
		}
		if o.ResourceLeaseID != nil {
			rl, e := c.repo.ResourceLeaseTx(ctx, tx, *o.ResourceLeaseID)
			if e != nil {
				return e
			}
			if rl.Status == "active" {
				if e := c.repo.ReleaseResourceLease(ctx, tx, rl.ID, rl.Revision, now); e != nil {
					return e
				}
				lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "operation_id": o.ID, "reason": "operation_aborted"})
				if e := c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &o.WorkspaceID, Type: "resource_lease.released", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: lp, OccurredAt: now}); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		return Operation{}, err
	}
	c.releaseGlobalResourceLease(ctx, Operation{GlobalResourceLeaseID: oGlobalLeaseID})
	return c.repo.Get(ctx, cmd.OperationID)
}

func (c *Coordinator) releaseGlobalResourceLease(ctx context.Context, o Operation) {
	if c.globalResources != nil && o.GlobalResourceLeaseID != nil {
		_ = c.globalResources.Release(ctx, *o.GlobalResourceLeaseID)
	}
}

func (c *Coordinator) finishKnownFailure(ctx context.Context, o Operation, reason string, actor, requestID, traceID *string) error {
	now := c.clock.UnixMilli()
	evtID, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	var globalLeaseID *string
	err := c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		cur, e := c.repo.GetTx(ctx, tx, o.ID)
		if e != nil {
			return e
		}
		globalLeaseID = cur.GlobalResourceLeaseID
		if cur.State != StateExecuting {
			return ErrInvalidTransition
		}
		if e := c.repo.Transition(ctx, tx, cur.ID, cur.Revision, StateExecuting, StateFailed, now); e != nil {
			return e
		}
		p, _ := json.Marshal(map[string]any{"operation_id": cur.ID, "from": StateExecuting, "to": StateFailed, "reason": reason})
		if e := c.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &cur.WorkspaceID, Type: "operation.failed", AggregateType: "operation", AggregateID: cur.ID, ActorPrincipalID: actor, RequestID: requestID, TraceID: traceID, Payload: p, OccurredAt: now}); e != nil {
			return e
		}
		if cur.ResourceLeaseID != nil {
			rl, e := c.repo.ResourceLeaseTx(ctx, tx, *cur.ResourceLeaseID)
			if e != nil {
				return e
			}
			if rl.Status == "active" {
				if e := c.repo.ReleaseResourceLease(ctx, tx, rl.ID, rl.Revision, now); e != nil {
					return e
				}
				lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "operation_id": cur.ID, "reason": "known_failure"})
				return c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &cur.WorkspaceID, Type: "resource_lease.released", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: actor, RequestID: requestID, TraceID: traceID, Payload: lp, OccurredAt: now})
			}
		}
		return nil
	})
	if err == nil {
		c.releaseGlobalResourceLease(ctx, Operation{GlobalResourceLeaseID: globalLeaseID})
	}
	return err
}
func (c *Coordinator) transitionWithDetails(ctx context.Context, id string, revision int64, from, to State, actor, requestID, traceID *string, details map[string]any) error {
	if !CanTransition(from, to) {
		return ErrInvalidTransition
	}
	evtID, _ := c.ids.New("evt")
	now := c.clock.UnixMilli()
	return c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		op, e := c.repo.GetTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if op.Revision != revision || op.State != from {
			return ErrRevisionConflict
		}
		if e := c.repo.Transition(ctx, tx, id, revision, from, to, now); e != nil {
			return e
		}
		// Once a mutation crosses PREPARED -> EXECUTING, any Task checkpoint
		// created before that boundary is conservatively stale. The adapter may
		// already have produced an external side effect by the time control
		// returns, so prior completion evidence must never remain reusable.
		if from == StatePrepared && to == StateExecuting && op.TaskID != nil {
			checkpointIDs, e := c.repo.StaleValidCheckpointsForTask(ctx, tx, *op.TaskID, now)
			if e != nil {
				return e
			}
			for _, checkpointID := range checkpointIDs {
				checkpointEvtID, e := c.ids.New("evt")
				if e != nil {
					return e
				}
				cp, _ := json.Marshal(map[string]any{
					"checkpoint_id": checkpointID,
					"task_id":       *op.TaskID,
					"operation_id":  op.ID,
					"reason":        "mutation_execution_started",
				})
				if e := c.events.Append(ctx, tx, event.Event{ID: checkpointEvtID, WorkspaceID: &op.WorkspaceID, Type: "checkpoint.stale", AggregateType: "checkpoint", AggregateID: checkpointID, ActorPrincipalID: actor, RequestID: requestID, TraceID: traceID, Payload: cp, OccurredAt: now}); e != nil {
					return e
				}
			}
		}
		p, _ := json.Marshal(map[string]any{"operation_id": id, "task_id": op.TaskID, "attempt_id": op.AttemptID, "resource_ref": op.ResourceRef, "tool_id": op.ToolID, "tool_version": op.ToolVersion, "capability_id": op.CapabilityID, "from": from, "to": to, "details": details})
		return c.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &op.WorkspaceID, Type: "operation." + string(to), AggregateType: "operation", AggregateID: id, ActorPrincipalID: actor, RequestID: requestID, TraceID: traceID, Payload: p, OccurredAt: now})
	})
}

func canonicalOrDefault(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return canonicalJSON(raw)
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
	return json.Marshal(v)
}
func hashJSON(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func sameIntent(o Operation, cmd PrepareCommand, inputHash string) bool {
	return o.WorkspaceID == cmd.WorkspaceID && o.PrincipalID == cmd.PrincipalID && o.ToolID == strings.TrimSpace(cmd.ToolID) && o.ToolVersion == strings.TrimSpace(cmd.ToolVersion) && o.ResourceRef == strings.TrimSpace(cmd.ResourceRef) && o.InputHash == inputHash && sameOptionalString(o.TaskID, cmd.TaskID) && sameOptionalString(o.AttemptID, cmd.AttemptID) && sameOptionalString(o.CompensatesOperationID, cmd.CompensatesOperationID)
}

// RecoverInterrupted converts operations that were EXECUTING when harnessd
// stopped into BLOCKED_UNKNOWN_OUTCOME. It never retries them. A fresh direct
// observation is required before either accepting the resulting state or
// retrying the mutation.
func (c *Coordinator) RecoverInterrupted(ctx context.Context, actor, requestID, traceID *string) (RecoverySweepResult, error) {
	ops, err := c.repo.ListByStates(ctx, StateExecuting, StateUnknownOutcome)
	if err != nil {
		return RecoverySweepResult{}, err
	}
	var out RecoverySweepResult
	for _, o := range ops {
		if o.State == StateExecuting {
			if err := c.transitionWithDetails(ctx, o.ID, o.Revision, StateExecuting, StateUnknownOutcome, actor, requestID, traceID, map[string]any{"reason": "process_restart", "retry": false}); err != nil {
				return out, err
			}
			out.Interrupted = append(out.Interrupted, o.ID)
			o, err = c.repo.Get(ctx, o.ID)
			if err != nil {
				return out, err
			}
		}
		if o.State == StateUnknownOutcome {
			if err := c.transitionWithDetails(ctx, o.ID, o.Revision, StateUnknownOutcome, StateBlockedUnknownOutcome, actor, requestID, traceID, map[string]any{"reason": "reconciliation_required"}); err != nil {
				return out, err
			}
			out.Blocked = append(out.Blocked, o.ID)
		}
	}
	return out, nil
}

func (c *Coordinator) BlockUnknown(ctx context.Context, cmd RecoveryCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || cmd.ExpectedRevision < 1 {
		return Operation{}, ErrInvalidCommand
	}
	if err := c.transitionWithDetails(ctx, cmd.OperationID, cmd.ExpectedRevision, StateUnknownOutcome, StateBlockedUnknownOutcome, cmd.ActorPrincipalID, cmd.RequestID, cmd.TraceID, map[string]any{"reason": "reconciliation_required"}); err != nil {
		return Operation{}, err
	}
	return c.repo.Get(ctx, cmd.OperationID)
}

// AcceptReconciledOutcome resumes the normal observation/commit path only
// after a V2+ passing verification has established that the desired external
// state already exists. This is the safe path for "request succeeded but the
// response was lost" failures.
func (c *Coordinator) AcceptReconciledOutcome(ctx context.Context, cmd RecoveryCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || cmd.ExpectedRevision < 1 {
		return Operation{}, ErrInvalidCommand
	}
	now := c.clock.UnixMilli()
	evt, _ := c.ids.New("evt")
	err := c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		o, e := c.repo.GetTx(ctx, tx, cmd.OperationID)
		if e != nil {
			return e
		}
		if o.Revision != cmd.ExpectedRevision || o.State != StateBlockedUnknownOutcome {
			return ErrRevisionConflict
		}
		v, e := c.repo.VerificationForOperation(ctx, tx, o.ID, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.Status != "pass" || v.AchievedLevel == nil || verification.LevelRank(policy.VerificationLevel(*v.AchievedLevel)) < verification.LevelRank(policy.VerificationV2) {
			return ErrVerificationInsufficient
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision, StateBlockedUnknownOutcome, StateObserving, now); e != nil {
			return e
		}
		p, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": StateBlockedUnknownOutcome, "to": StateObserving, "verification_id": v.ID, "reason": "reconciled_desired_state_present"})
		return c.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &o.WorkspaceID, Type: "operation.reconciled", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: p, OccurredAt: now})
	})
	if err != nil {
		return Operation{}, err
	}
	return c.repo.Get(ctx, cmd.OperationID)
}

// RetryAfterReconciliation is deliberately separate from normal Execute. It
// requires a V2+ verification proving that the previous unknown attempt is in
// a known state from which the idempotent mutation may safely be retried. The
// existing exclusive resource lease is renewed instead of allocating a second
// writer lease.
func (c *Coordinator) RetryAfterReconciliation(ctx context.Context, cmd RecoveryCommand) (Operation, error) {
	if strings.TrimSpace(cmd.OperationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || cmd.ExpectedRevision < 1 {
		return Operation{}, ErrInvalidCommand
	}
	ttl := cmd.LeaseTTLMillis
	if ttl == 0 {
		ttl = 5 * 60 * 1000
	}
	if ttl < 10_000 || ttl > 60*60*1000 {
		return Operation{}, ErrInvalidCommand
	}
	now := c.clock.UnixMilli()
	evt, _ := c.ids.New("evt")
	leaseEvt, _ := c.ids.New("evt")
	err := c.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		o, e := c.repo.GetTx(ctx, tx, cmd.OperationID)
		if e != nil {
			return e
		}
		if o.Revision != cmd.ExpectedRevision || o.State != StateBlockedUnknownOutcome || o.ResourceLeaseID == nil {
			return ErrRevisionConflict
		}
		v, e := c.repo.VerificationForOperation(ctx, tx, o.ID, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.Status != "pass" || v.AchievedLevel == nil || verification.LevelRank(policy.VerificationLevel(*v.AchievedLevel)) < verification.LevelRank(policy.VerificationV2) {
			return ErrVerificationInsufficient
		}
		rl, e := c.repo.ResourceLeaseTx(ctx, tx, *o.ResourceLeaseID)
		if e != nil {
			return e
		}
		if rl.Status != "active" {
			return ErrResourceLeaseInvalid
		}
		if e := c.repo.RenewResourceLease(ctx, tx, rl.ID, rl.Revision, now+ttl); e != nil {
			return e
		}
		if e := c.repo.Transition(ctx, tx, o.ID, o.Revision, StateBlockedUnknownOutcome, StatePrepared, now); e != nil {
			return e
		}
		lp, _ := json.Marshal(map[string]any{"resource_lease_id": rl.ID, "operation_id": o.ID, "expires_at": now + ttl, "reason": "verified_safe_retry"})
		if e := c.events.Append(ctx, tx, event.Event{ID: leaseEvt, WorkspaceID: &o.WorkspaceID, Type: "resource_lease.renewed", AggregateType: "resource_lease", AggregateID: rl.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: lp, OccurredAt: now}); e != nil {
			return e
		}
		p, _ := json.Marshal(map[string]any{"operation_id": o.ID, "from": StateBlockedUnknownOutcome, "to": StatePrepared, "verification_id": v.ID, "reason": "verified_safe_retry"})
		return c.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: &o.WorkspaceID, Type: "operation.retry_prepared", AggregateType: "operation", AggregateID: o.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: p, OccurredAt: now})
	})
	if err != nil {
		return Operation{}, err
	}
	return c.repo.Get(ctx, cmd.OperationID)
}
