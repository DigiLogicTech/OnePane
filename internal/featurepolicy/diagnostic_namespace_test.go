package featurepolicy

import (
	"testing"
	"time"
)

func TestDiagnosticCorrelationNamespace(t *testing.T) {
	for _, id := range []string{"unknown:123", "api:key", "secret:123", "plain"} {
		got := DecisionDiagnostic(time.Unix(1, 0), id, "inference.local", false)
		if got.CorrelationID != "redacted" {
			t.Fatalf("unexpected identifier retained: %q", got.CorrelationID)
		}
	}
	for _, id := range []string{"run:123", "task:abc", "attempt:7", "request:9", "trace:xyz"} {
		got := DecisionDiagnostic(time.Unix(1, 0), id, "inference.local", true)
		if got.CorrelationID != id {
			t.Fatalf("safe identifier lost: %q", id)
		}
	}
}
