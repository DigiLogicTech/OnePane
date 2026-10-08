package team

import (
	"encoding/json"
	"errors"
)

type Team struct {
	ID, WorkspaceID, Name, Purpose, Status, CreatedBy string
	Configuration                                     json.RawMessage `json:"configuration"`
	Revision, CreatedAt, UpdatedAt                    int64
}
type Member struct {
	ID, TeamID, WorkspaceID, MemberKind, DisplayName, RoleName, CapabilityID, ProtocolLevel, Status string
	PrincipalID                                                                                     *string
	RoutePolicy, Config                                                                             json.RawMessage
	Ordinal                                                                                         int
	CreatedAt, UpdatedAt                                                                            int64
}
type Session struct {
	ID, WorkspaceID, TeamID, TaskID, Status, CreatedBy string
	AcceptedPlanID, GatewayTargetID                    *string
	RoundNumber, Revision, CreatedAt, UpdatedAt        int64
	Config                                             json.RawMessage
}
type ResearchSettings struct {
	PinModels                bool   `json:"pin_models"`
	DisableModelSubstitution bool   `json:"disable_model_substitution"`
	SameModelRetries         bool   `json:"same_model_retries"`
	PreserveFailedSeats      bool   `json:"preserve_failed_seats"`
	IndependentFirstPass     bool   `json:"independent_first_pass"`
	ScopedEvidence           bool   `json:"scoped_evidence"`
	RecordRawOutputs         bool   `json:"record_raw_outputs"`
	FullProvenance           bool   `json:"full_provenance"`
	RequireAllSeats          bool   `json:"require_all_seats"`
	AnonymizedCrossCritique  bool   `json:"anonymized_cross_critique"`
	SynthesisPass            bool   `json:"synthesis_pass"`
	CritiqueRounds           int    `json:"critique_rounds"`
	SynthesisMemberID        string `json:"synthesis_member_id,omitempty"`
	ChairMode string `json:"chair_mode,omitempty"`
	ChairMemberID string `json:"chair_member_id,omitempty"`
	ChairRequireApproval bool `json:"chair_require_approval"`
}

const (
	ResearchPhaseIndependent = "independent"
	ResearchPhaseCritique    = "critique"
	ResearchPhaseSynthesis   = "synthesis"
)

func NormalizeResearchSettings(in ResearchSettings) ResearchSettings {
	if in.CritiqueRounds <= 0 {
		in.CritiqueRounds = 2
	}
	if in.CritiqueRounds > 5 {
		in.CritiqueRounds = 5
	}
	return in
}

func ResearchTotalRounds(in ResearchSettings) int64 {
	in = NormalizeResearchSettings(in)
	total := int64(1 + in.CritiqueRounds)
	if in.SynthesisPass {
		total++
	}
	return total
}

func ResearchPhaseForRound(in ResearchSettings, round int64) string {
	in = NormalizeResearchSettings(in)
	if round <= 1 {
		return ResearchPhaseIndependent
	}
	if round <= int64(1+in.CritiqueRounds) {
		return ResearchPhaseCritique
	}
	if in.SynthesisPass && round == int64(2+in.CritiqueRounds) {
		return ResearchPhaseSynthesis
	}
	return ""
}
type SnapshotAgentProfile struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Revision     int64  `json:"revision,omitempty"`
}
type SnapshotMember struct {
	ID            string                `json:"id"`
	MemberKind    string                `json:"member_kind"`
	DisplayName   string                `json:"display_name"`
	RoleName      string                `json:"role_name"`
	CapabilityID  string                `json:"capability_id"`
	ProtocolLevel string                `json:"protocol_level"`
	PrincipalID   *string               `json:"principal_id,omitempty"`
	RoutePolicy   json.RawMessage       `json:"route_policy"`
	Config        json.RawMessage       `json:"config"`
	Ordinal       int                   `json:"ordinal"`
	Status        string                `json:"status"`
	Profile       *SnapshotAgentProfile `json:"profile,omitempty"`
}
type SessionSnapshot struct {
	SessionID            string           `json:"session_id"`
	TaskID               string           `json:"task_id"`
	TaskObjective        string           `json:"task_objective"`
	TeamID               string           `json:"team_id"`
	TeamRevision         int64            `json:"team_revision"`
	TeamConfiguration    json.RawMessage  `json:"team_configuration"`
	SessionConfiguration json.RawMessage  `json:"session_configuration"`
	ExecutionMode        string           `json:"execution_mode"`
	ResearchMode         bool             `json:"research_mode"`
	Research             ResearchSettings `json:"research"`
	GatewayTargetID      *string          `json:"gateway_target_id,omitempty"`
	Members               []SnapshotMember `json:"members"`
}
type SessionManifest struct {
	SessionID, WorkspaceID, TeamID, ExecutionMode, SnapshotSHA256 string
	ResearchMode                                                   bool
	Snapshot                                                       json.RawMessage
	CreatedAt                                                      int64
}
type SeatBinding struct {
	SessionID, MemberID, CandidateKind, CandidateID string
	CandidateSnapshot                               json.RawMessage
	CreatedAt                                       int64
}
type Message struct {
	ID, WorkspaceID, SessionID, Kind                    string
	AuthorMemberID, AuthorPrincipalID, ReplyToMessageID *string
	Content                                             json.RawMessage
	RoundNumber, CreatedAt                              int64
}
type Plan struct {
	ID, WorkspaceID, SessionID, Status                               string
	Version                                                          int64
	Body                                                             json.RawMessage
	ProposedByMemberID, ProposedByPrincipalID, AcceptedByPrincipalID *string
	CreatedAt                                                        int64
	AcceptedAt                                                       *int64
}
type Objection struct {
	ID, WorkspaceID, SessionID, Severity, Status, Summary string
	PlanID, RaisedByMemberID, ResolvedByPrincipalID       *string
	Detail                                                json.RawMessage
	Resolution                                            json.RawMessage
	CreatedAt                                             int64
	ResolvedAt                                            *int64
}
type Decision struct {
	ID, WorkspaceID, SessionID, DecisionType, DecidedBy string
	Body                                                json.RawMessage
	CreatedAt                                           int64
}
type TurnRequest struct {
	ID, WorkspaceID, SessionID, MemberID, Status, ResearchPhase                                                     string
	TriggerMessageID, SelectedCandidateKind, SelectedCandidateID, ResponseMessageID, BudgetReservationID, ErrorText *string
	RoundNumber, AttemptCount, CreatedAt, UpdatedAt                                                                  int64
	RetryAfter, CompletedAt                                                                                          *int64
}

type CreateTeamCommand struct{ WorkspaceID, Name, Purpose, CreatedBy string }
type UpdateConfigurationCommand struct {
	TeamID, ActorPrincipalID string
	ExpectedRevision         int64
	Configuration            json.RawMessage
}
type AddMemberCommand struct {
	TeamID, MemberKind, DisplayName, RoleName, CapabilityID, ProtocolLevel, CreatedBy string
	PrincipalID                                                                       *string
	RoutePolicy, Config                                                               json.RawMessage
	Ordinal                                                                           int
}
type StartSessionCommand struct {
	TaskID, TeamID, CreatedBy, ExecutionMode string
	GatewayTargetID                          *string
	Config                                   json.RawMessage
}
type PostMessageCommand struct {
	SessionID, AuthorPrincipalID, Kind string
	AuthorMemberID, ReplyToMessageID   *string
	Content                            json.RawMessage
}
type RequestRoundCommand struct {
	SessionID, RequestedByPrincipalID string
	TriggerMessageID                  *string
	MemberIDs                         []string
	ExactMemberSelection              bool   `json:"-"`
	ResearchPhase                     string `json:"-"`
}

type PauseResearchRoundCommand struct {
	SessionID, TurnID, ActorPrincipalID, Reason string
	RetryAfter                                  *int64
}

type RetryResearchRoundCommand struct {
	SessionID, RequestedByPrincipalID string
	Automatic                         bool `json:"-"`
}
type ProposePlanCommand struct {
	SessionID, ProposedByPrincipalID string
	ProposedByMemberID               *string
	Plan                             json.RawMessage
}
type AcceptPlanCommand struct {
	SessionID, PlanID, AcceptedByPrincipalID string
	AcceptOpenRisk                           bool
	Decision                                 json.RawMessage
}
type RaiseObjectionCommand struct {
	SessionID, Severity, Summary, ActorPrincipalID string
	PlanID, RaisedByMemberID                       *string
	Detail                                         json.RawMessage
}
type ResolveObjectionCommand struct {
	ObjectionID, Status, ResolvedByPrincipalID string
	Resolution                                 json.RawMessage
}
type ReopenCommand struct{ SessionID, RequestedByPrincipalID, Reason string }

var (
	ErrInvalid               = errors.New("invalid team command")
	ErrWorkspaceMismatch     = errors.New("team workspace mismatch")
	ErrSessionState          = errors.New("invalid team session state")
	ErrPlanRequired          = errors.New("team plan must be accepted before task admission")
	ErrOpenBlockingObjection = errors.New("open blocking team objection requires explicit human risk acceptance")
	ErrHumanRequired         = errors.New("human principal required")
	ErrManualWebCouncilExecution = errors.New("Web-only Council consultation Tasks cannot be executed")
)
