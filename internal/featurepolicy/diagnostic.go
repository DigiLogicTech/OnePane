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

var safeDiagnosticToken = regexp.MustCompile(`^(run|task|attempt|request|trace):[a-zA-Z0-9_.-]{1,96}package featurepolicy

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

var safeDiagnosticToken = regexp.MustCompile()

// DecisionDiagnostic deliberately omits policy contents, prompts and secrets.
func DecisionDiagnostic(at time.Time, correlationID, action string, allowed bool) Diagnostic {
	category := "policy.denied"
	if allowed {
		category = "policy.allowed"
	}
	if !safeDiagnosticToken.MatchString(correlationID) || strings.HasSuffix(correlationID, ":") || !(strings.HasPrefix(correlationID, "run:") || strings.HasPrefix(correlationID, "task:") || strings.HasPrefix(correlationID, "attempt:") || strings.HasPrefix(correlationID, "request:") || strings.HasPrefix(correlationID, "trace:")) {
		correlationID = "redacted"
	}
	if !safeDiagnosticToken.MatchString(action) || !knownDiagnosticAction(action) {
		action = "redacted"
	}
	return Diagnostic{
		At: at.UTC(), Component: "featurepolicy",
		CorrelationID: correlationID, Action: action,
		Allowed: allowed, Category: category,
	}
}

func knownDiagnosticAction(action string) bool {
	switch action {
	case "inference.local", "inference.cloud", "inference.fallback", "files.read", "files.write", "projects.share", "tasks.autonomous":
		return true
	default:
		return false
	}
}
