package featurepolicy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticJSONExcludesSensitiveInputs(t *testing.T) {
	secret := "Bearer-password-value"
	record := DecisionDiagnostic(time.Unix(1, 0), secret, secret, false)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("diagnostic JSON leaked sensitive input")
	}
	if record.Action != "redacted" || record.CorrelationID != "redacted" {
		t.Fatalf("diagnostic not redacted: %+v", record)
	}
}
