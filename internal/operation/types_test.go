package operation

import "testing"

func TestOperationStateMachineRequiresObservationBeforeCommit(t *testing.T) {
	allowed := [][2]State{
		{StateProposed, StateAuthorized},
		{StateAuthorized, StatePrepared},
		{StatePrepared, StateExecuting},
		{StateExecuting, StateObserving},
		{StateObserving, StateVerified},
		{StateVerified, StateCommitted},
	}
	for _, p := range allowed {
		if !CanTransition(p[0], p[1]) {
			t.Fatalf("expected transition %s -> %s", p[0], p[1])
		}
	}
	for _, p := range [][2]State{
		{StateExecuting, StateCommitted},
		{StateObserving, StateCommitted},
		{StatePrepared, StateCommitted},
		{StateUnknownOutcome, StateExecuting},
	} {
		if CanTransition(p[0], p[1]) {
			t.Fatalf("unsafe transition allowed %s -> %s", p[0], p[1])
		}
	}
}

func TestUnknownOutcomeMustBlockBeforeRecoveryWork(t *testing.T) {
	if !CanTransition(StateUnknownOutcome, StateBlockedUnknownOutcome) {
		t.Fatal("unknown outcome must be blockable for reconciliation")
	}
	if CanTransition(StateUnknownOutcome, StatePrepared) || CanTransition(StateUnknownOutcome, StateCommitted) {
		t.Fatal("unknown outcome must not be directly retried or committed")
	}
}

func TestUnknownOutcomeRequiresBlockedReconciliationBeforeRetry(t *testing.T) {
	if CanTransition(StateUnknownOutcome, StatePrepared) {
		t.Fatal("unknown outcome must never retry directly")
	}
	if !CanTransition(StateUnknownOutcome, StateBlockedUnknownOutcome) {
		t.Fatal("unknown outcome must enter reconciliation block")
	}
	if !CanTransition(StateBlockedUnknownOutcome, StatePrepared) {
		t.Fatal("verified-safe reconciliation must be able to return to prepared")
	}
}
