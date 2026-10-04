package inference

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type RequestStatus string

const (
	RequestCreated        RequestStatus = "created"
	RequestRouted         RequestStatus = "routed"
	RequestBudgetReserved RequestStatus = "budget_reserved"
	RequestDispatched     RequestStatus = "dispatched"
	RequestExecuting      RequestStatus = "executing"
	RequestSucceeded      RequestStatus = "succeeded"
	RequestRateLimited    RequestStatus = "rate_limited"
	RequestFailed         RequestStatus = "failed"
	RequestCancelled      RequestStatus = "cancelled"
	RequestUnknown        RequestStatus = "unknown"
)

type InferenceRequest struct {
	ID                   string
	WorkspaceID          string
	TaskID               *string
	SessionID            *string
	PrincipalID          string
	DeploymentID         *string
	ProviderConnectionID *string
	OriginNodeID         *string
	ExecutionNodeID      *string
	Status               RequestStatus
	CapabilityJSON       json.RawMessage
	ContextManifestJSON  json.RawMessage
	ClassificationJSON   json.RawMessage
	RequestJSON          json.RawMessage
	ResponseArtifactID   *string
	UsageJSON            json.RawMessage
	ErrorCode            *string
	CreatedAt            int64
	UpdatedAt            int64
	CompletedAt          *int64
}

type ExecuteCommand struct {
	WorkspaceID            string
	TaskID                 *string
	SessionID              *string
	PrincipalID            string
	DeploymentID           string
	CapabilityJSON         json.RawMessage
	ContextManifestJSON    json.RawMessage
	ClassificationMetadata json.RawMessage
	InputLabel             policy.DataLabel
	RequestJSON            json.RawMessage
	ActorPrincipalID       *string
	RequestID              *string
	TraceID                *string
	BudgetReservationID    *string
}

var (
	ErrExecutionUnavailable  = errors.New("inference execution is not configured")
	ErrDeploymentNotReady    = errors.New("model deployment is not ready")
	ErrPrincipalIneligible   = errors.New("inference principal is not eligible")
	ErrTaskWorkspace         = errors.New("task belongs to a different workspace")
	ErrInformationFlow       = errors.New("information flow denied")
	ErrRemoteNodeUnsupported = errors.New("remote-node inference requires an active paired node")
	ErrTransportUnavailable  = errors.New("inference transport is unavailable")
	ErrBudgetRequired        = errors.New("budget reservation required for protected inference route")
	ErrBudgetUnavailable     = errors.New("budget enforcement is unavailable")
)

func ValidRequestStatus(s RequestStatus) bool {
	switch s {
	case RequestCreated, RequestRouted, RequestBudgetReserved, RequestDispatched,
		RequestExecuting, RequestSucceeded, RequestRateLimited, RequestFailed,
		RequestCancelled, RequestUnknown:
		return true
	default:
		return false
	}
}

func CanRequestTransition(from, to RequestStatus) bool {
	if from == to || !ValidRequestStatus(from) || !ValidRequestStatus(to) {
		return false
	}
	switch from {
	case RequestCreated:
		return to == RequestRouted || to == RequestCancelled || to == RequestFailed
	case RequestRouted:
		return to == RequestBudgetReserved || to == RequestDispatched || to == RequestCancelled || to == RequestFailed
	case RequestBudgetReserved:
		return to == RequestDispatched || to == RequestCancelled || to == RequestFailed
	case RequestDispatched:
		return to == RequestExecuting || to == RequestCancelled || to == RequestFailed || to == RequestUnknown
	case RequestExecuting:
		return to == RequestSucceeded || to == RequestRateLimited || to == RequestFailed || to == RequestCancelled || to == RequestUnknown
	default:
		return false
	}
}
