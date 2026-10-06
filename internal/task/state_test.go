package task

import "testing"

func TestTaskStateTransitionsExhaustive(t *testing.T) {
	states := []State{
		StateCreated, StateReady, StateRunning, StateWaitingDependency,
		StateWaitingApproval, StatePaused, StateCompletionRequested,
		StateVerifying, StateBlocked, StateFailed, StateComplete,
		StateCancelRequested, StateCancelling, StateCompensating, StateCancelled,
	}

	allowed := map[State]map[State]bool{
		StateCreated: {StateReady: true, StateCancelRequested: true},
		StateReady:   {StateRunning: true, StatePaused: true, StateCancelRequested: true, StateBlocked: true},
		StateRunning: {
			StateWaitingDependency: true, StateWaitingApproval: true, StatePaused: true,
			StateCompletionRequested: true, StateBlocked: true, StateFailed: true, StateCancelRequested: true,
		},
		StateWaitingDependency:   {StateRunning: true, StatePaused: true, StateBlocked: true, StateFailed: true, StateCancelRequested: true},
		StateWaitingApproval:     {StateRunning: true, StatePaused: true, StateBlocked: true, StateFailed: true, StateCancelRequested: true},
		StatePaused:              {StateReady: true, StateRunning: true, StateCancelRequested: true, StateBlocked: true},
		StateCompletionRequested: {StateVerifying: true, StateRunning: true, StateCancelRequested: true, StateBlocked: true},
		StateVerifying:           {StateComplete: true, StateRunning: true, StateBlocked: true, StateFailed: true, StateCancelRequested: true},
		StateBlocked:             {StateReady: true, StateFailed: true, StateCancelRequested: true},
		StateCancelRequested:     {StateCancelling: true, StateCancelled: true},
		StateCancelling:          {StateCompensating: true, StateCancelled: true, StateBlocked: true},
		StateCompensating:        {StateCancelled: true, StateBlocked: true},
	}

	for _, from := range states {
		for _, to := range states {
			want := allowed[from][to]
			if got := CanTransition(from, to); got != want {
				t.Fatalf("CanTransition(%s,%s)=%v want %v", from, to, got, want)
			}
		}
	}
}

func TestAttemptStateTransitionsExhaustive(t *testing.T) {
	states := []AttemptState{
		AttemptCreated, AttemptQueued, AttemptRunning, AttemptWaiting,
		AttemptSucceeded, AttemptFailed, AttemptCancelled, AttemptInterrupted,
	}
	allowed := map[AttemptState]map[AttemptState]bool{
		AttemptCreated: {AttemptQueued: true, AttemptRunning: true, AttemptCancelled: true, AttemptInterrupted: true},
		AttemptQueued:  {AttemptRunning: true, AttemptCancelled: true, AttemptInterrupted: true},
		AttemptRunning: {AttemptWaiting: true, AttemptSucceeded: true, AttemptFailed: true, AttemptCancelled: true, AttemptInterrupted: true},
		AttemptWaiting: {AttemptRunning: true, AttemptSucceeded: true, AttemptFailed: true, AttemptCancelled: true, AttemptInterrupted: true},
	}
	for _, from := range states {
		for _, to := range states {
			want := allowed[from][to]
			if got := CanTransitionAttempt(from, to); got != want {
				t.Fatalf("CanTransitionAttempt(%s,%s)=%v want %v", from, to, got, want)
			}
		}
	}
}
