package featurepolicy

import "strings"

type State string

const (
	Inherited   State = "inherited"
	Enabled     State = "enabled"
	Disabled    State = "disabled"
	Unavailable State = "unavailable"
)

type Authority string

const (
	AuthorityNever    Authority = "never"
	AuthorityRead     Authority = "read"
	AuthorityPropose  Authority = "propose"
	AuthorityAuto     Authority = "auto"
)

type Policy struct {
	Assistant          State
	ProjectAI          State
	ProjectMemory      State
	TelemetryAccess    State
	TelemetryAutonomy  State
	ModelRouting       State
	AutomaticFallback State
	CapabilityRouting State
	CloudRouting       State
	Images             State
	Files              State
	AudioInput         State
	AudioOutput        State
	Camera             State
	Video              State
	Team               State
	Council            State
	CrossProjectShare  State
	AutonomousRoutines State
	ConfigAuthority    Authority
}

func AssistedDefaults() Policy {
	return Policy{
		Assistant: Enabled, ProjectAI: Enabled, ProjectMemory: Enabled,
		TelemetryAccess: Enabled, TelemetryAutonomy: Disabled,
		ModelRouting: Enabled, AutomaticFallback: Enabled, CapabilityRouting: Enabled,
		CloudRouting: Inherited, Images: Enabled, Files: Enabled,
		AudioInput: Disabled, AudioOutput: Disabled, Camera: Disabled, Video: Disabled,
		Team: Enabled, Council: Enabled, CrossProjectShare: Disabled,
		AutonomousRoutines: Disabled, ConfigAuthority: AuthorityPropose,
	}
}

func MinimalDefaults() Policy {
	p := AssistedDefaults()
	p.Assistant = Disabled
	p.ProjectAI = Disabled
	p.ProjectMemory = Disabled
	p.TelemetryAutonomy = Disabled
	p.ModelRouting = Disabled
	p.AutomaticFallback = Disabled
	p.CapabilityRouting = Disabled
	p.CloudRouting = Disabled
	p.Team = Disabled
	p.Council = Disabled
	p.AutonomousRoutines = Disabled
	p.ConfigAuthority = AuthorityRead
	return p
}

// Resolve applies a child override without permitting a child to escape a
// restrictive parent. Inherited preserves the parent's value.
func Resolve(parent, child State) State {
	if parent != Enabled && parent != Disabled && parent != Unavailable {
    return Unavailable
  }
  if parent == Unavailable || parent == Disabled {
		return parent
	}
	if child == Inherited || strings.TrimSpace(string(child)) == "" {
		return parent
	}
	if child == Unavailable || child == Disabled {
		return child
	}
	if child != Enabled {
		// Unknown policy values must never grant a capability.
		return Unavailable
	}
	return Enabled
}

func ResolveAuthority(parent, child Authority) Authority {
	if child == "" {
		child = parent
	}
	rank := map[Authority]int{AuthorityNever: 0, AuthorityRead: 1, AuthorityPropose: 2, AuthorityAuto: 3}
	parentRank, parentOK := rank[parent]
	childRank, childOK := rank[child]
	if !parentOK || !childOK {
		return AuthorityNever
	}
	if childRank < parentRank {
		return child
	}
	return parent
}

func (p Policy) ProjectAIActive() bool {
	return p.ProjectAI == Enabled
}

func (p Policy) PersistentMemoryActive() bool {
	return p.ProjectAIActive() && p.ProjectMemory == Enabled
}

func (p Policy) AutomaticRoutingActive() bool {
	return p.ModelRouting == Enabled && p.CapabilityRouting == Enabled
}

// ResolveChain calculates the effective state across Project, Workspace,
// Agent and execution overrides. Each descendant can restrict but never
// expand its ancestor's effective capability.
func ResolveChain(root State, descendants ...State) State {
	effective := Resolve(root, Inherited)
	for _, child := range descendants {
		effective = Resolve(effective, child)
	}
	return effective
}

// ResolveAuthorityChain applies the same monotonic restriction to
// configuration authority across the execution hierarchy.
func ResolveAuthorityChain(root Authority, descendants ...Authority) Authority {
	effective := ResolveAuthority(root, "")
	for _, child := range descendants {
		effective = ResolveAuthority(effective, child)
	}
	return effective
}

// ResolvePolicy computes an effective policy for a child scope. Every feature
// must pass through the same fail-closed resolution; new fields must be added
// here before they can be granted to an execution scope.
func ResolvePolicy(parent, child Policy) Policy {
	return Policy{
		Assistant: Resolve(parent.Assistant, child.Assistant),
		ProjectAI: Resolve(parent.ProjectAI, child.ProjectAI),
		ProjectMemory: Resolve(parent.ProjectMemory, child.ProjectMemory),
		TelemetryAccess: Resolve(parent.TelemetryAccess, child.TelemetryAccess),
		TelemetryAutonomy: Resolve(parent.TelemetryAutonomy, child.TelemetryAutonomy),
		ModelRouting: Resolve(parent.ModelRouting, child.ModelRouting),
		AutomaticFallback: Resolve(parent.AutomaticFallback, child.AutomaticFallback),
		CapabilityRouting: Resolve(parent.CapabilityRouting, child.CapabilityRouting),
		CloudRouting: Resolve(parent.CloudRouting, child.CloudRouting),
		Images: Resolve(parent.Images, child.Images),
		Files: Resolve(parent.Files, child.Files),
		AudioInput: Resolve(parent.AudioInput, child.AudioInput),
		AudioOutput: Resolve(parent.AudioOutput, child.AudioOutput),
		Camera: Resolve(parent.Camera, child.Camera),
		Video: Resolve(parent.Video, child.Video),
		Team: Resolve(parent.Team, child.Team),
		Council: Resolve(parent.Council, child.Council),
		CrossProjectShare: Resolve(parent.CrossProjectShare, child.CrossProjectShare),
		AutonomousRoutines: Resolve(parent.AutonomousRoutines, child.AutonomousRoutines),
		ConfigAuthority: ResolveAuthority(parent.ConfigAuthority, child.ConfigAuthority),
	}
}

// ResolvePolicyChain applies successive Workspace, Agent and execution
// overrides without allowing any descendant to restore a denied capability.
func ResolvePolicyChain(project Policy, descendants ...Policy) Policy {
	effective := ResolvePolicy(project, Policy{})
	for _, child := range descendants {
		effective = ResolvePolicy(effective, child)
	}
	return effective
}
