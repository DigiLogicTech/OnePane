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
