package assurance

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/assurancecriteria"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

const VerifierPrincipal = "system:assurance-verifier"

type RunStatus string

const (
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingEvidence RunStatus = "waiting_evidence"
	RunWaitingHuman    RunStatus = "waiting_human"
	RunPassed          RunStatus = "passed"
	RunFailed          RunStatus = "failed"
	RunInconclusive    RunStatus = "inconclusive"
	RunInterrupted     RunStatus = "interrupted"
)

type Run struct {
	ID, VerificationID, WorkspaceID  string
	TaskID, OperationID, WorkerRunID *string
	Status                           RunStatus
	RequiredLevel                    policy.VerificationLevel
	AchievedLevel                    *policy.VerificationLevel
	EvidenceHash                     *string
	Result                           json.RawMessage
	FailureReason                    *string
	Revision, StartedAt, UpdatedAt   int64
	CompletedAt                      *int64
}

type JSONCheck = assurancecriteria.JSONCheck
type ArtifactRequirement = assurancecriteria.ArtifactRequirement
type ObservationRequirement = assurancecriteria.ObservationRequirement
type Criteria = assurancecriteria.Criteria

type Evidence struct {
	ArtifactIDs    []string `json:"artifact_ids,omitempty"`
	ObservationIDs []string `json:"observation_ids,omitempty"`
	OperationIDs   []string `json:"operation_ids,omitempty"`
}

type Spec struct {
	AssuranceVersion int             `json:"assurance_version"`
	Source           string          `json:"source"`
	WorkerRunID      string          `json:"worker_run_id,omitempty"`
	ProposedResult   json.RawMessage `json:"proposed_result"`
	Criteria         Criteria        `json:"criteria"`
	Evidence         Evidence        `json:"evidence"`
}

type AcceptanceCommand struct {
	VerificationID string
	PrincipalID    string
	Decision       string
	Note           string
}

type TickResult struct {
	VerificationID string
	RunID          string
	Status         RunStatus
	AchievedLevel  *policy.VerificationLevel
	Message        string
}

var (
	ErrInvalidSpec          = errors.New("invalid assurance specification")
	ErrEvidenceMissing      = errors.New("required verification evidence missing")
	ErrEvidenceMismatch     = errors.New("verification evidence does not satisfy criteria")
	ErrHumanAcceptance      = errors.New("V5 requires bound human acceptance")
	ErrAcceptanceIneligible = errors.New("principal is not eligible to accept V5 verification")
	ErrEvidenceChanged      = errors.New("verification evidence changed after acceptance")
)
