package featurepolicy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticJSONSchemaAndBoundedRemediation(t *testing.T) {
	for _, allowed := range []bool{false,true} {
		d := DecisionDiagnostic(time.Unix(10, 0), "run:123", "files.read", allowed)
		raw, err := json.Marshal(d)
		if err != nil { t.Fatal(err) }
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil { t.Fatal(err) }
		for _, name := range []string{"at_utc", "component", "correlation_id", "action", "allowed", "category", "remediation"} {
			if _, ok := fields[name]; !ok { t.Errorf("missing diagnostic key: %s", name) }
		}
		if len(fields) != 7 { t.Fatalf("unexpected diagnostic schema keys: %v", fields) }
		if !strings.Contains(d.Remediation, "permission") && !strings.Contains(d.Remediation, "policy") {
			t.Fatalf("unexpected remediation hint: %q", d.Remediation)
		}
	}
}

func TestDiagnosticCorrelationSuffixCannotCarryRawPayload(t *testing.T) {
	for _, id := range []string{
		"run:api:secret", "trace:with/slash", "request:with space",
		"task:" + strings.Repeat("z", 97), "task:\u00e9", "run:",
	} {
		d := DecisionDiagnostic(time.Unix(1, 0), id, "files.write", false)
		if d.CorrelationID != "redacted" { t.Fatalf("unsafe correlation identifier retained: %q", id) }
	}
}
