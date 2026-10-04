package chatcommands

// TurnState is the session-runner state used to decide whether the next queued
// user prompt may be materialized. Queue advancement is intentionally
// conservative around approvals, failures and unknown side effects.
type TurnState string

const (
	TurnIdle            TurnState = "idle"
	TurnRunning         TurnState = "running"
	TurnWaitingApproval TurnState = "waiting_approval"
	TurnBlocked         TurnState = "blocked"
	TurnInterrupted     TurnState = "interrupted"
	TurnFailed          TurnState = "failed"
	TurnUnknownOutcome  TurnState = "unknown_outcome"
	TurnComplete        TurnState = "complete"
)

func CanAdvanceQueue(state TurnState, queuePaused bool) bool {
	if queuePaused {
		return false
	}
	return state == TurnComplete || state == TurnIdle
}

// ShouldAutoApprove is the only intended interpretation of YOLO mode. All
// three predicates must remain true at the approval boundary: the session has
// opted in, the authenticated human is authorized to approve the operation,
// and governing policy allows that approval. A false predicate must fall back
// to the ordinary approval/denial path.
func ShouldAutoApprove(c SessionControls, userCanApprove, policyAllows bool) bool {
	return c.ApprovalMode == ApprovalAutoAuthorized && userCanApprove && policyAllows
}
