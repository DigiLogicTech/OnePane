package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	dbsqlite "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

// Only the observation boundary is instrumented. Authentication, authorization,
// case capture and journal reads use the real persisted production services.
type persistedJournalReader struct {
	*task.Service
	afterObservation func()
}

func (r *persistedJournalReader) ObserveRecoveryJournal(ctx context.Context, ws, taskID, caseID string) (task.RecoveryJournalView, error) {
	view, err := r.Service.ObserveRecoveryJournal(ctx, ws, taskID, caseID)
	if err == nil && r.afterObservation != nil {
		r.afterObservation()
	}
	return view, err
}

func TestRecoveryCaseReadsJournalHTTPRealCredentials(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			db, err := dbsqlite.Open(filepath.Join(t.TempDir(), "journal.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := db.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			now := clock.Real{}.UnixMilli()
			exec(`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('journal-tenant','Journal','active',1,1)`)
			exec(`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('journal-human','human','Human','active',1,1),('journal-worker','system','Worker','active',1,1)`)
			exec(`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('journal-tenant','journal-human','active',1,1)`)
			exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at,expires_at) VALUES('journal-key','journal-human','Disposable',?,'["journal-tenant"]','["task.read"]',?,?)`, transactionAuthHash("journal-disposable-bearer"), now, now+3600000)
			exec(`INSERT INTO roles(id,role_class,name,definition_json,created_at,updated_at) SELECT 'journal-admin','human','Admin','{}',1,1 WHERE NOT EXISTS(SELECT 1 FROM roles WHERE name='Admin')`)
			exec(`INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) SELECT 'journal-human',id,'journal-tenant',1 FROM roles WHERE name='Admin'`)
			exec(`INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES('journal-session','journal-human',?,'password','single_factor',?,?,?,?, '{}')`, transactionAuthHash("journal-disposable-session"), now, now, now+600000, now+3600000)
			svc := task.NewService(db.SQL(), db, clock.Real{})
			row, err := svc.Create(ctx, task.CreateCommand{WorkspaceID: "journal-tenant", Objective: "Inspect interrupted journal privately"})
			if err != nil {
				t.Fatal(err)
			}
			row, err = svc.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			if err != nil {
				t.Fatal(err)
			}
			worker := "journal-worker"
			row, attempt, err := svc.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision, WorkerPrincipalID: &worker})
			if err != nil {
				t.Fatal(err)
			}
			row, err = svc.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision, Reason: "Unknown synthetic result"})
			if err != nil {
				t.Fatal(err)
			}
			if captured, e := svc.CaptureRecoveryCases(ctx, 1); e != nil || captured.Captured != 1 {
				t.Fatalf("capture %+v %v", captured, e)
			}
			page, err := svc.ListRecoveryCases(ctx, "journal-tenant", row.ID, "")
			if err != nil || len(page.Cases) != 1 {
				t.Fatalf("case %+v %v", page, err)
			}
			exec(`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('journal-owned-run','journal-tenant',?,?,'journal-worker','interrupted','general','inference.general','L1',24,3,3,'{}','{"PRIVATE_PROMPT":"secret"}',1,1)`, row.ID, attempt.ID)
			exec(`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,result_ref,detail_json,started_at) VALUES('journal-owned-step','journal-owned-run',1,'model','unknown','PRIVATE_TARGET','{"reason":"PRIVATE_REASON","outcome":"unknown_or_response_unreadable","action":"operator_reconciliation_required"}',1)`)
			reader := &persistedJournalReader{Service: svc}
			bearer := NewBearerAuthorizer(db.SQL(), clock.Real{})
			var auth Authorizer = bearer
			if method == "session" {
				auth = NewHybridAuthorizer(bearer, webauth.NewService(db.SQL(), db, clock.Real{}))
			}
			server := NewServer(nil, fakeEvents{}, auth)
			server.tasks = reader
			for _, scenario := range []string{"allowed", "revoked", "expired", "membership", "principal_disabled", "permission", "task_revision"} {
				t.Run(scenario, func(t *testing.T) {
					exec(`UPDATE api_credentials SET revoked_at=NULL,expires_at=?,capability_scope_json='["task.read"]' WHERE id='journal-key'`, now+3600000)
					exec(`UPDATE auth_sessions SET revoked_at=NULL,idle_expires_at=?,expires_at=? WHERE id='journal-session'`, now+600000, now+3600000)
					exec(`UPDATE workspace_memberships SET status='active' WHERE workspace_id='journal-tenant' AND principal_id='journal-human'`)
					exec(`UPDATE principals SET status='active' WHERE id='journal-human'`)
					exec(`INSERT OR IGNORE INTO principal_roles(principal_id,role_id,workspace_id,created_at) SELECT 'journal-human',id,'journal-tenant',1 FROM roles WHERE name='Admin'`)
					reader.afterObservation = func() {
						switch scenario {
						case "revoked":
							if method == "bearer" {
								exec(`UPDATE api_credentials SET revoked_at=? WHERE id='journal-key'`, now)
							} else {
								exec(`UPDATE auth_sessions SET revoked_at=? WHERE id='journal-session'`, now)
							}
						case "expired":
							if method == "bearer" {
								exec(`UPDATE api_credentials SET created_at=1,expires_at=2 WHERE id='journal-key'`)
							} else {
								exec(`UPDATE auth_sessions SET created_at=1,expires_at=2 WHERE id='journal-session'`)
							}
						case "membership":
							exec(`UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='journal-tenant' AND principal_id='journal-human'`)
						case "principal_disabled":
							exec(`UPDATE principals SET status='disabled' WHERE id='journal-human'`)
						case "permission":
							if method == "bearer" {
								exec(`UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='journal-key'`)
							} else {
								exec(`DELETE FROM principal_roles WHERE principal_id='journal-human'`)
							}
						case "task_revision":
							exec(`UPDATE tasks SET revision=revision+1 WHERE id=?`, row.ID)
						}
					}
					req := httptest.NewRequest("GET", "/v1/tasks/"+row.ID+"/recovery-cases/"+page.Cases[0].ID+"/journal", nil)
					if method == "bearer" {
						req.Header.Set("Authorization", "Bearer journal-disposable-bearer")
					} else {
						req.AddCookie(&http.Cookie{Name: WebSessionCookie, Value: "journal-disposable-session"})
					}
					w := httptest.NewRecorder()
					server.Handler().ServeHTTP(w, req)
					if scenario == "allowed" {
						if w.Code != 200 || !strings.Contains(w.Body.String(), "journal-owned-step") || w.Header().Get("Cache-Control") != "no-store, private" {
							t.Fatalf("persisted inspection status=%d body=%s", w.Code, w.Body.String())
						}
					} else if w.Code != 401 && w.Code != 403 {
						t.Fatalf("changed authority status=%d body=%s", w.Code, w.Body.String())
					} else if strings.Contains(w.Body.String(), "journal-owned") || strings.Contains(w.Body.String(), "snapshot_sha256") {
						t.Fatal("withheld journal leaked")
					}
					if strings.Contains(w.Body.String(), "PRIVATE_") {
						t.Fatal("raw journal payload leaked")
					}
				})
			}
		})
	}
}
