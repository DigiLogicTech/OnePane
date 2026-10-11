package projectorchestrator

import "testing"

func TestWorkspaceExecutionProfileIsConfiguredNotModelSupplied(t *testing.T) {
	tests := []struct {
		name, settings, mode, team string
	}{
		{"unset", "{}", "direct", ""},
		{"invalid settings", "{bad", "direct", ""},
		{"direct rejects team", `{"orchestration":{"mode":"direct","team_id":"unrelated"}}`, "direct", ""},
		{"team scoped", `{"orchestration":{"mode":"TEAM","team":{"team_id":"team-specific"},"team_id":"fallback"}}`, "team", "team-specific"},
		{"council scoped", `{"orchestration":{"mode":"council","council":{"team_id":"research-council"},"team_id":"fallback"}}`, "council", "research-council"},
		{"team fallback", `{"orchestration":{"mode":"team","team_id":"legacy-team"}}`, "team", "legacy-team"},
		{"routing disabled", `{"orchestration":{"mode":"council","council":{"team_id":"sensitive-team"}},"routing":{"enabled":false}}`, "direct", ""},
		{"unknown mode", `{"orchestration":{"mode":"admin","team_id":"not-authorized"}}`, "direct", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, teamID := workspaceExecutionProfile(tt.settings)
			if mode != tt.mode || teamID != tt.team {
				t.Fatalf("profile %s %s, expected %s %s", mode, teamID, tt.mode, tt.team)
			}
		})
	}
}
