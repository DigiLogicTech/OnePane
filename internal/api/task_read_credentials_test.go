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
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type persistedTaskRead struct {
	*task.Service
	afterRead func()
}

func (s persistedTaskRead) Get(ctx context.Context, id string) (task.Task, error) {
	row, err := s.Service.Get(ctx, id)
	if err == nil && s.afterRead != nil {
		s.afterRead()
	}
	return row, err
}

func TestTaskInspectionPersistedCredentialRevocationAndScopeChanges(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/task-read.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte("test-task-inspection-token"))
	if _, err = db.SQL().ExecContext(ctx, `INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at) VALUES('credential','operator','Test',?,'["tenant"]','["task.read"]',1)`, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	service := task.NewService(db.SQL(), db, clock.Real{})
	row, err := service.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "PRIVATE_OBJECTIVE_CANARY"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"", "/recovery-cases"} {
		for _, scenario := range []string{"allowed", "revoked", "disabled", "workspace_scope", "capability_scope", "membership"} {
			t.Run(scenario+route, func(t *testing.T) {
				for _, statement := range []string{
					`UPDATE api_credentials SET revoked_at=NULL,workspace_scope_json='["tenant"]',capability_scope_json='["task.read"]' WHERE id='credential'`,
					`UPDATE principals SET status='active' WHERE id='operator'`,
					`UPDATE workspace_memberships SET status='active' WHERE workspace_id='tenant' AND principal_id='operator'`,
				} {
					if _, err = db.SQL().ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
				hook := func() {
					var statement string
					switch scenario {
					case "allowed":
						return
					case "revoked":
						statement = `UPDATE api_credentials SET revoked_at=2 WHERE id='credential'`
					case "disabled":
						statement = `UPDATE principals SET status='disabled' WHERE id='operator'`
					case "workspace_scope":
						statement = `UPDATE api_credentials SET workspace_scope_json='["other"]' WHERE id='credential'`
					case "capability_scope":
						statement = `UPDATE api_credentials SET capability_scope_json='[]' WHERE id='credential'`
					case "membership":
						statement = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tenant' AND principal_id='operator'`
					}
					if _, err = db.SQL().ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
				s := NewServer(nil, fakeEvents{}, NewBearerAuthorizer(db.SQL(), clock.Real{}))
				s.tasks = persistedTaskRead{Service: service, afterRead: hook}
				request := httptest.NewRequest("GET", "/v1/tasks/"+row.ID+route, nil)
				request.Header.Set("Authorization", "Bearer test-task-inspection-token")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, request)
				want := 403
				if scenario == "allowed" {
					want = 200
				} else if scenario == "revoked" || scenario == "disabled" {
					want = 401
				}
				if w.Code != want {
					t.Fatalf("status %d want %d body %s", w.Code, want, w.Body.String())
				}
				if want != 200 && strings.Contains(w.Body.String(), "CANARY") {
					t.Fatal("revoked credential disclosed Task metadata")
				}
				if want == 200 && w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("Task response cacheable")
				}
			})
		}
	}
}
