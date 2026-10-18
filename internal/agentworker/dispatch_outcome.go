package agentworker

import (
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/inference"
)

// ErrDispatchRequiresReconciliation signals that an inference/Agent-runtime
// invocation might have consumed paid quota, produced output or performed
// external effects. The Worker must park its original Attempt for operator
// reconciliation, never automatically route to a fallback model.
var ErrDispatchRequiresReconciliation = errors.New("agent worker dispatch requires operator reconciliation")

func dispatchReconciliationError(err error) error {
	return fmt.Errorf("%w: %w", ErrDispatchRequiresReconciliation, err)
}

// Only a durable known terminal failure/rate limit proves an inference
// request was not left executing with an unknown result. Empty identifiers,
// interrupted database reads and intermediate states fail closed.
func reconcileInferenceDispatchError(q inference.InferenceRequest, err error) error {
	if err == nil {
		return nil
	}
	switch q.Status {
	case inference.RequestFailed, inference.RequestRateLimited:
		return err
	default:
		return dispatchReconciliationError(fmt.Errorf("inference request %s status %s: %w", q.ID, q.Status, err))
	}
}

// External Agent runtimes may perform their own side effects. An UNKNOWN,
// in-flight, successful-but-unreadable or unavailable Invocation is never
// proof that repeating the request is safe.
func reconcileRuntimeDispatchError(r agentruntime.InvokeResult, err error) error {
	if err == nil {
		return nil
	}
	if r.Invocation.Status == agentruntime.InvocationFailed {
		return err
	}
	return dispatchReconciliationError(fmt.Errorf("Agent runtime invocation %s status %s: %w", r.Invocation.ID, r.Invocation.Status, err))
}
