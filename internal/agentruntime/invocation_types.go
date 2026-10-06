package agentruntime

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type InvocationStatus string

const (
	InvocationCreated    InvocationStatus = "created"
	InvocationAuthorized InvocationStatus = "authorized"
	InvocationDispatched InvocationStatus = "dispatched"
	InvocationExecuting  InvocationStatus = "executing"
	InvocationSucceeded  InvocationStatus = "succeeded"
	InvocationFailed     InvocationStatus = "failed"
	InvocationUnknown    InvocationStatus = "unknown"
	InvocationCancelled  InvocationStatus = "cancelled"
)

type Invocation struct {
	ID                 string
	WorkspaceID        string
	TaskID             *string
	AttemptID          *string
	PrincipalID        string
	ConnectionID       string
	Status             InvocationStatus
	RequestJSON        json.RawMessage
	ResponseArtifactID *string
	ErrorCode          *string
	Revision           int64
	CreatedAt          int64
	UpdatedAt          int64
	CompletedAt        *int64
}

type InvokeCommand struct {
	WorkspaceID            string
	TaskID                 *string
	AttemptID              *string
	PrincipalID            string
	ConnectionID           string
	Role                   string
	Objective              string
	Constraints            json.RawMessage
	Context                []agentprotocol.ContextSection
	ContextManifest        json.RawMessage
	PermittedProposalTypes []agentprotocol.ProposalType
	InputLabel             policy.DataLabel
	ActorPrincipalID       *string
	RequestID              *string
	TraceID                *string
	BudgetReservationID    *string
}

type InvokeResult struct {
	Invocation Invocation
	Response   *agentprotocol.Response
}

var (
	ErrRuntimeExecutionUnavailable     = errors.New("agent runtime execution is not configured")
	ErrRuntimeNotSchedulable           = errors.New("agent runtime is not schedulable")
	ErrRuntimeInformationFlow          = errors.New("agent runtime information flow denied")
	ErrTrustedNodeTransportUnavailable = errors.New("trusted-node agent runtime transport requires paired mTLS federation")
	ErrRuntimeBudgetRequired           = errors.New("budget reservation required for protected agent runtime route")
	ErrRuntimeBudgetUnavailable        = errors.New("budget enforcement is unavailable for agent runtime")
)

func ValidInvocationStatus(s InvocationStatus) bool {
	switch s {
	case InvocationCreated, InvocationAuthorized, InvocationDispatched, InvocationExecuting, InvocationSucceeded, InvocationFailed, InvocationUnknown, InvocationCancelled:
		return true
	default:
		return false
	}
}
func CanInvocationTransition(from, to InvocationStatus) bool {
	if from == to || !ValidInvocationStatus(from) || !ValidInvocationStatus(to) {
		return false
	}
	switch from {
	case InvocationCreated:
		return to == InvocationAuthorized || to == InvocationFailed || to == InvocationCancelled
	case InvocationAuthorized:
		return to == InvocationDispatched || to == InvocationFailed || to == InvocationCancelled
	case InvocationDispatched:
		return to == InvocationExecuting || to == InvocationFailed || to == InvocationUnknown || to == InvocationCancelled
	case InvocationExecuting:
		return to == InvocationSucceeded || to == InvocationFailed || to == InvocationUnknown || to == InvocationCancelled
	default:
		return false
	}
}
