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
	if parent == Unavailable || parent == Disabled {
		return parent
	}
	if child == Inherited || strings.TrimSpace(string(child)) == "" {
		return parent
	}
	if child == Unavailable || child == Disabled {
		return child
	}
	return Enabled
}

func ResolveAuthority(parent, child Authority) Authority {
	if child == "" {
		return parent
	}
	rank := map[Authority]int{AuthorityNever: 0, AuthorityRead: 1, AuthorityPropose: 2, AuthorityAuto: 3}
	if rank[child] < rank[parent] {
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
