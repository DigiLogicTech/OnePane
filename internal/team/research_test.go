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

func TestResearchDefaultsToAutomaticMultiRoundWorkflow(t *testing.T) {
	cfg := NormalizeResearchSettings(ResearchSettings{SynthesisPass: true})
	if cfg.CritiqueRounds != 2 {
		t.Fatalf("default critique rounds=%d", cfg.CritiqueRounds)
	}
	if got := ResearchTotalRounds(cfg); got != 4 {
		t.Fatalf("total rounds=%d", got)
	}
	want := []string{ResearchPhaseIndependent, ResearchPhaseCritique, ResearchPhaseCritique, ResearchPhaseSynthesis}
	for i, phase := range want {
		if got := ResearchPhaseForRound(cfg, int64(i+1)); got != phase {
			t.Fatalf("round %d phase=%q want %q", i+1, got, phase)
		}
	}
}

func TestResearchCritiqueRoundsAreBounded(t *testing.T) {
	cfg := NormalizeResearchSettings(ResearchSettings{CritiqueRounds: 99, SynthesisPass: true})
	if cfg.CritiqueRounds != 5 {
		t.Fatalf("critique rounds=%d", cfg.CritiqueRounds)
	}
	if got := ResearchTotalRounds(cfg); got != 7 {
		t.Fatalf("total rounds=%d", got)
	}
}
