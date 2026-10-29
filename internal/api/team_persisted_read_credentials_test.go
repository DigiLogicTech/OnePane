package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Team storage is deliberately hooked at each collection boundary; credential,
// principal, scope and membership decisions use the real migrated SQLite store
// and BearerAuthorizer. A successful pre-read authentication must not survive
// a persisted revocation observed before the response is published.
func TestTeamReadsPersistedCredentialRevocationAndScopeChanges(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/team-read.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.SQL().ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`)
	for _, principal := range []string{"operator", "replacement"} {
		exec(`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'human','Operator','active',1,1,1)`, principal)
		exec(`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant',?,'active',1,1)`, principal)
	}
	digest := sha256.Sum256([]byte("test-team-inspection-token"))
	exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at) VALUES('credential','operator','Test',?,'["tenant"]','["team.read","task.read"]',1)`, hex.EncodeToString(digest[:]))
	for _, target := range []struct{ stage, path string }{
		{"lookup", "/v1/tasks/task/team-session"},
		{"session", "/v1/team-sessions/room"},
		{"live", "/v1/team-sessions/room/live"},
		{"messages", "/v1/team-sessions/room/messages"},
		{"page", "/v1/team-sessions/room/messages/page"},
		{"teams", "/v1/teams?workspace_id=tenant"},
		{"members", "/v1/teams/team/members"},
		{"manifest", "/v1/team-sessions/room/manifest"},
		{"bindings", "/v1/team-sessions/room/manifest"},
		{"turns", "/v1/team-sessions/room/turns"},
		{"plans", "/v1/team-sessions/room/plans"},
		{"objections", "/v1/team-sessions/room/objections"},
		{"decisions", "/v1/team-sessions/room/decisions"},
		{"evidence", "/v1/team-sessions/room/research/evidence-selection"},
	} {
		scenarios := []string{"allowed", "revoked", "disabled", "principal_changed", "workspace_scope", "team_scope", "membership"}
		if target.stage == "live" {
			scenarios = append(scenarios, "task_scope")
		}
		for _, scenario := range scenarios {
			t.Run(target.stage+"/"+scenario, func(t *testing.T) {
				exec(`UPDATE api_credentials SET principal_id='operator',revoked_at=NULL,workspace_scope_json='["tenant"]',capability_scope_json='["team.read","task.read"]' WHERE id='credential'`)
				exec(`UPDATE principals SET status='active' WHERE id='operator'`)
				exec(`UPDATE workspace_memberships SET status='active' WHERE workspace_id='tenant' AND principal_id='operator'`)
				observed := false
				hook := func() {
					observed = true
					switch scenario {
					case "allowed":
					case "revoked":
						exec(`UPDATE api_credentials SET revoked_at=2 WHERE id='credential'`)
					case "disabled":
						exec(`UPDATE principals SET status='disabled' WHERE id='operator'`)
					case "principal_changed":
						exec(`UPDATE api_credentials SET principal_id='replacement' WHERE id='credential'`)
					case "workspace_scope":
						exec(`UPDATE api_credentials SET workspace_scope_json='["other"]' WHERE id='credential'`)
					case "team_scope":
						exec(`UPDATE api_credentials SET capability_scope_json='["task.read"]' WHERE id='credential'`)
					case "task_scope":
						exec(`UPDATE api_credentials SET capability_scope_json='["team.read"]' WHERE id='credential'`)
					case "membership":
						exec(`UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tenant' AND principal_id='operator'`)
					}
				}
				s := NewServer(nil, fakeEvents{}, NewBearerAuthorizer(db.SQL(), clock.Real{}))
				s.SetTeam(inspectedTeamRead{stage: target.stage, afterRead: hook})
				s.projects = inspectedResearchRead{afterRead: hook}
				request := httptest.NewRequest("GET", target.path, nil)
				request.Header.Set("Authorization", "Bearer test-team-inspection-token")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, request)
				want := 403
				if scenario == "allowed" {
					want = 200
				} else if scenario == "revoked" || scenario == "disabled" {
					want = 401
				}
				if !observed || w.Code != want {
					t.Fatalf("read observed %v status %d want %d body %s", observed, w.Code, want, w.Body.String())
				}
				if want != 200 && strings.Contains(w.Body.String(), "CANARY") {
					t.Fatal("revoked persisted credential disclosed Team data")
				}
				if want == 200 && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "CANARY")) {
					t.Fatal("authorized Team read lost its payload/cache contract")
				}
			})
		}
	}
}
