package featurepolicy

import (
	"regexp"
	"strings"
	"time"
)

// Diagnostic records a bounded policy decision without persisting credentials.
type Diagnostic struct {
	At time.Time
	Component string
	CorrelationID string
	Action string
	Allowed bool
	Category string
}

var safeDiagnosticToken = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

// DecisionDiagnostic deliberately omits policy contents, prompts and secrets.
func DecisionDiagnostic(at time.Time, correlationID, action string, allowed bool) Diagnostic {
	category := "policy.denied"
	if allowed {
		category = "policy.allowed"
	}
	if !safeDiagnosticToken.MatchString(correlationID) {
		correlationID = "redacted"
	}
	if !safeDiagnosticToken.MatchString(action) || strings.Contains(strings.ToLower(action), "token") {
		action = "redacted"
	}
	return Diagnostic{
		At: at.UTC(), Component: "featurepolicy",
		CorrelationID: correlationID, Action: action,
		Allowed: allowed, Category: category,
	}
}
