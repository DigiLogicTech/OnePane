package agentworker

import (
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/inference"
)

func TestInferenceDispatchReconciliationClassification(t *testing.T) {
	cause := errors.New("connection reset or persistence error")
	for _, tc := range []struct {
		status inference.RequestStatus
		reconcile bool
	}{
		{inference.RequestFailed, false},
		{inference.RequestRateLimited, false},
		{inference.RequestUnknown, true},
		{inference.RequestExecuting, true},
		{inference.RequestDispatched, true},
		{inference.RequestSucceeded, true},
		{inference.RequestCreated, true},
		{"", true},
	} {
		got := reconcileInferenceDispatchError(inference.InferenceRequest{ID: "inference-1", Status: tc.status}, cause)
		if !errors.Is(got, cause) || errors.Is(got, ErrDispatchRequiresReconciliation) != tc.reconcile {
			t.Errorf("inference status %q reconciliation=%v: %v", tc.status, tc.reconcile, got)
		}
	}
	if reconcileInferenceDispatchError(inference.InferenceRequest{Status: inference.RequestSucceeded}, nil) != nil {
		t.Fatal("successful call with no error must not be relabelled")
	}
}

func TestAgentRuntimeDispatchReconciliationClassification(t *testing.T) {
	cause := errors.New("external runtime transport or database error")
	for _, tc := range []struct {
		status agentruntime.InvocationStatus
		reconcile bool
	}{
		{agentruntime.InvocationFailed, false},
		{agentruntime.InvocationUnknown, true},
		{agentruntime.InvocationExecuting, true},
		{agentruntime.InvocationDispatched, true},
		{agentruntime.InvocationSucceeded, true},
		{agentruntime.InvocationAuthorized, true},
		{"", true},
	} {
		got := reconcileRuntimeDispatchError(agentruntime.InvokeResult{
			Invocation: agentruntime.Invocation{ID: "agent-invocation-1", Status: tc.status},
		}, cause)
		if !errors.Is(got, cause) || errors.Is(got, ErrDispatchRequiresReconciliation) != tc.reconcile {
			t.Errorf("runtime status %q reconciliation=%v: %v", tc.status, tc.reconcile, got)
		}
	}
	if !errors.Is(dispatchReconciliationError(cause), ErrDispatchRequiresReconciliation) {
		t.Fatal("post-dispatch response loss must prevent automatic fallback")
	}
}
