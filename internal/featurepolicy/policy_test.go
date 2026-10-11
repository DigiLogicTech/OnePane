package featurepolicy

import "testing"

func TestRestrictiveParentWins(t *testing.T) {
	if got := Resolve(Disabled, Enabled); got != Disabled {
		t.Fatalf("disabled parent escaped: %q", got)
	}
	if got := Resolve(Unavailable, Enabled); got != Unavailable {
		t.Fatalf("unavailable parent escaped: %q", got)
	}
}

func TestInheritedUsesParent(t *testing.T) {
	if got := Resolve(Enabled, Inherited); got != Enabled {
		t.Fatalf("inheritance mismatch: %q", got)
	}
}

func TestMinimalDoesNotActivateIntelligence(t *testing.T) {
	p := MinimalDefaults()
	if p.ProjectAIActive() || p.PersistentMemoryActive() || p.AutomaticRoutingActive() {
		t.Fatal("minimal policy activated intelligence")
	}
}

func TestAuthorityCannotExpand(t *testing.T) {
	if got := ResolveAuthority(AuthorityPropose, AuthorityAuto); got != AuthorityPropose {
		t.Fatalf("child expanded authority: %q", got)
	}
	if got := ResolveAuthority(AuthorityPropose, AuthorityRead); got != AuthorityRead {
		t.Fatalf("child restriction ignored: %q", got)
	}
}

func TestUnknownPolicyStateFailsClosed(t *testing.T) {
	for _, parent := range []State{Enabled, Inherited} {
		if got := Resolve(parent, State("unexpected")); got != Unavailable {
			t.Fatalf("unknown child state escaped parent %q: %q", parent, got)
		}
	}
}

func TestUnknownAuthorityFailsClosed(t *testing.T) {
	for _, tc := range []struct{ parent, child Authority }{
		{AuthorityAuto, Authority("unexpected")},
		{Authority("unexpected"), AuthorityRead},
		{Authority("unexpected"), ""},
	} {
		if got := ResolveAuthority(tc.parent, tc.child); got != AuthorityNever {
			t.Fatalf("unknown authority escaped (%q, %q): %q", tc.parent, tc.child, got)
		}
	}
}

func TestUnknownParentStateFailsClosed(t *testing.T) {
	for _, child := range []State{Enabled, Inherited, ""} {
		if got := Resolve(State("malformed"), child); got != Unavailable {
			t.Fatalf("unknown parent with child %q granted %q", child, got)
		}
	}
}

func TestFeatureChainCannotExpandWorkspaceOrProject(t *testing.T) {
	tests := []struct {
		name string
		root State
		descendants []State
		want State
	}{
		{"project-deny", Disabled, []State{Enabled, Enabled, Enabled}, Disabled},
		{"workspace-deny", Enabled, []State{Disabled, Enabled, Enabled}, Disabled},
		{"agent-deny", Enabled, []State{Enabled, Unavailable, Enabled}, Unavailable},
		{"inherited", Enabled, []State{Inherited, "", Enabled}, Enabled},
		{"malformed-project", State("bad"), []State{Enabled}, Unavailable},
		{"malformed-workspace", Enabled, []State{State("bad"), Enabled}, Unavailable},
	}
	for _, tc := range tests {
		if got := ResolveChain(tc.root, tc.descendants...); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAuthorityChainCannotExpandWorkspaceOrProject(t *testing.T) {
	tests := []struct {
		name string
		root Authority
		descendants []Authority
		want Authority
	}{
		{"project-propose", AuthorityPropose, []Authority{AuthorityAuto}, AuthorityPropose},
		{"workspace-read", AuthorityAuto, []Authority{AuthorityRead, AuthorityAuto}, AuthorityRead},
		{"agent-never", AuthorityAuto, []Authority{AuthorityAuto, AuthorityNever, AuthorityAuto}, AuthorityNever},
		{"malformed-parent", Authority("bad"), []Authority{AuthorityAuto}, AuthorityNever},
		{"malformed-workspace", AuthorityAuto, []Authority{Authority("bad"), AuthorityAuto}, AuthorityNever},
	}
	for _, tc := range tests {
		if got := ResolveAuthorityChain(tc.root, tc.descendants...); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCompletePolicyInheritanceRestrictsAllCapabilities(t *testing.T) {
	project := AssistedDefaults()
	project.CloudRouting = Enabled
	workspace := Policy{
		Files: Disabled,
		CloudRouting: Disabled,
		CrossProjectShare: Enabled,
		ConfigAuthority: AuthorityRead,
	}
	agent := Policy{
		Files: Enabled,
		CloudRouting: Enabled,
		CrossProjectShare: Enabled,
		ConfigAuthority: AuthorityAuto,
	}
	effective := ResolvePolicyChain(project, workspace, agent)
	if effective.Files != Disabled || effective.CloudRouting != Disabled {
		t.Fatalf("agent escaped Workspace restriction: %+v", effective)
	}
	if effective.CrossProjectShare != Disabled {
		t.Fatalf("agent escaped Project cross-sharing restriction: %+v", effective)
	}
	if effective.ConfigAuthority != AuthorityRead {
		t.Fatalf("agent expanded Workspace authority: %q", effective.ConfigAuthority)
	}
	if effective.ProjectAI != Enabled {
		t.Fatalf("unrestricted inherited capability was lost: %q", effective.ProjectAI)
	}
}

func TestCompletePolicyUnknownValueDenied(t *testing.T) {
	project := AssistedDefaults()
	workspace := Policy{ModelRouting: State("invalid")}
	effective := ResolvePolicyChain(project, workspace, Policy{ModelRouting: Enabled})
	if effective.ModelRouting != Unavailable || effective.AutomaticRoutingActive() {
		t.Fatalf("malformed Workspace routing unexpectedly enabled: %+v", effective)
	}
}

func TestCloudRoutingRequiresExplicitPermission(t *testing.T) {
	p := AssistedDefaults()
	if p.CanUseCloudModel() {
		t.Fatal("inherited cloud permission allowed outbound inference")
	}
	p.CloudRouting = Enabled
	if !p.CanUseCloudModel() {
		t.Fatal("explicitly granted cloud routing was denied")
	}
	p.ProjectAI = Disabled
	if p.CanUseCloudModel() {
		t.Fatal("cloud inference bypassed disabled Project AI")
	}
}

func TestResearchSeatFallbackRemainsDisabled(t *testing.T) {
	project := AssistedDefaults()
	workspace := Policy{AutomaticFallback: Disabled}
	agent := Policy{AutomaticFallback: Enabled}
	effective := ResolvePolicyChain(project, workspace, agent)
	if effective.CanUseAutomaticFallback() {
		t.Fatal("agent restored automatic fallback disabled by Workspace")
	}
}

func TestCrossProjectSharingRequiresExplicitGrant(t *testing.T) {
	p := AssistedDefaults()
	if p.CanShareAcrossProjects() {
		t.Fatal("default policy unexpectedly permits cross-project sharing")
	}
	p.CrossProjectShare = Enabled
	if !p.CanShareAcrossProjects() {
		t.Fatal("explicitly granted sharing denied")
	}
}

func TestExecutionGrantsFailClosed(t *testing.T) {
	defaults := AssistedDefaults().ExecutionGrants()
	if defaults.CloudInference || defaults.CrossProjectShare || defaults.AutonomousTasks {
		t.Fatalf("defaults leaked privileged execution grants: %+v", defaults)
	}
	project := AssistedDefaults()
	project.CloudRouting = Enabled
	project.AutonomousRoutines = Enabled
	workspace := Policy{CloudRouting: Disabled, Files: Disabled}
	agent := Policy{CloudRouting: Enabled, Files: Enabled}
	got := ResolvePolicyChain(project, workspace, agent).ExecutionGrants()
	if got.CloudInference || got.FileAccess || got.CrossProjectShare {
		t.Fatalf("agent expanded Workspace grants: %+v", got)
	}
}
