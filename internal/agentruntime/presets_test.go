package agentruntime

import "testing"

func TestHarnessCatalogCoversPopularRuntimes(t *testing.T) {
	ps := BuiltinPresets()
	if len(ps) < 30 {
		t.Fatalf("expected broad harness catalog, got %d", len(ps))
	}
	for _, id := range []string{"hermes", "microsoft-agent-framework", "google-adk", "aws-strands", "openai-agents-sdk", "langgraph", "crewai", "openhands", "goose", "opencode", "claude-code", "openai-codex", "gemini-cli", "aider", "cursor", "windsurf", "n8n-ai"} {
		if _, ok := PresetByID(id); !ok {
			t.Fatalf("missing harness preset %s", id)
		}
	}
	cursor, _ := PresetByID("cursor")
	if cursor.AutonomousEligible || cursor.RecommendedMode != Unmanaged {
		t.Fatalf("opaque IDE runtime must remain human-only: %+v", cursor)
	}
}
