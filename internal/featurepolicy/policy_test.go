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
