package artifact

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusQuarantined Status = "quarantined"
	StatusCorrupted   Status = "corrupted"
	StatusArchived    Status = "archived"
)

type Artifact struct {
	ID          string
	WorkspaceID string
	ProjectID   *string
	ContentHash string
	MediaType   string
	SizeBytes   int64
	StorageRef  string
	Label       policy.DataLabel
	Status      Status
	Metadata    json.RawMessage
	CreatedBy   *string
	CreatedAt   int64
}

type CreateCommand struct {
	WorkspaceID      string
	ProjectID        *string
	MediaType        string
	Label            policy.DataLabel
	Status           Status
	Metadata         json.RawMessage
	CreatedBy        *string
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

var (
	ErrInvalidCommand    = errors.New("invalid artifact command")
	ErrProjectWorkspace  = errors.New("project belongs to a different workspace")
	ErrWorkspaceInactive = errors.New("workspace is not active")
	ErrCreatorIneligible = errors.New("artifact creator is not eligible in workspace")
	ErrCorruptContent    = errors.New("artifact content hash or size mismatch")
	ErrUnsafeStorageRef  = errors.New("unsafe artifact storage reference")
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusActive, StatusQuarantined, StatusCorrupted, StatusArchived:
		return true
	default:
		return false
	}
}
