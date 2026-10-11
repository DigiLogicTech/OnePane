package featurepolicy

import "testing"

func TestScopedAuthorizationRejectsFileEscalation(t *testing.T) {
	project := AssistedDefaults()
	workspace := Policy{Files: Disabled}
	agent := Policy{Files: Enabled}
	for _, action := range []string{"files.read", "files.write"} {
		if AuthorizeExecution(project, action, workspace, agent) {
			t.Fatalf("Workspace restriction bypassed for %s", action)
		}
	}
}

func TestScopedAuthorizationRespectsResearchFallbackPin(t *testing.T) {
	project := AssistedDefaults()
	seat := Policy{AutomaticFallback: Disabled}
	if AuthorizeExecution(project, "inference.fallback", seat) {
		t.Fatal("disabled fallback was granted")
	}
}
