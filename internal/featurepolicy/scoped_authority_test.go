package featurepolicy

import "testing"

func TestScopedAuthorizationDeniesAutonomousEscalation(t *testing.T) {
	project := AssistedDefaults()
	project.AutonomousRoutines = Enabled
	workspace := Policy{AutonomousRoutines: Disabled}
	agent := Policy{AutonomousRoutines: Enabled}
	if AuthorizeExecution(project, "tasks.autonomous", workspace, agent) {
		t.Fatal("Agent overrode Workspace autonomy restriction")
	}
}
