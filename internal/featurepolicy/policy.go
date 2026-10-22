package featurepolicy

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
	// Independent Workspace controls; the legacy Files switch is still
	// the parent master gate for read and write compatibility.
	FileRead           State
	FileWrite          State
	ProcessExecute     State
	NetworkAccess      State
	SecretsAccess      State
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
		FileRead: Enabled, FileWrite: Enabled, ProcessExecute: Inherited,
		NetworkAccess: Inherited, SecretsAccess: Inherited,
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
	if child == Inherited || child == "" {
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

// The legacy Files master grant predates independent Read/Write states.
// For older root Policy objects without the new fields, preserve the
// historical file capability and then apply monotonic child restrictions.
func resolveFileSubGrant(parent,child,legacy State)State{
    if parent==""||parent==Inherited {
        switch legacy{
        case Enabled,Disabled,Unavailable:parent=legacy
        default:parent=Unavailable
        }
    }
    return Resolve(parent,child)
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
		FileRead: resolveFileSubGrant(parent.FileRead, child.FileRead, parent.Files),
		FileWrite: resolveFileSubGrant(parent.FileWrite, child.FileWrite, parent.Files),
		ProcessExecute: Resolve(parent.ProcessExecute, child.ProcessExecute),
		NetworkAccess: Resolve(parent.NetworkAccess, child.NetworkAccess),
		SecretsAccess: Resolve(parent.SecretsAccess, child.SecretsAccess),
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

// CanUseCloudModel requires both model routing and explicit cloud permission.
// An inherited or malformed cloud state never authorises outbound inference.
func (p Policy) CanUseCloudModel() bool {
	return p.ProjectAI == Enabled && p.ModelRouting == Enabled &&
		p.CloudRouting == Enabled
}

// CanUseAutomaticFallback is independent of model routing: research seats
// that disable fallback must remain pinned to their selected provider/model.
func (p Policy) CanUseAutomaticFallback() bool {
	return p.ProjectAI == Enabled && p.ModelRouting == Enabled &&
		p.AutomaticFallback == Enabled
}

// CanShareAcrossProjects requires an explicit grant; an inherited value
// does not imply permission to publish Workspace artifacts.
func (p Policy) CanShareAcrossProjects() bool {
	return p.CrossProjectShare == Enabled
}

// ExecutionGrants is the small, explicit capability set that must be checked
// at the point of use. Never infer cloud or cross-project authority from a
// model candidate list, task payload or an Agent role alone.
type ExecutionGrants struct {
	LocalInference bool
	CloudInference bool
	AutomaticFallback bool
	FileAccess bool
	ReadFiles bool
	WriteFiles bool
	Execute bool
	Network bool
	Secrets bool
	CrossProjectShare bool
	AutonomousTasks bool
}

func (p Policy) ExecutionGrants() ExecutionGrants {
	return ExecutionGrants{
		LocalInference: p.ProjectAI == Enabled && p.ModelRouting == Enabled,
		CloudInference: p.CanUseCloudModel(),
		AutomaticFallback: p.CanUseAutomaticFallback(),
		FileAccess: p.Files == Enabled,
		ReadFiles: p.Files == Enabled && p.FileRead == Enabled,
		WriteFiles: p.Files == Enabled && p.FileWrite == Enabled,
		Execute: p.ProcessExecute == Enabled,
		Network: p.NetworkAccess == Enabled,
		Secrets: p.SecretsAccess == Enabled,
		CrossProjectShare: p.CanShareAcrossProjects(),
		AutonomousTasks: p.ProjectAI == Enabled && p.AutonomousRoutines == Enabled,
	}
}

// ValidateExecution refuses actions that are not explicitly authorised by
// the effective Project/Workspace/Agent policy. The caller must resolve
// the complete policy chain before invoking this boundary check.
func (p Policy) ValidateExecution(action string) bool {
	grants := p.ExecutionGrants()
	switch action {
	case "inference.local":
		return grants.LocalInference
	case "inference.cloud":
		return grants.CloudInference
	case "inference.fallback":
		return grants.AutomaticFallback
	case "files.read":
		return grants.ReadFiles
	case "files.write":
		return grants.WriteFiles
	case "process.execute":
		return grants.Execute
	case "network.connect":
		return grants.Network
	case "secrets.use":
		return grants.Secrets
	case "projects.share":
		return grants.CrossProjectShare
	case "tasks.autonomous":
		return grants.AutonomousTasks
	default:
		return false
	}
}
