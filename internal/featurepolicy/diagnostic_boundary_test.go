package featurepolicy

import (
	"strings"
	"testing"
	"time"
)

func TestDiagnosticBoundaryRejectsMalformedValues(t *testing.T) {
	now := time.Unix(123, 0)
	for _, id := range []string{"run:", "request:", "trace:has spaces", "task:\nsecret", strings.Repeat("a", 129)} {
		record := DecisionDiagnostic(now, id, "inference.local", false)
		if record.CorrelationID != "redacted" {
			t.Errorf("invalid correlation identifier retained: %q", id)
		}
	}
	for _, action := range []string{"", "shell.exec", "secrets.export", "inference.cloud;token=abc"} {
		record := DecisionDiagnostic(now, "run:1", action, false)
		if record.Action != "redacted" {
			t.Errorf("unexpected action retained: %q", action)
		}
	}
}
