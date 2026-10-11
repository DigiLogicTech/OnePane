package featurepolicy

import "time"

// AuthorizeWithDiagnostic resolves scope restrictions and emits a bounded
// decision record. The caller remains responsible for enforcing the result.
func AuthorizeWithDiagnostic(project Policy, action, correlationID string, at time.Time, descendants ...Policy) (bool, Diagnostic) {
	allowed := ResolvePolicyChain(project, descendants...).ValidateExecution(action)
	return allowed, DecisionDiagnostic(at, correlationID, action, allowed)
}
