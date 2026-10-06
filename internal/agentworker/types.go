package agentworker

import (
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

const (
	AuthorityPrincipal = "system:agent-authority"
	WorkerPrincipal    = "system:agent-worker"
	VerifierPrincipal  = "system:agent-verifier"
)

type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunWaiting     RunStatus = "waiting"
	RunBlocked     RunStatus = "blocked"
	RunSucceeded   RunStatus = "succeeded"
	RunFailed      RunStatus = "failed"
	RunInterrupted RunStatus = "interrupted"
)

type Run struct {
	ID                string
	WorkspaceID       string
	TaskID            string
	AttemptID         string
	WorkerPrincipalID string
	Status            RunStatus
	RoleName          string
	CapabilityID      string
	ProtocolLevel     string
	MaxSteps          int64
	StepCount         int64
	MaxReplans        int64
	ReplanCount       int64
	MaxEscalations    int64
	EscalationCount   int64
	RoutePolicy       json.RawMessage
	Continuation      json.RawMessage
	LastCandidateKind *string
	LastCandidateID   *string
	LastError         *string
	Revision          int64
	StartedAt         int64
	UpdatedAt         int64
	CompletedAt       *int64
}

type TickResult struct {
	TaskID   string `json:"task_id"`
	RunID    string `json:"run_id,omitempty"`
	Status   string `json:"status"`
	Proposal string `json:"proposal,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Config struct {
	MaxSteps        int64
	MaxReplans      int64
	MaxEscalations  int64
	ContextMaxBytes int
	RoleName        string
	CapabilityID    string
	ProtocolLevel   string
}

func DefaultConfig() Config {
	return Config{MaxSteps: 24, MaxReplans: 3, MaxEscalations: 3, ContextMaxBytes: 64 << 10, RoleName: "general", CapabilityID: "inference.general", ProtocolLevel: "L1"}
}

type routePolicy struct {
	RoutingEnabled              bool     `json:"routing_enabled"`
	AllowDelegation             bool     `json:"allow_delegation"`
	AllowRemote                 bool     `json:"allow_remote"`
	AllowSubscriptionUsage      bool     `json:"allow_subscription_usage"`
	AllowPotentialMonetarySpend bool     `json:"allow_potential_monetary_spend"`
	PreferZeroIncrementalCost   bool     `json:"prefer_zero_incremental_cost"`
	RequireZeroIncrementalCost  bool     `json:"require_zero_incremental_cost"`
	AllowUntested               bool     `json:"allow_untested"`
	AllowLimited                bool     `json:"allow_limited"`
	AllowMediated               bool     `json:"allow_mediated"`
	AllowDegraded               bool     `json:"allow_degraded"`
	IncludeCandidateIDs         []string `json:"include_candidate_ids,omitempty"`
	ExcludedCandidateIDs        []string `json:"excluded_candidate_ids,omitempty"`
	BudgetAccountID             string   `json:"budget_account_id,omitempty"`
	BudgetReserveAmount         int64    `json:"budget_reserve_amount,omitempty"`
}

func defaultRoutePolicy() routePolicy {
	return routePolicy{RoutingEnabled: true, AllowDelegation: true, AllowRemote: true, PreferZeroIncrementalCost: true, AllowMediated: true, AllowDegraded: true}
}

type toolProposal struct {
	ToolID         string          `json:"tool_id"`
	ToolVersion    string          `json:"tool_version"`
	ResourceRef    string          `json:"resource_ref"`
	Input          json.RawMessage `json:"input"`
	DesiredState   json.RawMessage `json:"desired_state,omitempty"`
	Precondition   json.RawMessage `json:"precondition,omitempty"`
	Reconciliation json.RawMessage `json:"reconciliation,omitempty"`
	Compensation   json.RawMessage `json:"compensation,omitempty"`
}

type delegateProposal struct {
	Objective  string          `json:"objective"`
	Completion json.RawMessage `json:"completion,omitempty"`
}

type replanProposal struct {
	Reason       string `json:"reason,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type completeProposal struct {
	Result   json.RawMessage `json:"result"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}
type humanProposal struct {
	Question string `json:"question"`
}
type escalateProposal struct {
	Reason string `json:"reason,omitempty"`
}
type waitProposal struct {
	Reason string `json:"reason,omitempty"`
}
type failProposal struct {
	Reason string `json:"reason"`
}

var (
	ErrInvalidWorkerState = errors.New("invalid agent worker state")
	ErrStepLimit          = errors.New("agent worker step limit reached")
	ErrReplanLimit        = errors.New("agent worker replan limit reached")
	ErrEscalationLimit    = errors.New("agent worker escalation limit reached")
	ErrAuthorityRequired  = errors.New("task-bound capability lease required")
	ErrHigherVerification = errors.New("completion requires verification above V0")
)

var permittedProposals = []agentprotocol.ProposalType{
	agentprotocol.ProposalTool, agentprotocol.ProposalDelegate, agentprotocol.ProposalReplan,
	agentprotocol.ProposalComplete, agentprotocol.ProposalHuman, agentprotocol.ProposalEscalate,
	agentprotocol.ProposalWait, agentprotocol.ProposalFail,
}
