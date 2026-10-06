package verification

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type Status string

const (
	StatusPending      Status = "pending"
	StatusPass         Status = "pass"
	StatusFail         Status = "fail"
	StatusInconclusive Status = "inconclusive"
	StatusStale        Status = "stale"
)

type Verification struct {
	ID            string
	WorkspaceID   string
	TaskID        *string
	OperationID   *string
	SubjectRef    string
	RequiredLevel policy.VerificationLevel
	AchievedLevel *policy.VerificationLevel
	Status        Status
	Spec          json.RawMessage
	Result        json.RawMessage
	VerifiedBy    *string
	StartedAt     int64
	CompletedAt   *int64
	Revision      int64
}

type CheckpointStatus string

const (
	CheckpointValid      CheckpointStatus = "valid"
	CheckpointStale      CheckpointStatus = "stale"
	CheckpointSuperseded CheckpointStatus = "superseded"
)

type Checkpoint struct {
	ID             string
	WorkspaceID    string
	TaskID         string
	VerificationID string
	State          json.RawMessage
	Status         CheckpointStatus
	CreatedAt      int64
	InvalidatedAt  *int64
}

type CreateCommand struct {
	WorkspaceID      string
	TaskID           *string
	OperationID      *string
	SubjectRef       string
	RequiredLevel    policy.VerificationLevel
	Spec             json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type ResolveCommand struct {
	VerificationID   string
	ExpectedRevision int64
	Status           Status
	AchievedLevel    *policy.VerificationLevel
	Result           json.RawMessage
	VerifiedBy       string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type StaleCommand struct {
	VerificationID   string
	ExpectedRevision int64
	Reason           string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type CheckpointCommand struct {
	WorkspaceID      string
	TaskID           string
	VerificationID   string
	State            json.RawMessage
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type InvalidateCheckpointCommand struct {
	CheckpointID     string
	Reason           string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

var (
	ErrInvalidCommand         = errors.New("invalid verification command")
	ErrRevisionConflict       = errors.New("verification revision conflict")
	ErrInvalidTransition      = errors.New("invalid verification transition")
	ErrInsufficientLevel      = errors.New("achieved verification level is below required level")
	ErrVerifierIneligible     = errors.New("verifier is not eligible in workspace")
	ErrWorkspaceMismatch      = errors.New("verification subject belongs to a different workspace")
	ErrVerificationNotPassed  = errors.New("checkpoint requires a passed verification")
	ErrUnknownMutationOutcome = errors.New("task has unknown mutation outcome")
	ErrCheckpointInvalid      = errors.New("checkpoint is not valid")
)

func ValidStatus(s Status) bool {
	return s == StatusPending || s == StatusPass || s == StatusFail || s == StatusInconclusive || s == StatusStale
}
func CanResolve(from, to Status) bool {
	if from == StatusPending {
		return to == StatusPass || to == StatusFail || to == StatusInconclusive
	}
	return (from == StatusPass || from == StatusFail || from == StatusInconclusive) && to == StatusStale
}
func ValidLevel(v policy.VerificationLevel) bool { return LevelRank(v) >= 0 }
func LevelRank(v policy.VerificationLevel) int {
	switch v {
	case policy.VerificationV0:
		return 0
	case policy.VerificationV1:
		return 1
	case policy.VerificationV2:
		return 2
	case policy.VerificationV3:
		return 3
	case policy.VerificationV4:
		return 4
	case policy.VerificationV5:
		return 5
	default:
		return -1
	}
}
