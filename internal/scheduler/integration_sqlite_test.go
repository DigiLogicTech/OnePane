//go:build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteSchedulerProtectsSubscriptionAndPrefersStrictFree(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	seed := []string{
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-free','ws','omniroute','OmniRoute','none','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}',1,1,1)`,
		`INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m-free','omniroute','auto/coding','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('d-free','m-free','p-free','openai-compatible','{}','ready','fp-free',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-free','d-free','agent.reason','L2',0,64000,'low','supported','{}','{}','cpf-free',1,1,1)`,
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-sub','openai','openai_chatgpt_plan','ChatGPT Plan','oauth2-pkce','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"included_subscription","hard_zero_incremental_cost":false}}',1,1,1)`,
	}
	// Correct workspace typo in the final statement separately so the fixture is
	// explicit and foreign-key checked.
	for i, q := range seed[:4] {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-sub','ws','openai_chatgpt_plan','ChatGPT Plan','oauth2-pkce','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"included_subscription","hard_zero_incremental_cost":false}}',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	for i, q := range []string{
		`INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m-sub','openai','gpt-sub','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('d-sub','m-sub','p-sub','openai-chatgpt-plan','{}','ready','fp-sub',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-sub','d-sub','agent.reason','L2',0,64000,'low','verified','{}','{}','cpf-sub',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-sub-only','d-sub','agent.subscription-only','L2',0,64000,'low','verified','{}','{}','cpf-sub-only',1,1,1)`,
	} {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatalf("subscription seed %d: %v", i, err)
		}
	}

	svc := NewService(db.SQL(), db, clock.Real{})
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	d, err := svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label, AllowSubscriptionUsage: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "d-free" {
		t.Fatalf("strict free route should beat opted-in subscription: %#v", d.Selected)
	}

	if _, err := svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.subscription-only", ProtocolLevel: "L1", DataLabel: label}); err != ErrNoEligibleCandidate {
		t.Fatalf("subscription-only route should be protected by default, got %v", err)
	}
	d, err = svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.subscription-only", ProtocolLevel: "L1", DataLabel: label, AllowSubscriptionUsage: true})
	if err != nil || d.Selected == nil || d.Selected.Candidate.ID != "d-sub" {
		t.Fatalf("explicit subscription opt-in failed: %v %#v", err, d.Selected)
	}
}
