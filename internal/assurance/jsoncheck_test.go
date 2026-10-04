package assurance

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestJSONChecks(t *testing.T) {
	raw := json.RawMessage(`{"status":"ready","count":2,"nested":{"ok":true}}`)
	checks := []JSONCheck{
		{Pointer: "/status", Operator: "equals", Value: json.RawMessage(`"ready"`)},
		{Pointer: "/nested/ok", Operator: "equals", Value: json.RawMessage(`true`)},
		{Pointer: "/count", Operator: "type", Type: "number"},
		{Pointer: "/nested", Operator: "exists"},
	}
	if err := checkJSON(raw, checks); err != nil {
		t.Fatal(err)
	}
	if err := checkJSON(raw, []JSONCheck{{Pointer: "/status", Operator: "equals", Value: json.RawMessage(`"failed"`)}}); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestDeriveVerificationLevels(t *testing.T) {
	if got := deriveLevel(true, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{"result": true}, false); got != policy.VerificationV1 {
		t.Fatalf("got %s", got)
	}
	direct := map[string]bool{"inspect@1|podman@1": true}
	if got := deriveLevel(false, direct, map[string]bool{}, map[string]bool{"worker": true}, map[string]bool{"direct": true}, false); got != policy.VerificationV2 {
		t.Fatalf("got %s", got)
	}
	integration := map[string]bool{"http@1|proxy@1": true}
	sources := map[string]bool{"worker": true, "verifier": true}
	layers := map[string]bool{"result": true, "direct": true, "integration": true}
	if got := deriveLevel(true, direct, integration, sources, layers, true); got != policy.VerificationV4 {
		t.Fatalf("expected V4, got %s", got)
	}
	if got := deriveLevel(true, direct, map[string]bool{"inspect@1|podman@1": true}, sources, layers, true); got != policy.VerificationV2 {
		t.Fatalf("same path must not earn V3, got %s", got)
	}
}
