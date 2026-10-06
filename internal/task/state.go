package task

import "errors"

var (
	ErrRevisionConflict     = errors.New("revision conflict")
	ErrInvalidTransition    = errors.New("invalid task state transition")
	ErrInvalidAttempt       = errors.New("invalid task attempt state transition")
	ErrActiveAttempt        = errors.New("task already has an active attempt")
	ErrNoActiveAttempt      = errors.New("task has no active attempt")
	ErrInvalidCommand       = errors.New("invalid task command")
	ErrVerificationRequired = errors.New("valid verified checkpoint required for task completion")
)

func ValidState(s State) bool {
	switch s {
	case StateCreated, StateReady, StateRunning, StateWaitingDependency,
		StateWaitingApproval, StatePaused, StateCompletionRequested,
		StateVerifying, StateBlocked, StateFailed, StateComplete,
		StateCancelRequested, StateCancelling, StateCompensating, StateCancelled:
		return true
	default:
		return false
	}
}

func ValidSchedulingClass(c SchedulingClass) bool {
	switch c {
	case ClassUrgentRecovery, ClassUserInteractive, ClassApprovalContinuation,
		ClassNormal, ClassBackgroundRoutine, ClassMaintenance, ClassBestEffort:
		return true
	default:
		return false
	}
}

// CanTransition describes ordinary Task lifecycle transitions. Recovery-specific
// transitions are deliberately kept out of this function so a crash does not
// accidentally become equivalent to an ordinary user/runtime transition.
func CanTransition(from, to State) bool {
	if from == to {
		return false
	}
	switch from {
	case StateCreated:
		return to == StateReady || to == StateCancelRequested
	case StateReady:
		return to == StateRunning || to == StatePaused || to == StateCancelRequested || to == StateBlocked
	case StateRunning:
		return to == StateWaitingDependency || to == StateWaitingApproval ||
			to == StatePaused || to == StateCompletionRequested ||
			to == StateBlocked || to == StateFailed || to == StateCancelRequested
	case StateWaitingDependency:
		return to == StateRunning || to == StatePaused || to == StateBlocked ||
			to == StateFailed || to == StateCancelRequested
	case StateWaitingApproval:
		return to == StateRunning || to == StatePaused || to == StateBlocked ||
			to == StateFailed || to == StateCancelRequested
	case StatePaused:
		return to == StateReady || to == StateRunning || to == StateCancelRequested || to == StateBlocked
	case StateCompletionRequested:
		return to == StateVerifying || to == StateRunning || to == StateCancelRequested || to == StateBlocked
	case StateVerifying:
		return to == StateComplete || to == StateRunning || to == StateBlocked ||
			to == StateFailed || to == StateCancelRequested
	case StateBlocked:
		return to == StateReady || to == StateFailed || to == StateCancelRequested
	case StateCancelRequested:
		return to == StateCancelling || to == StateCancelled
	case StateCancelling:
		return to == StateCompensating || to == StateCancelled || to == StateBlocked
	case StateCompensating:
		return to == StateCancelled || to == StateBlocked
	case StateFailed, StateComplete, StateCancelled:
		return false
	default:
		return false
	}
}

func ValidAttemptState(s AttemptState) bool {
	switch s {
	case AttemptCreated, AttemptQueued, AttemptRunning, AttemptWaiting,
		AttemptSucceeded, AttemptFailed, AttemptCancelled, AttemptInterrupted:
		return true
	default:
		return false
	}
}

func CanTransitionAttempt(from, to AttemptState) bool {
	if from == to {
		return false
	}
	switch from {
	case AttemptCreated:
		return to == AttemptQueued || to == AttemptRunning || to == AttemptCancelled || to == AttemptInterrupted
	case AttemptQueued:
		return to == AttemptRunning || to == AttemptCancelled || to == AttemptInterrupted
	case AttemptRunning:
		return to == AttemptWaiting || to == AttemptSucceeded || to == AttemptFailed ||
			to == AttemptCancelled || to == AttemptInterrupted
	case AttemptWaiting:
		return to == AttemptRunning || to == AttemptSucceeded || to == AttemptFailed ||
			to == AttemptCancelled || to == AttemptInterrupted
	case AttemptSucceeded, AttemptFailed, AttemptCancelled, AttemptInterrupted:
		return false
	default:
		return false
	}
}
