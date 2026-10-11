package agentrole

import "testing"

func TestAssignmentIdentityIsIndependentFromRuntime(t *testing.T) {
	a := Assignment{ID: "aa-1", DefinitionID: "direct", Enabled: true}
	r1 := RuntimeSession{ID: "rs-1", AssignmentID: a.ID, DeploymentID: "model-a"}
	r2 := RuntimeSession{ID: "rs-2", AssignmentID: a.ID, DeploymentID: "model-b"}
	if r1.AssignmentID != r2.AssignmentID || r1.AssignmentID != a.ID {
		t.Fatal("runtime swap changed durable agent identity")
	}
}
