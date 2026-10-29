//go:build integration

package provideronboarding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Actual migrated storage and onboarding HTTP probes, without any model/provider
// execution. Existing legacy records deliberately retain their original bytes.
func TestOmniRoutePolicyObservationNeverGrantsEconomicVerification(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "onboarding.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	inf := inference.NewService(db.SQL(), db, clock.Real{})
	legacy, err := inf.RegisterProvider(ctx, inference.RegisterProviderCommand{Provider: "omniroute", DisplayName: "Preserved legacy", AuthType: "none", ConnectionJSON: json.RawMessage(`{"base_url":"https://legacy.invalid/v1","scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, setting, model string
		economic, policy     bool
		denied               bool
	}{
		{"legacy_hard_requirement", "strict", "auto/coding", true, false, true},
		{"both_requirements_preserve_hard_requirement", "strict", "auto/coding", true, true, true},
		{"observed_policy_auto", "strict", "auto/coding", false, true, false},
		{"observed_policy_arbitrary_model", "strict", "paid-example/direct-model", false, true, false},
		{"missing_required_policy", "off", "auto/coding", false, true, true},
		{"optional_policy_unobserved", "off", "auto", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/v1/models":
					_, _ = w.Write([]byte(`{"data":[{"id":"auto/coding"}]}`))
				case "/api/settings/export-json":
					_, _ = w.Write([]byte(`{"settings":{"freeAccessPolicy":"` + tc.setting + `"}}`))
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
					http.Error(w, "unexpected", http.StatusBadRequest)
				}
			}))
			defer gateway.Close()
			var before int
			if err = db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_connections`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			result, err := New(inf, nil).AddOmniRoute(ctx, OmniRouteCommand{BaseURL: gateway.URL + "/v1", RequireStrictZeroCost: tc.economic, RequireStrictPolicy: tc.policy, DefaultModel: tc.model})
			if calls != 2 {
				t.Fatalf("unexpected adapter traffic count %d", calls)
			}
			if tc.denied {
				if err == nil {
					t.Fatal("unmet requested guarantee was silently weakened")
				}
				if tc.economic && !errors.Is(err, ErrStrictZeroCostNotVerified) {
					t.Fatalf("legacy hard requirement error=%v", err)
				}
				var after int
				if err = db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_connections`).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if after != before {
					t.Fatal("denied onboarding persisted provider")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Probe.StrictZeroCostVerified {
				t.Fatal("probe configuration became economic proof")
			}
			if result.Deployment.Status != inference.DeploymentReady || result.Deployment.Revision != 3 {
				t.Fatalf("gateway did not follow discovered/qualifying/ready lifecycle: %+v", result.Deployment)
			}
			rows, err := db.SQL().QueryContext(ctx, `SELECT json_extract(payload_json,'$.to') FROM events WHERE aggregate_id=? AND event_type='model_deployment.status_changed' ORDER BY sequence`, result.Deployment.ID)
			if err != nil {
				t.Fatal(err)
			}
			var transitions []string
			for rows.Next() {
				var state string
				if err = rows.Scan(&state); err != nil {
					_ = rows.Close()
					t.Fatal(err)
				}
				transitions = append(transitions, state)
			}
			err = errors.Join(rows.Err(), rows.Close())
			if err != nil || len(transitions) != 2 || transitions[0] != "qualifying" || transitions[1] != "ready" {
				t.Fatalf("gateway lifecycle journal=%v err=%v", transitions, err)
			}
			for _, raw := range []json.RawMessage{result.Provider.ConnectionJSON, result.Deployment.RuntimeConfigJSON} {
				var value struct {
					Scheduling struct {
						Cost string `json:"cost_class"`
						Hard bool   `json:"hard_zero_incremental_cost"`
					} `json:"scheduling"`
				}
				if err = json.Unmarshal(raw, &value); err != nil {
					t.Fatal(err)
				}
				if value.Scheduling.Hard || value.Scheduling.Cost != "provider_managed" {
					t.Fatalf("unproven economic authority persisted: %s", raw)
				}
			}
			var providerMeta struct {
				Observed bool   `json:"strict_policy_observed"`
				Source   string `json:"strict_policy_source"`
			}
			if err = json.Unmarshal(result.Provider.ConnectionJSON, &providerMeta); err != nil {
				t.Fatal(err)
			}
			if providerMeta.Observed != (tc.setting == "strict") {
				t.Fatal("lost exact observed configuration")
			}
			if providerMeta.Observed && providerMeta.Source != "settings.freeAccessPolicy" {
				t.Fatal("lost configuration observation source")
			}
			var modelMeta struct {
				Verified bool `json:"strict_zero_cost_verified"`
				Observed bool `json:"strict_policy_observed"`
			}
			if err = json.Unmarshal(result.Model.StaticMetadataJSON, &modelMeta); err != nil {
				t.Fatal(err)
			}
			if modelMeta.Verified || modelMeta.Observed != providerMeta.Observed {
				t.Fatal("model provenance confused observation with economic proof")
			}
		})
	}
	preserved, err := inf.Provider(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved.ConnectionJSON) != string(legacy.ConnectionJSON) || preserved.Revision != legacy.Revision {
		t.Fatal("existing legacy provider was rewritten")
	}
}
