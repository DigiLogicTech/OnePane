package observation

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type Observation struct {
	ID                string
	WorkspaceID       string
	SubjectRef        string
	ObservationType   string
	ProbeToolID       string
	ProbeToolVersion  string
	SourcePrincipalID *string
	AdapterID         *string
	AdapterVersion    *string
	Value             json.RawMessage
	Label             policy.DataLabel
	IntegrityHash     string
	ObservedAt        int64
	CreatedAt         int64
}

type RecordCommand struct {
	WorkspaceID       string
	SubjectRef        string
	ObservationType   string
	ProbeToolID       string
	ProbeToolVersion  string
	SourcePrincipalID *string
	AdapterID         *string
	AdapterVersion    *string
	Value             json.RawMessage
	Label             policy.DataLabel
	ObservedAt        int64
	ActorPrincipalID  *string
	RequestID         *string
	TraceID           *string
}

var (
	ErrInvalidCommand      = errors.New("invalid observation command")
	ErrWorkspaceInactive   = errors.New("workspace is not active")
	ErrSourceIneligible    = errors.New("source principal is not eligible in workspace")
	ErrSourceActorMismatch = errors.New("observation source principal must match recording actor")
)
