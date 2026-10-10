package agentrole

import "time"

type ScopeType string
type ModelStrategy string
type RuntimeState string

const (
	ScopeGlobal    ScopeType = "global"
	ScopeProject   ScopeType = "project"
	ScopeWorkspace ScopeType = "workspace"

	ModelFixed     ModelStrategy = "fixed"
	ModelPreferred ModelStrategy = "preferred"
	ModelAuto      ModelStrategy = "auto"

	RuntimeReady      RuntimeState = "ready"
	RuntimeRunning    RuntimeState = "running"
	RuntimeSwapping   RuntimeState = "swapping"
	RuntimeRecovering RuntimeState = "recovering"
	RuntimeDegraded   RuntimeState = "degraded"
	RuntimeBlocked    RuntimeState = "blocked"
	RuntimePaused     RuntimeState = "paused"
	RuntimeDisabled   RuntimeState = "disabled"
)

type Definition struct {
	ID              string
	SchemaVersion   int
	Name            string
	Builtin         bool
	CurrentRevision int
}

type DefinitionRevision struct {
	DefinitionID string
	Revision     int
	AgentMD      string
	CreatedAt    time.Time
}

type Scope struct {
	Type ScopeType
	ID   string
}

type ModelPolicy struct {
	Strategy  ModelStrategy
	Primary   string
	Fallbacks []string
}

type Assignment struct {
	ID                 string
	DefinitionID       string
	DefinitionRevision int
	Scope              Scope
	ModelPolicy        ModelPolicy
	PermissionPolicyID string
	MemoryNamespace    string
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// RuntimeSession is disposable. Durable identity belongs to Assignment.
type RuntimeSession struct {
	ID             string
	AssignmentID   string
	DeploymentID   string
	NodeID         string
	State          RuntimeState
	DefinitionRev  int
	StartedAt      time.Time
	LastActivityAt time.Time
}
