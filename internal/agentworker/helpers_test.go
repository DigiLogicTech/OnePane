package agentworker

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestCompletionVerificationDefaultsFailConservativelyToV0(t *testing.T) {
	cases := []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"required_verification":"bogus"}`), json.RawMessage(`not-json`)}
	for _, raw := range cases {
		if got := completionVerification(raw); got != policy.VerificationV0 {
			t.Fatalf("completionVerification(%q)=%s, want V0", raw, got)
		}
	}
}

func TestCompletionVerificationParsesLevels(t *testing.T) {
	for _, level := range []policy.VerificationLevel{
		policy.VerificationV0, policy.VerificationV1, policy.VerificationV2,
		policy.VerificationV3, policy.VerificationV4, policy.VerificationV5,
	} {
		raw := json.RawMessage(`{"required_verification":"` + string(level) + `"}`)
		if got := completionVerification(raw); got != level {
			t.Fatalf("got %s, want %s", got, level)
		}
	}
}

func TestDefaultWorkerPolicyProtectsSubscriptionAndMoney(t *testing.T) {
	p := defaultRoutePolicy()
	if p.AllowSubscriptionUsage {
		t.Fatal("subscription allowance must be protected by default")
	}
	if p.AllowPotentialMonetarySpend {
		t.Fatal("potential monetary spend must be protected by default")
	}
	if !p.PreferZeroIncrementalCost {
		t.Fatal("worker should prefer local/proven-zero-cost routes")
	}
}
