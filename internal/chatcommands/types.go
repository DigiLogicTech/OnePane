package chatcommands

import "time"

type Category string

const (
	CategoryFlow          Category = "flow"
	CategorySession       Category = "session"
	CategoryModel         Category = "model"
	CategoryExecution     Category = "execution"
	CategoryObservability Category = "observability"
	CategoryGovernance    Category = "governance"
)

type BusyMode string

const (
	BusyQueue     BusyMode = "queue"
	BusySteer     BusyMode = "steer"
	BusyInterrupt BusyMode = "interrupt"
)

type ApprovalMode string

const (
	ApprovalManual         ApprovalMode = "manual"
	ApprovalAutoAuthorized ApprovalMode = "auto_authorized"
)

// ApprovalLevel controls how aggressively OnePane auto-approves operations that
// already passed policy and user-authority checks. Higher means more prompts.
type ApprovalLevel string

const (
	ApprovalHigh   ApprovalLevel = "high"
	ApprovalMedium ApprovalLevel = "medium"
	ApprovalLow    ApprovalLevel = "low"
)

// ApprovalRisk is assigned by the authoritative operation/policy layer. Slash
// commands never choose their own risk.
type ApprovalRisk string

const (
	RiskLow      ApprovalRisk = "low"
	RiskMedium   ApprovalRisk = "medium"
	RiskHigh     ApprovalRisk = "high"
	RiskCritical ApprovalRisk = "critical"
)

type ApprovalDecision string

const (
	DecisionDeny        ApprovalDecision = "deny"
	DecisionPrompt      ApprovalDecision = "prompt"
	DecisionAutoApprove ApprovalDecision = "auto_approve"
)

type QueueStatus string

const (
	QueuePending    QueueStatus = "pending"
	QueueDelivering QueueStatus = "delivering"
	QueueDelivered  QueueStatus = "delivered"
	QueueCancelled  QueueStatus = "cancelled"
	QueueBlocked    QueueStatus = "blocked"
	QueueFailed     QueueStatus = "failed"
)

type CommandSpec struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Usage       string   `json:"usage"`
	Description string   `json:"description"`
	Category    Category `json:"category"`
	Mutating    bool     `json:"mutating"`
	RequiresRun bool     `json:"requires_run"`
}

type ParsedCommand struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
	Raw  string   `json:"raw"`
}

type QueueItem struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	Position  int64       `json:"position"`
	Prompt    string      `json:"prompt"`
	Status    QueueStatus `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type SessionControls struct {
	SessionID       string        `json:"session_id"`
	BusyMode        BusyMode      `json:"busy_mode"`
	ApprovalMode    ApprovalMode  `json:"approval_mode"`
	ApprovalLevel   ApprovalLevel `json:"approval_level"`
	QueuePaused     bool          `json:"queue_paused"`
	ModelOverride   string        `json:"model_override,omitempty"`
	AgentOverride   string        `json:"agent_override,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type ActionKind string

const (
	ActionNone          ActionKind = "none"
	ActionQueueChanged  ActionKind = "queue_changed"
	ActionSteer         ActionKind = "steer"
	ActionStop          ActionKind = "stop"
	ActionRetry         ActionKind = "retry"
	ActionContinue      ActionKind = "continue"
	ActionNewSession    ActionKind = "new_session"
	ActionBranch        ActionKind = "branch"
	ActionBackground    ActionKind = "background"
	ActionGoal          ActionKind = "goal"
	ActionSubgoal       ActionKind = "subgoal"
	ActionModel         ActionKind = "model"
	ActionAgent         ActionKind = "agent"
	ActionReasoning     ActionKind = "reasoning"
	ActionTools         ActionKind = "tools"
	ActionSandbox       ActionKind = "sandbox"
	ActionVerify        ActionKind = "verify"
	ActionCheckpoint    ActionKind = "checkpoint"
	ActionApproval      ActionKind = "approval"
	ActionReadOnly      ActionKind = "read_only"
	ActionSessionChange ActionKind = "session_change"
)

type Result struct {
	Handled  bool             `json:"handled"`
	Action   ActionKind       `json:"action"`
	Message  string           `json:"message,omitempty"`
	Command  *ParsedCommand   `json:"command,omitempty"`
	Controls *SessionControls `json:"controls,omitempty"`
	Queue    []QueueItem      `json:"queue,omitempty"`
	Payload  map[string]any   `json:"payload,omitempty"`
}

// Authority is evaluated by the integration layer before a command that changes
// execution state is applied. Approval levels and /yolo change approval UX only;
// they never mint a CapabilityLease or turn a hard policy denial into an allowed operation.
type Authority interface {
	CanControlSession(sessionID, command string) error
	CanEnableAutoApproval(sessionID string) error
}

// DecideApproval is deliberately small and deterministic. It is called only
// after the authoritative policy layer has classified the operation risk and
// established whether the current user is allowed to approve it.
func DecideApproval(c SessionControls, risk ApprovalRisk, userAuthorized, hardDenied bool) ApprovalDecision {
	if hardDenied || !userAuthorized {
		return DecisionDeny
	}
	if c.ApprovalMode == ApprovalAutoAuthorized {
		return DecisionAutoApprove
	}
	level := c.ApprovalLevel
	if level == "" {
		level = ApprovalMedium
	}
	switch level {
	case ApprovalHigh:
		return DecisionPrompt
	case ApprovalMedium:
		if risk == RiskLow {
			return DecisionAutoApprove
		}
		return DecisionPrompt
	case ApprovalLow:
		if risk == RiskLow || risk == RiskMedium {
			return DecisionAutoApprove
		}
		return DecisionPrompt
	default:
		return DecisionPrompt
	}
}
