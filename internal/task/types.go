package task

import "encoding/json"

type State string

const (
	StateCreated             State = "created"
	StateReady               State = "ready"
	StateRunning             State = "running"
	StateWaitingDependency   State = "waiting_dependency"
	StateWaitingApproval     State = "waiting_approval"
	StatePaused              State = "paused"
	StateCompletionRequested State = "completion_requested"
	StateVerifying           State = "verifying"
	StateBlocked             State = "blocked"
	StateFailed              State = "failed"
	StateComplete            State = "complete"
	StateCancelRequested     State = "cancel_requested"
	StateCancelling          State = "cancelling"
	StateCompensating        State = "compensating"
	StateCancelled           State = "cancelled"
)

type SchedulingClass string

const (
	ClassUrgentRecovery       SchedulingClass = "urgent_recovery"
	ClassUserInteractive      SchedulingClass = "user_interactive"
	ClassApprovalContinuation SchedulingClass = "approval_continuation"
	ClassNormal               SchedulingClass = "normal_task"
	ClassBackgroundRoutine    SchedulingClass = "background_routine"
	ClassMaintenance          SchedulingClass = "maintenance"
	ClassBestEffort           SchedulingClass = "best_effort"
)

type AttemptState string

const (
	AttemptCreated     AttemptState = "created"
	AttemptQueued      AttemptState = "queued"
	AttemptRunning     AttemptState = "running"
	AttemptWaiting     AttemptState = "waiting"
	AttemptSucceeded   AttemptState = "succeeded"
	AttemptFailed      AttemptState = "failed"
	AttemptCancelled   AttemptState = "cancelled"
	AttemptInterrupted AttemptState = "interrupted"
)

type Task struct {
	ID                string
	WorkspaceID       string
	ProjectID         *string
	ArtifactSessionID *string
	PlanID            *string
	ParentTaskID      *string
	Objective         string
	State             State
	SchedulingClass   SchedulingClass
	Priority          int
	Completion        json.RawMessage
	Result            json.RawMessage
	Revision          int64
	ReadyAt           *int64
	CancelRequestedAt *int64
	CreatedAt         int64
	UpdatedAt         int64
}

type Attempt struct {
	ID                 string
	TaskID             string
	AttemptNumber      int64
	WorkerPrincipalID  *string
	State              AttemptState
	RecoverySnapshotID *string
	StartedAt          *int64
	EndedAt            *int64
	Metadata           json.RawMessage
}

type CreateCommand struct {
	WorkspaceID       string
	ProjectID         *string
	ArtifactSessionID *string
	PlanID            *string
	ParentTaskID      *string
	Objective         string
	SchedulingClass   SchedulingClass
	Priority          int
	Completion        json.RawMessage
	ActorPrincipalID  *string
	RequestID         *string
	TraceID           *string
}

type TransitionCommand struct {
	TaskID           string
	ExpectedRevision int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	Reason           string
}

type StartCommand struct {
	TaskID            string
	ExpectedRevision  int64
	WorkerPrincipalID *string
	ActorPrincipalID  *string
	RequestID         *string
	TraceID           *string
	Metadata          json.RawMessage
}

type CompleteCommand struct {
	TaskID           string
	ExpectedRevision int64
	CheckpointID     string
	Result           json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}
