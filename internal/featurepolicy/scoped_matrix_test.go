package featurepolicy

import "testing"

func TestScopedAuthorizationMatrix(t *testing.T) {
	tests := []struct {
		name string
		action string
		project Policy
		workspace Policy
		want bool
	}{
		{"local allowed", "inference.local", AssistedDefaults(), Policy{}, true},
		{"local denied", "inference.local", AssistedDefaults(), Policy{ModelRouting: Disabled}, false},
		{"files allowed", "files.read", AssistedDefaults(), Policy{}, true},
		{"files denied", "files.write", AssistedDefaults(), Policy{Files: Disabled}, false},
		{"unknown denied", "tool.execute", AssistedDefaults(), Policy{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AuthorizeExecution(tc.project, tc.action, tc.workspace); got != tc.want {
				t.Fatalf("action %q got %t want %t", tc.action, got, tc.want)
			}
		})
	}
}
