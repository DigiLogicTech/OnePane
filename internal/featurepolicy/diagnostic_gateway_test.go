package featurepolicy

import (
	"testing"
	"time"
)

func TestAuthorizeWithDiagnosticPreservesDenial(t *testing.T) {
	project := AssistedDefaults()
	project.CloudRouting = Enabled
	allowed, record := AuthorizeWithDiagnostic(project, "inference.cloud", "run:42", time.Unix(3, 0), Policy{CloudRouting: Disabled})
	if allowed || record.Allowed || record.Category != "policy.denied" {
		t.Fatalf("denied action reported as granted: %+v", record)
	}
	if record.CorrelationID != safeCorrelationID("run:42") || record.Action != "inference.cloud" {
		t.Fatalf("lost safe decision metadata: %+v", record)
	}
}
