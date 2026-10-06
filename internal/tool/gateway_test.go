package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type fakeClock struct{ now int64 }

func (f *fakeClock) Now() time.Time   { return time.UnixMilli(f.now).UTC() }
func (f *fakeClock) UnixMilli() int64 { f.now++; return f.now }

type fakePolicy struct {
	decision policy.Decision
	got      policy.AuthorityInput
}

func (f *fakePolicy) EvaluateAuthority(_ context.Context, in policy.AuthorityInput) policy.Decision {
	f.got = in
	return f.decision
}

type fakeAuthority struct {
	err   error
	calls int
	got   authority.ConsumeCommand
}

func (f *fakeAuthority) Consume(_ context.Context, cmd authority.ConsumeCommand) (authority.Lease, error) {
	f.calls++
	f.got = cmd
	return authority.Lease{}, f.err
}

type fakeStore struct {
	inv         Invocation
	transitions []Transition
	contextErr  error
}

func (s *fakeStore) Get(_ context.Context, id string) (Invocation, error) {
	if s.inv.ID != id {
		return Invocation{}, errors.New("not found")
	}
	return s.inv, nil
}
func (s *fakeStore) Create(_ context.Context, inv Invocation, _ EventMeta) error {
	s.inv = inv
	return nil
}
func (s *fakeStore) Transition(_ context.Context, tr Transition, _ EventMeta) error {
	if s.inv.ID != tr.InvocationID || s.inv.Status != tr.From || !CanTransition(tr.From, tr.To) {
		return ErrInvalidTransition
	}
	s.transitions = append(s.transitions, tr)
	s.inv.Status = tr.To
	s.inv.UpdatedAt = tr.At
	if tr.Summary != nil {
		v := *tr.Summary
		s.inv.Summary = &v
	}
	if len(tr.Result) > 0 {
		s.inv.Result = append([]byte(nil), tr.Result...)
	}
	if tr.ErrorCode != nil {
		v := *tr.ErrorCode
		s.inv.ErrorCode = &v
	}
	if tr.To == StatusRunning {
		v := tr.At
		s.inv.StartedAt = &v
	}
	if tr.To == StatusSucceeded || tr.To == StatusFailed || tr.To == StatusTimedOut || tr.To == StatusCancelled || tr.To == StatusInterrupted {
		v := tr.At
		s.inv.EndedAt = &v
	}
	return nil
}
func (s *fakeStore) ValidateExecutionContext(context.Context, string, string, *string, *string) error {
	return s.contextErr
}

type countingAdapter struct {
	id, version string
	calls       int
	result      AdapterResult
	err         error
	panicValue  any
}

func (a *countingAdapter) ID() string      { return a.id }
func (a *countingAdapter) Version() string { return a.version }
func (a *countingAdapter) Invoke(context.Context, AdapterRequest) (AdapterResult, error) {
	a.calls++
	if a.panicValue != nil {
		panic(a.panicValue)
	}
	return a.result, a.err
}

func testGateway(t *testing.T, mode authority.ActionMode, decision policy.Decision, adapter *countingAdapter) (*Gateway, *fakeStore, *fakePolicy, *fakeAuthority) {
	t.Helper()
	reg := NewRegistry()
	if adapter == nil {
		adapter = &countingAdapter{id: "adapter", version: "1", result: AdapterResult{Summary: "ok", Result: json.RawMessage(`{"ok":true}`)}}
	}
	err := reg.Register(Definition{ID: "tool", Version: "1", CapabilityID: "cap", Mode: mode, AdapterID: adapter.id, AdapterVersion: adapter.version, Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	pol := &fakePolicy{decision: decision}
	auth := &fakeAuthority{}
	g := newGateway(store, pol, auth, reg, &fakeClock{now: 1000})
	return g, store, pol, auth
}

func command() InvokeCommand {
	return InvokeCommand{WorkspaceID: "ws", PrincipalID: "agent", LeaseID: "lease", ToolID: "tool", ToolVersion: "1", ResourceRef: "synthetic://r", Input: json.RawMessage(`{"b":2,"a":1}`)}
}

func TestGatewayAuthorizedReadConsumesAuthorityAndExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1", result: AdapterResult{Summary: "done", Result: json.RawMessage(`{"value":42}`)}}
	g, store, pol, auth := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 7, RequiredApproval: policy.ApprovalNone, RequiredVerification: policy.VerificationV1}, adapter)
	inv, err := g.Invoke(context.Background(), command())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != StatusSucceeded || adapter.calls != 1 || auth.calls != 1 {
		t.Fatalf("status=%s adapter=%d authority=%d", inv.Status, adapter.calls, auth.calls)
	}
	if auth.got.ExpectedRevision != 7 || auth.got.Uses != 1 {
		t.Fatalf("unexpected consume: %+v", auth.got)
	}
	if pol.got.CapabilityID != "cap" || pol.got.Action != authority.ActionRead || pol.got.Risk != policy.RiskLow {
		t.Fatalf("trusted definition not used: %+v", pol.got)
	}
	want := []Status{StatusAuthorized, StatusRunning, StatusSucceeded}
	if len(store.transitions) != len(want) {
		t.Fatalf("transitions=%v", store.transitions)
	}
	for i, w := range want {
		if store.transitions[i].To != w {
			t.Fatalf("transition %d=%s want %s", i, store.transitions[i].To, w)
		}
	}
}

func TestGatewayPolicyDeniedNeverConsumesOrExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionRead, policy.Decision{Allowed: false, Reasons: []policy.Reason{policy.ReasonResourceOutOfScope}}, adapter)
	inv, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("err=%v", err)
	}
	if inv.Status != StatusFailed || inv.ErrorCode == nil || *inv.ErrorCode != "policy_denied" {
		t.Fatalf("inv=%+v", inv)
	}
	if auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("authority=%d adapter=%d", auth.calls, adapter.calls)
	}
}

func TestGatewayApprovalRequiredNeverConsumesOrExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalApprover}, adapter)
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("err=%v", err)
	}
	if auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("authority=%d adapter=%d", auth.calls, adapter.calls)
	}
}

func TestGatewayBlocksMutationUntilOperationCoordinator(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionMutate, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, adapter)
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrOperationCoordinatorRequired) {
		t.Fatalf("err=%v", err)
	}
	if auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("authority=%d adapter=%d", auth.calls, adapter.calls)
	}
}

func TestGatewayBlocksSandboxUntilRunnerExists(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionExecuteSandboxed, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, adapter)
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrSandboxRunnerRequired) {
		t.Fatalf("err=%v", err)
	}
	if auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("authority=%d adapter=%d", auth.calls, adapter.calls)
	}
}

func TestGatewayConsumeFailureNeverExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 2, RequiredApproval: policy.ApprovalNone}, adapter)
	auth.err = authority.ErrRevisionConflict
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrAuthorityConsumption) {
		t.Fatalf("err=%v", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter called %d", adapter.calls)
	}
}

func TestGatewayExecutionContextFailureNeverConsumesOrExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, store, _, auth := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, adapter)
	store.contextErr = ErrAttemptPrincipalMismatch
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrExecutionContext) {
		t.Fatalf("err=%v", err)
	}
	if auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("authority=%d adapter=%d", auth.calls, adapter.calls)
	}
}

func TestGatewayAdapterPanicBecomesFailedInvocation(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1", panicValue: "boom"}
	g, _, _, _ := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, adapter)
	inv, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrAdapterFailure) {
		t.Fatalf("err=%v", err)
	}
	if inv.Status != StatusFailed {
		t.Fatalf("status=%s", inv.Status)
	}
}

func TestCanonicalJSONProducesStableInputHash(t *testing.T) {
	a, err := canonicalJSON(json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalJSON(json.RawMessage(` { "a" : 1, "b" : 2 } `))
	if err != nil {
		t.Fatal(err)
	}
	if inputHash(a) != inputHash(b) {
		t.Fatalf("hash mismatch: %s %s", a, b)
	}
}

func TestRegisterSyntheticAndInvokeAdapter(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterSynthetic(reg); err != nil {
		t.Fatal(err)
	}
	b, err := reg.Resolve(SyntheticEchoToolID, SyntheticEchoToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.Adapter.Invoke(context.Background(), AdapterRequest{ToolID: SyntheticEchoToolID, ToolVersion: SyntheticEchoToolVersion, ResourceRef: "synthetic://demo", Input: json.RawMessage(`{"x":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(res.Result) {
		t.Fatalf("invalid result %s", res.Result)
	}
}

type fakeMutationPermitValidator struct {
	calls int
	got   MutationPermitCheck
	err   error
}

func (f *fakeMutationPermitValidator) ValidateAndConsume(_ context.Context, c MutationPermitCheck) error {
	f.calls++
	f.got = c
	return f.err
}

func TestGatewayMutationRequiresAndConsumesExecutionPermit(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1", result: AdapterResult{Summary: "mutated", Result: json.RawMessage(`{"ok":true}`)}}
	g, _, _, auth := testGateway(t, authority.ActionMutate, policy.Decision{Allowed: true, LeaseRevision: 4, RequiredApproval: policy.ApprovalNone, RequiredVerification: policy.VerificationV2}, adapter)
	permits := &fakeMutationPermitValidator{}
	g.SetMutationPermitValidator(permits)
	cmd := command()
	op := "op-1"
	cmd.OperationID = &op
	cmd.ExecutionPermit = "ephemeral-token"
	inv, err := g.Invoke(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != StatusSucceeded || permits.calls != 1 || auth.calls != 1 || adapter.calls != 1 {
		t.Fatalf("status=%s permits=%d authority=%d adapter=%d", inv.Status, permits.calls, auth.calls, adapter.calls)
	}
	if permits.got.OperationID != op || permits.got.Permit != "ephemeral-token" || permits.got.InputHash == "" {
		t.Fatalf("permit check=%+v", permits.got)
	}
}

func TestGatewayInvalidMutationPermitNeverConsumesOrExecutes(t *testing.T) {
	adapter := &countingAdapter{id: "adapter", version: "1"}
	g, _, _, auth := testGateway(t, authority.ActionMutate, policy.Decision{Allowed: true, LeaseRevision: 4, RequiredApproval: policy.ApprovalNone}, adapter)
	permits := &fakeMutationPermitValidator{err: errors.New("bad permit")}
	g.SetMutationPermitValidator(permits)
	cmd := command()
	op := "op-1"
	cmd.OperationID = &op
	cmd.ExecutionPermit = "bad"
	_, err := g.Invoke(context.Background(), cmd)
	if !errors.Is(err, ErrOperationCoordinatorRequired) {
		t.Fatalf("err=%v", err)
	}
	if permits.calls != 1 || auth.calls != 0 || adapter.calls != 0 {
		t.Fatalf("permits=%d authority=%d adapter=%d", permits.calls, auth.calls, adapter.calls)
	}
}

func TestGatewayPreservesKnownVsUnknownAdapterOutcome(t *testing.T) {
	knownAdapter := &countingAdapter{id: "adapter", version: "1", err: KnownFailure(errors.New("engine unavailable before dispatch"))}
	g, _, _, _ := testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, knownAdapter)
	_, err := g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrAdapterFailure) || AdapterOutcomeUnknown(err) {
		t.Fatalf("known failure classification err=%v unknown=%v", err, AdapterOutcomeUnknown(err))
	}
	unknownAdapter := &countingAdapter{id: "adapter", version: "1", err: errors.New("connection reset after dispatch")}
	g, _, _, _ = testGateway(t, authority.ActionRead, policy.Decision{Allowed: true, LeaseRevision: 1, RequiredApproval: policy.ApprovalNone}, unknownAdapter)
	_, err = g.Invoke(context.Background(), command())
	if !errors.Is(err, ErrAdapterFailure) || !AdapterOutcomeUnknown(err) {
		t.Fatalf("unknown failure classification err=%v unknown=%v", err, AdapterOutcomeUnknown(err))
	}
}

func TestGatewayAllowsExplicitTrustedSandboxAdapter(t *testing.T) {
	adapter := &countingAdapter{id: "sandbox", version: "1", result: AdapterResult{Summary: "done", Result: json.RawMessage(`{"ok":true}`)}}
	g, _, _, auth := testGateway(t, authority.ActionExecuteSandboxed, policy.Decision{Allowed: true, LeaseRevision: 3, RequiredApproval: policy.ApprovalNone, RequiredVerification: policy.VerificationV1}, adapter)
	g.EnableSandboxAdapter(adapter.id, adapter.version)
	inv, err := g.Invoke(context.Background(), command())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != StatusSucceeded || adapter.calls != 1 || auth.calls != 1 {
		t.Fatalf("inv=%+v adapter=%d authority=%d", inv, adapter.calls, auth.calls)
	}
}
