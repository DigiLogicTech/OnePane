package team

import (
	"encoding/json"
	"errors"
)

type Team struct {
	ID, WorkspaceID, Name, Purpose, Status, CreatedBy string
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
	ID, WorkspaceID, SessionID, MemberID, Status                                                                    string
	TriggerMessageID, SelectedCandidateKind, SelectedCandidateID, ResponseMessageID, BudgetReservationID, ErrorText *string
	CreatedAt, UpdatedAt                                                                                            int64
	CompletedAt                                                                                                     *int64
}

type CreateTeamCommand struct{ WorkspaceID, Name, Purpose, CreatedBy string }
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
)
