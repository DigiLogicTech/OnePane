package featurepolicy

import (
	"crypto/sha256"
	"encoding/hex"
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
	Remediation string `json:"remediation"`
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
	// Hash even well-formed IDs: a typed, harmless-looking token may still
	// carry a password or private identifier in its suffix.
	sum := sha256.Sum256([]byte(id))
	return parts[0]+":"+hex.EncodeToString(sum[:12])
}

// DecisionDiagnostic emits a safe, fixed-shape policy decision without logging
// policy contents, prompts, credentials or arbitrary user-provided action names.
func DecisionDiagnostic(at time.Time, correlationID, action string, allowed bool) Diagnostic {
	category := "policy.denied"
	remediation := "Review effective Project, Workspace and Agent policy; do not auto-escalate permissions."
	if allowed {
		category = "policy.allowed"
		remediation = "No permission change required."
	}
	if !knownDiagnosticAction(action) {
		action = "redacted"
	}
	return Diagnostic{
		At: at.UTC(), Component: "featurepolicy",
		CorrelationID: safeCorrelationID(correlationID), Action: action,
		Allowed: allowed, Category: category, Remediation: remediation,
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
