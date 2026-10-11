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

func TestManualWebCouncilTaskExecutionIsBlocked(t *testing.T) {
	for _, test := range []struct {
		mode string
		config string
		blocked bool
	}{
		{mode:"council", config:`{"manual_web_only":true,"task_execution":false}`, blocked:true},
		{mode:"council", config:`{"manual_web_only":false}`, blocked:false},
		{mode:"council", config:`{}`, blocked:false},
		{mode:"team", config:`{"manual_web_only":true}`, blocked:false},
		{mode:"direct", config:`{"manual_web_only":true}`, blocked:false},
		{mode:"council", config:`{invalid_json`, blocked:true},
	} {
		result:=manualWebCouncilExecutionDisabled(test.mode, json.RawMessage(test.config))
		if result!=test.blocked {
			t.Fatalf("mode=%s, config=%s blocked=%t want=%t",test.mode,test.config,result,test.blocked)
		}
	}
}

func TestResearchModeEnforcesStrictModelSeatsEvenWithoutOptionalFlags(t *testing.T){
 mode,settings:=researchConfiguration(json.RawMessage(`{"research_mode":true,"research":{"critique_rounds":3,"synthesis_pass":true,"anonymized_cross_critique":true}}`))
 if !mode{t.Fatal("Research mode was lost")}
 if !settings.PinModels||!settings.DisableModelSubstitution||
  !settings.SameModelRetries||!settings.PreserveFailedSeats||
  !settings.IndependentFirstPass||!settings.ScopedEvidence||
  !settings.RecordRawOutputs||!settings.FullProvenance||
  !settings.RequireAllSeats{
  t.Fatalf("incomplete mandatory Research integrity profile: %+v",settings)
 }
 if settings.CritiqueRounds!=3||!settings.SynthesisPass||!settings.AnonymizedCrossCritique{
  t.Fatalf("strict mode discarded explicit research workflow configuration: %+v",settings)
 }
 if got:=ResearchTotalRounds(settings);got!=5{t.Fatalf("unexpected round count: %d",got)}
}

func TestResearchModeCannotDisableStrictSettingsExplicitly(t *testing.T){
 raw:=json.RawMessage(`{"research_mode":true,"research":{"pin_models":false,
  "disable_model_substitution":false,"same_model_retries":false,
  "preserve_failed_seats":false,"independent_first_pass":false,
  "scoped_evidence":false,"record_raw_outputs":false,"full_provenance":false,
  "require_all_seats":false}}`)
 _,settings:=researchConfiguration(raw)
 strict:=StrictResearchSettings(ResearchSettings{})
 if settings.PinModels!=strict.PinModels||
  settings.DisableModelSubstitution!=strict.DisableModelSubstitution||
  settings.SameModelRetries!=strict.SameModelRetries||
  settings.RequireAllSeats!=strict.RequireAllSeats||
  settings.FullProvenance!=strict.FullProvenance{
  t.Fatalf("explicit false flags weakened Research integrity: %+v",settings)
 }
 // The frozen session settings are separate data, not mutated in-place.
 if got:=string(raw);!json.Valid([]byte(got))||got==""{
  t.Fatal("original team configuration lost")
 }
}

func TestOrdinaryTeamModeNotForcedIntoResearchDefaults(t *testing.T){
 mode,settings:=researchConfiguration(json.RawMessage(`{"research_mode":false,"research":{"critique_rounds":4}}`))
 if mode||settings.PinModels||settings.DisableModelSubstitution||
  settings.RequireAllSeats||settings.IndependentFirstPass{
  t.Fatalf("ordinary Team unexpectedly became strict Research: %+v",settings)
 }
 if settings.CritiqueRounds!=4{t.Fatal("ordinary team options unexpectedly mutated")}
}
