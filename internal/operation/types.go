package operation

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type State string

const (
	StateProposed              State = "proposed"
	StateAuthorized            State = "authorized"
	StatePrepared              State = "prepared"
	StateExecuting             State = "executing"
	StateObserving             State = "observing"
	StateVerified              State = "verified"
	StateCommitted             State = "committed"
	StateDenied                State = "denied"
	StateFailed                State = "failed"
	StateAborted               State = "aborted"
	StateUnknownOutcome        State = "unknown_outcome"
	StateBlockedUnknownOutcome State = "blocked_unknown_outcome"
	StateCompensating          State = "compensating"
	StateCompensated           State = "compensated"
	StateCompensationFailed    State = "compensation_failed"
)

type Operation struct {
	ID                      string
	WorkspaceID             string
	TaskID                  *string
	AttemptID               *string
	PrincipalID             string
	CompensatesOperationID  *string
	IdempotencyKey          string
	State                   State
	CapabilityID            string
	ResourceRef             string
	ToolID                  string
	ToolVersion             string
	AdapterID               string
	AdapterVersion          string
	DesiredState            json.RawMessage
	Precondition            json.RawMessage
	Reconciliation          json.RawMessage
	Compensation            json.RawMessage
	PolicyRevision          int64
	InputHash               string
	CapabilityLeaseID       *string
	CapabilityLeaseRevision *int64
	ResourceLeaseID         *string
	GlobalResourceLeaseID   *string
	RequiredVerification    *policy.VerificationLevel
	RequiredApproval        *policy.ApprovalLevel
	Revision                int64
	CreatedAt               int64
	UpdatedAt               int64
}

type Receipt struct {
	ID                string
	OperationID       string
	ReceiptType       string
	ExternalRequestID *string
	Receipt           json.RawMessage
	IntegrityHash     string
	CreatedAt         int64
}

type ResourceLease struct {
	ID          string
	WorkspaceID string
	TaskID      *string
	ResourceRef string
	LeaseMode   string
	Status      string
	AcquiredAt  int64
	ExpiresAt   int64
	ReleasedAt  *int64
	Revision    int64
}

type PrepareCommand struct {
	WorkspaceID            string
	TaskID                 *string
	AttemptID              *string
	PrincipalID            string
	CapabilityLeaseID      string
	IdempotencyKey         string
	ToolID                 string
	ToolVersion            string
	ResourceRef            string
	Input                  json.RawMessage
	DesiredState           json.RawMessage
	Precondition           json.RawMessage
	Reconciliation         json.RawMessage
	Compensation           json.RawMessage
	CompensatesOperationID *string
	ResourceLeaseTTLMillis int64
	ActorPrincipalID       *string
	RequestID              *string
	TraceID                *string
}

type CompensateCommand struct {
	OriginalOperationID    string
	PrincipalID            string
	CapabilityLeaseID      string
	IdempotencyKey         string
	ResourceLeaseTTLMillis int64
	ActorPrincipalID       *string
	RequestID              *string
	TraceID                *string
}

type ContinueApprovedCommand struct {
	OperationID            string
	ExpectedRevision       int64
	ApprovalID             string
	ResourceLeaseTTLMillis int64
	ActorPrincipalID       *string
	RequestID              *string
	TraceID                *string
}

type ExecuteCommand struct {
	OperationID      string
	ExpectedRevision int64
	LeaseID          string
	Input            json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type CommitCommand struct {
	OperationID      string
	ExpectedRevision int64
	VerificationID   string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type AbortCommand struct {
	OperationID      string
	ExpectedRevision int64
	Reason           string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

var (
	ErrInvalidCommand           = errors.New("invalid operation command")
	ErrInvalidTransition        = errors.New("invalid operation transition")
	ErrRevisionConflict         = errors.New("operation revision conflict")
	ErrIdempotencyConflict      = errors.New("operation idempotency conflict")
	ErrMutationToolRequired     = errors.New("operation requires a mutating or external-send tool")
	ErrPolicyDenied             = errors.New("operation denied by policy")
	ErrApprovalRequired         = errors.New("operation requires approval subsystem")
	ErrResourceBusy             = errors.New("resource already has an active mutation lease")
	ErrResourceLeaseInvalid     = errors.New("operation resource lease is not active")
	ErrExecutionPermit          = errors.New("invalid or expired execution permit")
	ErrUnknownOutcome           = errors.New("operation has unknown external outcome")
	ErrVerificationRequired     = errors.New("operation requires passing verification")
	ErrVerificationInsufficient = errors.New("operation verification level is insufficient")
)

func ValidState(s State) bool {
	switch s {
	case StateProposed, StateAuthorized, StatePrepared, StateExecuting, StateObserving,
		StateVerified, StateCommitted, StateDenied, StateFailed, StateAborted,
		StateUnknownOutcome, StateBlockedUnknownOutcome, StateCompensating,
		StateCompensated, StateCompensationFailed:
		return true
	default:
		return false
	}
}

func CanTransition(from, to State) bool {
	if from == to {
		return false
	}
	switch from {
	case StateProposed:
		return to == StateAuthorized || to == StateDenied || to == StateFailed || to == StateAborted
	case StateAuthorized:
		return to == StatePrepared || to == StateDenied || to == StateFailed || to == StateAborted
	case StatePrepared:
		return to == StateExecuting || to == StateAborted || to == StateFailed || to == StateBlockedUnknownOutcome
	case StateExecuting:
		return to == StateObserving || to == StateUnknownOutcome || to == StateFailed
	case StateObserving:
		return to == StateVerified || to == StateUnknownOutcome || to == StateFailed
	case StateVerified:
		return to == StateCommitted || to == StateUnknownOutcome || to == StateFailed
	case StateUnknownOutcome:
		return to == StateBlockedUnknownOutcome
	case StateBlockedUnknownOutcome:
		return to == StatePrepared || to == StateObserving || to == StateFailed || to == StateCompensating
	case StateCompensating:
		return to == StateCompensated || to == StateCompensationFailed || to == StateUnknownOutcome
	default:
		return false
	}
}

type RecoveryCommand struct {
	OperationID      string
	ExpectedRevision int64
	VerificationID   string
	LeaseTTLMillis   int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type RecoverySweepResult struct {
	Interrupted []string
	Blocked     []string
}
