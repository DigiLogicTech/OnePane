package chatcommands

import "testing"

func TestQueueAdvanceConservative(t *testing.T) {
	allowed := []TurnState{TurnComplete, TurnIdle}
	for _, s := range allowed {
		if !CanAdvanceQueue(s, false) {
			t.Fatalf("expected %s to advance", s)
		}
	}
	blocked := []TurnState{TurnRunning, TurnWaitingApproval, TurnBlocked, TurnInterrupted, TurnFailed, TurnUnknownOutcome}
	for _, s := range blocked {
		if CanAdvanceQueue(s, false) {
			t.Fatalf("expected %s to block", s)
		}
	}
	if CanAdvanceQueue(TurnComplete, true) {
		t.Fatal("paused queue advanced")
	}
}
func TestYoloStillRequiresAuthorityAndPolicy(t *testing.T) {
	c := SessionControls{ApprovalMode: ApprovalAutoAuthorized}
	if !ShouldAutoApprove(c, true, true) {
		t.Fatal("expected auto approval")
	}
	if ShouldAutoApprove(c, false, true) {
		t.Fatal("auto approved without user authority")
	}
	if ShouldAutoApprove(c, true, false) {
		t.Fatal("auto approved against policy")
	}
	c.ApprovalMode = ApprovalManual
	if ShouldAutoApprove(c, true, true) {
		t.Fatal("manual mode auto approved")
	}
}
