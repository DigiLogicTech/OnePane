package verticalslice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type taskService interface {
	Create(context.Context, task.CreateCommand) (task.Task, error)
	MarkReady(context.Context, task.TransitionCommand) (task.Task, error)
	Start(context.Context, task.StartCommand) (task.Task, task.Attempt, error)
	RequestCompletion(context.Context, task.TransitionCommand) (task.Task, error)
	BeginVerification(context.Context, task.TransitionCommand) (task.Task, error)
	CompleteVerified(context.Context, task.CompleteCommand) (task.Task, error)
}

type authorityService interface {
	Issue(context.Context, authority.IssueCommand) (authority.Lease, error)
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
	CreateCheckpoint(context.Context, verification.CheckpointCommand) (verification.Checkpoint, error)
}

type ReadOnlyRunner struct {
	tasks        taskService
	authority    authorityService
	tools        toolGateway
	observations observationService
	verification verificationService
}

func NewReadOnlyRunner(tasks taskService, auth authorityService, tools toolGateway, observations observationService, verification verificationService) *ReadOnlyRunner {
	return &ReadOnlyRunner{tasks: tasks, authority: auth, tools: tools, observations: observations, verification: verification}
}

type ReadOnlyCommand struct {
	WorkspaceID         string
	ProjectID           *string
	ActorPrincipalID    string
	WorkerPrincipalID   string
	IssuerPrincipalID   string
	VerifierPrincipalID string
	Objective           string
	ResourceRef         string
	Input               json.RawMessage
	RequestID           *string
	TraceID             *string
}

type ReadOnlyResult struct {
	Task         task.Task
	Attempt      task.Attempt
	Lease        authority.Lease
	Invocation   tool.Invocation
	Observation  observation.Observation
	Verification verification.Verification
	Checkpoint   verification.Checkpoint
}

var ErrInvalidReadOnlyCommand = errors.New("invalid read-only vertical slice command")

// Run executes the M14 reference read-only vertical slice. It intentionally uses
// the built-in synthetic read tool so the slice proves control-plane semantics
// without depending on an external service. No mutating capability is granted.
func (r *ReadOnlyRunner) Run(ctx context.Context, cmd ReadOnlyCommand) (ReadOnlyResult, error) {
	if r == nil || r.tasks == nil || r.authority == nil || r.tools == nil || r.observations == nil || r.verification == nil {
		return ReadOnlyResult{}, fmt.Errorf("%w: runner dependencies are unavailable", ErrInvalidReadOnlyCommand)
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.ActorPrincipalID) == "" ||
		strings.TrimSpace(cmd.WorkerPrincipalID) == "" || strings.TrimSpace(cmd.IssuerPrincipalID) == "" ||
		strings.TrimSpace(cmd.VerifierPrincipalID) == "" || strings.TrimSpace(cmd.ResourceRef) == "" {
		return ReadOnlyResult{}, fmt.Errorf("%w: workspace, actor, worker, issuer, verifier and resource are required", ErrInvalidReadOnlyCommand)
	}
	if cmd.WorkerPrincipalID == cmd.VerifierPrincipalID {
		return ReadOnlyResult{}, fmt.Errorf("%w: verifier must be independent from the worker", ErrInvalidReadOnlyCommand)
	}
	if strings.TrimSpace(cmd.Objective) == "" {
		cmd.Objective = "M14 synthetic read-only vertical slice"
	}
	if len(cmd.Input) == 0 {
		cmd.Input = json.RawMessage(`{"probe":"m14"}`)
	}
	if !json.Valid(cmd.Input) {
		return ReadOnlyResult{}, fmt.Errorf("%w: input must be valid JSON", ErrInvalidReadOnlyCommand)
	}

	actor := cmd.ActorPrincipalID
	t, err := r.tasks.Create(ctx, task.CreateCommand{
		WorkspaceID: cmd.WorkspaceID, ProjectID: cmd.ProjectID, Objective: cmd.Objective,
		SchedulingClass: task.ClassUserInteractive, Priority: 0,
		Completion:       json.RawMessage(`{"verification_level":"V1","condition":"synthetic read observation integrity verified"}`),
		ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("create task: %w", err)
	}

	t, err = r.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: t.ID, ExpectedRevision: t.Revision, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("mark task ready: %w", err)
	}
	worker := cmd.WorkerPrincipalID
	t, attempt, err := r.tasks.Start(ctx, task.StartCommand{TaskID: t.ID, ExpectedRevision: t.Revision, WorkerPrincipalID: &worker, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Metadata: json.RawMessage(`{"vertical_slice":"M14","mode":"read_only"}`)})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("start task: %w", err)
	}

	usage := int64(1)
	lease, err := r.authority.Issue(ctx, authority.IssueCommand{
		WorkspaceID: cmd.WorkspaceID, PrincipalID: worker, TaskID: &t.ID,
		CapabilityID: tool.SyntheticEchoCapability,
		Scope:        authority.Scope{ResourceRefs: []string{cmd.ResourceRef}, Actions: []authority.ActionMode{authority.ActionRead}},
		IssuedBy:     cmd.IssuerPrincipalID, ExpiresAt: time.Now().UTC().Add(5 * time.Minute).UnixMilli(), UsageLimit: &usage,
		ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("issue read lease: %w", err)
	}

	inv, err := r.tools.Invoke(ctx, tool.InvokeCommand{
		WorkspaceID: cmd.WorkspaceID, TaskID: &t.ID, AttemptID: &attempt.ID,
		PrincipalID: worker, LeaseID: lease.ID, ToolID: tool.SyntheticEchoToolID, ToolVersion: tool.SyntheticEchoToolVersion,
		ResourceRef: cmd.ResourceRef, Input: cmd.Input, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("invoke read tool: %w", err)
	}
	if inv.Status != tool.StatusSucceeded || len(inv.Result) == 0 || !json.Valid(inv.Result) {
		return ReadOnlyResult{}, fmt.Errorf("read tool returned non-success terminal state %q", inv.Status)
	}

	adapterID, adapterVersion := inv.AdapterID, inv.AdapterVersion
	obs, err := r.observations.Record(ctx, observation.RecordCommand{
		WorkspaceID: cmd.WorkspaceID, SubjectRef: cmd.ResourceRef, ObservationType: "synthetic.read",
		ProbeToolID: inv.ToolID, ProbeToolVersion: inv.ToolVersion, SourcePrincipalID: &worker,
		AdapterID: &adapterID, AdapterVersion: &adapterVersion, Value: inv.Result,
		Label:            policy.DataLabel{WorkspaceID: cmd.WorkspaceID, Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: policy.TrustUnverifiedDerived},
		ActorPrincipalID: &worker, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("record observation: %w", err)
	}
	if err := r.observations.VerifyIntegrity(ctx, obs.ID); err != nil {
		return ReadOnlyResult{}, fmt.Errorf("verify observation integrity: %w", err)
	}

	t, err = r.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: t.ID, ExpectedRevision: t.Revision, ActorPrincipalID: &worker, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "read observation captured"})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("request completion: %w", err)
	}
	t, err = r.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: t.ID, ExpectedRevision: t.Revision, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Reason: "verify M14 observation"})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("begin verification: %w", err)
	}

	verificationSpec, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "integrity_hash": obs.IntegrityHash, "method": "deterministic_integrity"})
	v, err := r.verification.Create(ctx, verification.CreateCommand{
		WorkspaceID: cmd.WorkspaceID, TaskID: &t.ID, SubjectRef: "observation:" + obs.ID,
		RequiredLevel: policy.VerificationV1, Spec: verificationSpec,
		ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("create verification: %w", err)
	}
	achieved := policy.VerificationV1
	verificationResult, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "integrity_hash_matches": true})
	v, err = r.verification.Resolve(ctx, verification.ResolveCommand{
		VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass,
		AchievedLevel: &achieved, Result: verificationResult, VerifiedBy: cmd.VerifierPrincipalID,
		ActorPrincipalID: &cmd.VerifierPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("resolve verification: %w", err)
	}

	cpState, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "tool_invocation_id": inv.ID, "verification_id": v.ID})
	cp, err := r.verification.CreateCheckpoint(ctx, verification.CheckpointCommand{
		WorkspaceID: cmd.WorkspaceID, TaskID: t.ID, VerificationID: v.ID, State: cpState,
		ActorPrincipalID: &cmd.VerifierPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("create checkpoint: %w", err)
	}

	finalResult, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "checkpoint_id": cp.ID, "verification_level": "V1"})
	t, err = r.tasks.CompleteVerified(ctx, task.CompleteCommand{
		TaskID: t.ID, ExpectedRevision: t.Revision, CheckpointID: cp.ID, Result: finalResult,
		ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID,
	})
	if err != nil {
		return ReadOnlyResult{}, fmt.Errorf("complete verified task: %w", err)
	}

	return ReadOnlyResult{Task: t, Attempt: attempt, Lease: lease, Invocation: inv, Observation: obs, Verification: v, Checkpoint: cp}, nil
}
