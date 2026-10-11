package featurepolicy

import "testing"

func TestAuthorizeExecutionAppliesAllScopes(t *testing.T) {
	project := AssistedDefaults()
	project.CloudRouting = Enabled
	if !AuthorizeExecution(project, "inference.cloud") {
		t.Fatal("explicit Project grant denied")
	}
	workspace := Policy{CloudRouting: Disabled}
	agent := Policy{CloudRouting: Enabled}
	if AuthorizeExecution(project, "inference.cloud", workspace, agent) {
		t.Fatal("Agent bypassed Workspace denial")
	}
	if AuthorizeExecution(project, "shell.exec") {
		t.Fatal("unknown action granted")
	}
}
