package featurepolicy

import (
	"strings"
	"time"
)

// Diagnostic contains only bounded identifiers and fixed policy decision labels.
// It must never contain a prompt, credential, input payload or raw tool result.
type Diagnostic struct {
	At time.Time `json:"at_utc"`
	Component string `json:"component"`
	CorrelationID string `json:"correlation_id"`
	Action string `json:"action"`
	Allowed bool `json:"allowed"`
	Category string `json:"category"`
}

func safeCorrelationID(id string) string {
	if len(id) < 5 || len(id) > 104 {
		return "redacted"
	}
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 || len(parts[1]) == 0 || len(parts[1]) > 96 {
		return "redacted"
	}
	switch parts[0] {
	case "run", "task", "attempt", "request", "trace":
	default:
		return "redacted"
	}
	for _, c := range parts[1] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return "redacted"
		}
	}
	return id
}

// DecisionDiagnostic emits a safe, fixed-shape policy decision without logging
// policy contents, prompts, credentials or arbitrary user-provided action names.
func DecisionDiagnostic(at time.Time, correlationID, action string, allowed bool) Diagnostic {
	category := "policy.denied"
	if allowed {
		category = "policy.allowed"
	}
	if !knownDiagnosticAction(action) {
		action = "redacted"
	}
	return Diagnostic{
		At: at.UTC(), Component: "featurepolicy",
		CorrelationID: safeCorrelationID(correlationID), Action: action,
		Allowed: allowed, Category: category,
	}
}

func knownDiagnosticAction(action string) bool {
	switch action {
	case "inference.local", "inference.cloud", "inference.fallback",
		"files.read", "files.write", "projects.share", "tasks.autonomous":
		return true
	default:
		return false
	}
}
