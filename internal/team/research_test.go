package team

import (
	"encoding/json"
	"testing"
)

func TestResearchConfigurationParsesIntegrityPolicy(t *testing.T) {
	raw := json.RawMessage(`{"research_mode":true,"research":{"pin_models":true,"disable_model_substitution":true,"same_model_retries":true,"preserve_failed_seats":true,"independent_first_pass":true,"scoped_evidence":true,"record_raw_outputs":true,"full_provenance":true,"require_all_seats":true}}`)
	mode, cfg := researchConfiguration(raw)
	if !mode || !cfg.PinModels || !cfg.DisableModelSubstitution || !cfg.SameModelRetries || !cfg.PreserveFailedSeats || !cfg.IndependentFirstPass || !cfg.ScopedEvidence || !cfg.RecordRawOutputs || !cfg.FullProvenance || !cfg.RequireAllSeats {
		t.Fatalf("research integrity policy was not preserved: mode=%v cfg=%+v", mode, cfg)
	}
}

func TestProfileIDFromMemberConfigIsStable(t *testing.T) {
	if got := profileIDFromMemberConfig(json.RawMessage(`{"agent_profile_id":"agent.researcher"}`)); got != "agent.researcher" {
		t.Fatalf("profile id=%q", got)
	}
	if got := profileIDFromMemberConfig(json.RawMessage(`{}`)); got != "agent.md" {
		t.Fatalf("default profile id=%q", got)
	}
}
