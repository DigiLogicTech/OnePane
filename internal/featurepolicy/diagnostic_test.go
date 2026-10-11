package featurepolicy

import (
	"strings"
	"testing"
	"time"
)

func TestDecisionDiagnosticDoesNotEchoCredentialLikeInput(t *testing.T) {
	secret := "Bearer secret-credential-value"
	got := DecisionDiagnostic(time.Unix(1, 0), secret, "api-token", false)
	if got.CorrelationID != "redacted" || got.Action != "redacted" {
		t.Fatalf("sensitive diagnostic fields not redacted: %+v", got)
	}
	if strings.Contains(got.Category, secret) {
		t.Fatal("secret leaked in category")
	}
	if got.Category != "policy.denied" || got.Component != "featurepolicy" {
		t.Fatalf("unexpected diagnostic: %+v", got)
	}
}

func TestDecisionDiagnosticKeepsSafeIdentifiers(t *testing.T) {
	got := DecisionDiagnostic(time.Unix(2, 0), "run:123", "inference.local", true)
	if got.CorrelationID != "run:123" || got.Action != "inference.local" || got.Category != "policy.allowed" {
		t.Fatalf("safe diagnostic changed: %+v", got)
	}
}
