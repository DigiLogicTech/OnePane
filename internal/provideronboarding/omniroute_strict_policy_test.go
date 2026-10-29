package provideronboarding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Pinned upstream v3.8.51 export-json returns settings:safeSettings. Its
// virtualFactory enables the guard only for settings.freeAccessPolicy ===
// "strict". Other backup objects and differently cased strings do not do so.
// This uses the real HTTP adapter with a local fixture only; no provider,
// credential, model request, remote service or financial outcome is exercised.
func TestProbeOmniRouteStrictPolicyUsesAuthoritativeSettings(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		want           bool
	}{
		{"pinned_export_shape", `{"settings":{"freeAccessPolicy":"strict"},"providerConnections":[],"combos":[],"_meta":{"version":"omniroute-v3-legacy-export"}}`, true},
		{"global_permissive_nested_strict", `{"settings":{"freeAccessPolicy":"permissive"},"combos":[{"metadata":{"freeAccessPolicy":"strict"}}]}`, false},
		{"unrelated_routing_object", `{"routing":{"freeAccessPolicy":"strict"}}`, false},
		{"provider_metadata_only", `{"settings":{},"providerConnections":[{"providerSpecificData":{"freeAccessPolicy":"strict"}}]}`, false},
		{"uppercase_inactive_upstream", `{"settings":{"freeAccessPolicy":"STRICT"}}`, false},
		{"settings_array", `{"settings":[{"freeAccessPolicy":"strict"}]}`, false},
		{"missing_settings", `{"freeAccessPolicy":"strict"}`, false},
		{"valid_permissive", `{"settings":{"freeAccessPolicy":"permissive"}}`, false},
		{"global_strict_unrelated_permissive", `{"settings":{"freeAccessPolicy":"strict"},"combos":[{"metadata":{"freeAccessPolicy":"permissive"}}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, settings := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/models":
					models++
					_, _ = w.Write([]byte(`{"data":[{"id":"auto/coding"}]}`))
				case "/api/settings/export-json":
					settings++
					_, _ = w.Write([]byte(tc.settings))
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			probe, err := New(nil, nil).ProbeOmniRoute(context.Background(), server.URL+"/v1", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !probe.Reachable || !probe.SettingsReadable || models != 1 || settings != 1 {
				t.Fatalf("fixture did not exercise actual bounded probes: %+v models=%d settings=%d", probe, models, settings)
			}
			if probe.StrictPolicyObserved != tc.want {
				t.Fatalf("strict guard declaration=%v want=%v for %s", probe.StrictPolicyObserved, tc.want, tc.settings)
			}
			if probe.StrictZeroCostVerified {
				t.Fatal("observed policy became economic verification")
			}
		})
	}
}
