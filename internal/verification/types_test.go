package verification

import (
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"testing"
)

func TestVerificationLevelsAreOrdered(t *testing.T) {
	levels := []policy.VerificationLevel{policy.VerificationV0, policy.VerificationV1, policy.VerificationV2, policy.VerificationV3, policy.VerificationV4, policy.VerificationV5}
	for i, l := range levels {
		if LevelRank(l) != i {
			t.Fatalf("%s rank=%d", l, LevelRank(l))
		}
	}
	if Meets(policy.VerificationV3, policy.VerificationV2) {
		t.Fatal("V2 must not satisfy V3")
	}
	if !Meets(policy.VerificationV3, policy.VerificationV4) {
		t.Fatal("V4 should satisfy V3")
	}
}

func TestVerificationTransitions(t *testing.T) {
	if !CanResolve(StatusPending, StatusPass) || !CanResolve(StatusPass, StatusStale) {
		t.Fatal("expected transitions missing")
	}
	if CanResolve(StatusPass, StatusFail) || CanResolve(StatusStale, StatusPass) {
		t.Fatal("invalid verification transition allowed")
	}
}
