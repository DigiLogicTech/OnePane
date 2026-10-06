package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type Status string

const (
	StatusCreated     Status = "created"
	StatusAuthorized  Status = "authorized"
	StatusRunning     Status = "running"
	StatusSucceeded   Status = "succeeded"
	StatusFailed      Status = "failed"
	StatusTimedOut    Status = "timed_out"
	StatusCancelled   Status = "cancelled"
	StatusInterrupted Status = "interrupted"
)

type Definition struct {
	ID                  string
	Version             string
	CapabilityID        string
	Mode                authority.ActionMode
	AdapterID           string
	AdapterVersion      string
	Risk                policy.RiskLevel
	MinimumVerification policy.VerificationLevel
	MinimumApproval     policy.ApprovalLevel
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" ||
		strings.TrimSpace(d.CapabilityID) == "" || strings.TrimSpace(d.AdapterID) == "" ||
		strings.TrimSpace(d.AdapterVersion) == "" {
		return fmt.Errorf("%w: tool identity, capability and adapter identity are required", ErrInvalidDefinition)
	}
	if !authority.ValidActionMode(d.Mode) {
		return fmt.Errorf("%w: invalid mode %q", ErrInvalidDefinition, d.Mode)
	}
	switch d.Risk {
	case policy.RiskLow, policy.RiskMedium, policy.RiskHigh, policy.RiskCritical:
	default:
		return fmt.Errorf("%w: invalid risk %q", ErrInvalidDefinition, d.Risk)
	}
	switch d.MinimumVerification {
	case policy.VerificationV0, policy.VerificationV1, policy.VerificationV2,
		policy.VerificationV3, policy.VerificationV4, policy.VerificationV5:
	default:
		return fmt.Errorf("%w: invalid minimum verification %q", ErrInvalidDefinition, d.MinimumVerification)
	}
	switch d.MinimumApproval {
	case policy.ApprovalNone, policy.ApprovalApprover, policy.ApprovalAdmin:
	default:
		return fmt.Errorf("%w: invalid minimum approval %q", ErrInvalidDefinition, d.MinimumApproval)
	}
	return nil
}

type Invocation struct {
	ID             string
	WorkspaceID    string
	TaskID         *string
	AttemptID      *string
	PrincipalID    string
	OperationID    *string
	ToolID         string
	ToolVersion    string
	AdapterID      string
	AdapterVersion string
	Mode           authority.ActionMode
	ResourceRef    *string
	InputHash      string
	Status         Status
	Summary        *string
	Result         json.RawMessage
	ErrorCode      *string
	StartedAt      *int64
	EndedAt        *int64
	CreatedAt      int64
	UpdatedAt      int64
}

type InvokeCommand struct {
	WorkspaceID      string
	TaskID           *string
	AttemptID        *string
	PrincipalID      string
	LeaseID          string
	OperationID      *string
	ToolID           string
	ToolVersion      string
	ResourceRef      string
	Input            json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	// ExecutionPermit is an ephemeral, single-use token minted by the
	// OperationCoordinator. It is required for mutate/external_send modes and
	// is never persisted in ToolInvocation or Operation state.
	ExecutionPermit string
}

type MutationPermitCheck struct {
	OperationID    string
	Permit         string
	WorkspaceID    string
	TaskID         *string
	AttemptID      *string
	PrincipalID    string
	LeaseID        string
	ToolID         string
	ToolVersion    string
	AdapterID      string
	AdapterVersion string
	ResourceRef    string
	InputHash      string
}

type MutationPermitValidator interface {
	ValidateAndConsume(context.Context, MutationPermitCheck) error
}

type AdapterRequest struct {
	InvocationID string
	WorkspaceID  string
	TaskID       *string
	AttemptID    *string
	ToolID       string
	ToolVersion  string
	ResourceRef  string
	Input        json.RawMessage
}

type AdapterResult struct {
	Summary string
	Result  json.RawMessage
}

type Adapter interface {
	ID() string
	Version() string
	Invoke(context.Context, AdapterRequest) (AdapterResult, error)
}

type EventMeta struct {
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type Transition struct {
	InvocationID string
	From         Status
	To           Status
	Summary      *string
	Result       json.RawMessage
	ErrorCode    *string
	At           int64
	Details      map[string]any
}

var (
	ErrInvalidDefinition            = errors.New("invalid tool definition")
	ErrDuplicateTool                = errors.New("tool already registered")
	ErrToolNotFound                 = errors.New("tool not found")
	ErrAdapterIdentityMismatch      = errors.New("adapter identity does not match tool definition")
	ErrInvalidCommand               = errors.New("invalid tool invocation command")
	ErrInvalidTransition            = errors.New("invalid tool invocation transition")
	ErrPolicyDenied                 = errors.New("tool invocation denied by policy")
	ErrApprovalRequired             = errors.New("tool invocation requires approval")
	ErrOperationCoordinatorRequired = errors.New("mutating tool execution requires OperationCoordinator")
	ErrSandboxRunnerRequired        = errors.New("sandboxed tool execution requires sandbox-runner")
	ErrAuthorityConsumption         = errors.New("capability lease could not be consumed")
	ErrExecutionContext             = errors.New("tool execution context is invalid")
	ErrTaskWorkspaceMismatch        = errors.New("task belongs to a different workspace")
	ErrAttemptRequiresTask          = errors.New("attempt requires task")
	ErrAttemptTaskMismatch          = errors.New("attempt belongs to a different task")
	ErrAttemptNotRunning            = errors.New("task attempt is not running")
	ErrAttemptPrincipalMismatch     = errors.New("task attempt worker does not match invoking principal")
	ErrAdapterFailure               = errors.New("tool adapter failed")
	ErrAdapterInvalidResult         = errors.New("tool adapter returned invalid result")
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusCreated, StatusAuthorized, StatusRunning, StatusSucceeded,
		StatusFailed, StatusTimedOut, StatusCancelled, StatusInterrupted:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	switch from {
	case StatusCreated:
		return to == StatusAuthorized || to == StatusFailed || to == StatusCancelled
	case StatusAuthorized:
		return to == StatusRunning || to == StatusFailed || to == StatusCancelled
	case StatusRunning:
		return to == StatusSucceeded || to == StatusFailed || to == StatusTimedOut ||
			to == StatusCancelled || to == StatusInterrupted
	default:
		return false
	}
}

// KnownAdapterFailure marks an adapter error that is known to have occurred
// before any external side effect. Mutation callers may safely treat it as a
// known failure rather than UNKNOWN_OUTCOME.
type KnownAdapterFailure struct{ Err error }

func (e KnownAdapterFailure) Error() string {
	if e.Err == nil {
		return "known adapter failure"
	}
	return e.Err.Error()
}
func (e KnownAdapterFailure) Unwrap() error { return e.Err }

func KnownFailure(err error) error { return KnownAdapterFailure{Err: err} }

// AdapterExecutionError preserves whether a failing adapter call may have
// produced an external side effect. Unknown is deliberately the default.
type AdapterExecutionError struct {
	Cause          error
	UnknownOutcome bool
}

func (e *AdapterExecutionError) Error() string {
	return fmt.Sprintf("%v: %v", ErrAdapterFailure, e.Cause)
}
func (e *AdapterExecutionError) Unwrap() error { return ErrAdapterFailure }

func AdapterOutcomeUnknown(err error) bool {
	var e *AdapterExecutionError
	return errors.As(err, &e) && e.UnknownOutcome
}
