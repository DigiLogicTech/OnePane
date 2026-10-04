package verticalslice

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type fakeTasks struct{ calls []string }

func (f *fakeTasks) Create(_ context.Context, c task.CreateCommand) (task.Task, error) {
	f.calls = append(f.calls, "create")
	return task.Task{ID: "t", WorkspaceID: c.WorkspaceID, State: task.StateCreated, Revision: 1}, nil
}
func (f *fakeTasks) MarkReady(_ context.Context, c task.TransitionCommand) (task.Task, error) {
	f.calls = append(f.calls, "ready")
	if c.ExpectedRevision != 1 {
		panic("bad revision")
	}
	return task.Task{ID: "t", WorkspaceID: "ws", State: task.StateReady, Revision: 2}, nil
}
func (f *fakeTasks) Start(_ context.Context, c task.StartCommand) (task.Task, task.Attempt, error) {
	f.calls = append(f.calls, "start")
	if c.ExpectedRevision != 2 {
		panic("bad revision")
	}
	return task.Task{ID: "t", WorkspaceID: "ws", State: task.StateRunning, Revision: 3}, task.Attempt{ID: "a", TaskID: "t", State: task.AttemptRunning}, nil
}
func (f *fakeTasks) RequestCompletion(_ context.Context, c task.TransitionCommand) (task.Task, error) {
	f.calls = append(f.calls, "completion_requested")
	if c.ExpectedRevision != 3 {
		panic("bad revision")
	}
	return task.Task{ID: "t", WorkspaceID: "ws", State: task.StateCompletionRequested, Revision: 4}, nil
}
func (f *fakeTasks) BeginVerification(_ context.Context, c task.TransitionCommand) (task.Task, error) {
	f.calls = append(f.calls, "verifying")
	if c.ExpectedRevision != 4 {
		panic("bad revision")
	}
	return task.Task{ID: "t", WorkspaceID: "ws", State: task.StateVerifying, Revision: 5}, nil
}
func (f *fakeTasks) CompleteVerified(_ context.Context, c task.CompleteCommand) (task.Task, error) {
	f.calls = append(f.calls, "complete")
	if c.ExpectedRevision != 5 || c.CheckpointID != "cp" {
		panic("bad completion")
	}
	return task.Task{ID: "t", WorkspaceID: "ws", State: task.StateComplete, Revision: 6}, nil
}

type fakeAuthority struct{ got authority.IssueCommand }

func (f *fakeAuthority) Issue(_ context.Context, c authority.IssueCommand) (authority.Lease, error) {
	f.got = c
	return authority.Lease{ID: "lease", WorkspaceID: c.WorkspaceID, PrincipalID: c.PrincipalID, TaskID: c.TaskID, CapabilityID: c.CapabilityID, Scope: c.Scope, Status: authority.StatusActive, Revision: 1}, nil
}

type fakeTools struct{ got tool.InvokeCommand }

func (f *fakeTools) Invoke(_ context.Context, c tool.InvokeCommand) (tool.Invocation, error) {
	f.got = c
	return tool.Invocation{ID: "inv", WorkspaceID: c.WorkspaceID, TaskID: c.TaskID, AttemptID: c.AttemptID, PrincipalID: c.PrincipalID, ToolID: c.ToolID, ToolVersion: c.ToolVersion, AdapterID: "synthetic", AdapterVersion: "1", Mode: authority.ActionRead, Status: tool.StatusSucceeded, Result: json.RawMessage(`{"ok":true}`)}, nil
}

type fakeObservations struct {
	recorded observation.RecordCommand
	verified string
}

func (f *fakeObservations) Record(_ context.Context, c observation.RecordCommand) (observation.Observation, error) {
	f.recorded = c
	return observation.Observation{ID: "obs", WorkspaceID: c.WorkspaceID, SubjectRef: c.SubjectRef, IntegrityHash: "sha256:obs", Value: c.Value}, nil
}
func (f *fakeObservations) VerifyIntegrity(_ context.Context, id string) error {
	f.verified = id
	return nil
}

type fakeVerification struct {
	create     verification.CreateCommand
	resolve    verification.ResolveCommand
	checkpoint verification.CheckpointCommand
}

func (f *fakeVerification) Create(_ context.Context, c verification.CreateCommand) (verification.Verification, error) {
	f.create = c
	return verification.Verification{ID: "v", WorkspaceID: c.WorkspaceID, TaskID: c.TaskID, RequiredLevel: c.RequiredLevel, Status: verification.StatusPending, Revision: 1}, nil
}
func (f *fakeVerification) Resolve(_ context.Context, c verification.ResolveCommand) (verification.Verification, error) {
	f.resolve = c
	ach := policy.VerificationV1
	return verification.Verification{ID: "v", WorkspaceID: "ws", Status: verification.StatusPass, AchievedLevel: &ach, Revision: 2}, nil
}
func (f *fakeVerification) CreateCheckpoint(_ context.Context, c verification.CheckpointCommand) (verification.Checkpoint, error) {
	f.checkpoint = c
	return verification.Checkpoint{ID: "cp", WorkspaceID: c.WorkspaceID, TaskID: c.TaskID, VerificationID: c.VerificationID, Status: verification.CheckpointValid}, nil
}

func TestReadOnlyRunnerExercisesVerifiedReadPath(t *testing.T) {
	tasks := &fakeTasks{}
	auth := &fakeAuthority{}
	tools := &fakeTools{}
	obs := &fakeObservations{}
	ver := &fakeVerification{}
	r := NewReadOnlyRunner(tasks, auth, tools, obs, ver)
	got, err := r.Run(context.Background(), ReadOnlyCommand{WorkspaceID: "ws", ActorPrincipalID: "human", WorkerPrincipalID: "worker", IssuerPrincipalID: "human", VerifierPrincipalID: "verifier", ResourceRef: "synthetic://r", Input: json.RawMessage(`{"x":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Task.State != task.StateComplete {
		t.Fatalf("state=%s", got.Task.State)
	}
	want := []string{"create", "ready", "start", "completion_requested", "verifying", "complete"}
	if len(tasks.calls) != len(want) {
		t.Fatalf("calls=%v", tasks.calls)
	}
	for i := range want {
		if tasks.calls[i] != want[i] {
			t.Fatalf("calls=%v", tasks.calls)
		}
	}
	if auth.got.CapabilityID != tool.SyntheticEchoCapability || !auth.got.Scope.AllowsAction(authority.ActionRead) || auth.got.Scope.AllowsAction(authority.ActionMutate) {
		t.Fatalf("unsafe lease=%+v", auth.got.Scope)
	}
	if tools.got.OperationID != nil || tools.got.ExecutionPermit != "" {
		t.Fatalf("read slice used mutation path: %+v", tools.got)
	}
	if obs.verified != "obs" || obs.recorded.Label.Trust != policy.TrustUnverifiedDerived {
		t.Fatalf("observation=%+v verified=%q", obs.recorded, obs.verified)
	}
	if ver.resolve.VerifiedBy != "verifier" || ver.create.RequiredLevel != policy.VerificationV1 {
		t.Fatalf("verification=%+v %+v", ver.create, ver.resolve)
	}
}

func TestReadOnlyRunnerRequiresIndependentVerifier(t *testing.T) {
	r := NewReadOnlyRunner(&fakeTasks{}, &fakeAuthority{}, &fakeTools{}, &fakeObservations{}, &fakeVerification{})
	_, err := r.Run(context.Background(), ReadOnlyCommand{WorkspaceID: "ws", ActorPrincipalID: "human", WorkerPrincipalID: "same", IssuerPrincipalID: "human", VerifierPrincipalID: "same", ResourceRef: "r", Input: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected independent verifier rejection")
	}
}
