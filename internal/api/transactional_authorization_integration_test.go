//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	dbsqlite "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

type transactionAuthClock struct{}

func (transactionAuthClock) UnixMilli() int64 { return 1000000 }
func (transactionAuthClock) Now() time.Time   { return time.UnixMilli(1000000) }

func transactionAuthFixture(t *testing.T) *dbsqlite.DB {
	t.Helper()
	db, err := dbsqlite.Open(filepath.Join(t.TempDir(), "transaction-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('tx-auth-ws','Auth','active',1,1),('tx-auth-other','Other','active',1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('tx-auth-human','human','Human','active',1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tx-auth-ws','tx-auth-human','active',1,1),('tx-auth-other','tx-auth-human','active',1,1)`,
	} {
		if _, err = db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	// Existing migrations may seed standard roles; use those names when present.
	for _, name := range []string{"Admin", "Approver"} {
		if _, err = db.SQL().Exec(`INSERT INTO roles(id,role_class,name,definition_json,created_at,updated_at) SELECT ?, 'human',?,'{}',1,1 WHERE NOT EXISTS(SELECT 1 FROM roles WHERE name=?)`, "tx-role-"+name, name, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL().Exec(`INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) SELECT 'tx-auth-human',id,'tx-auth-other',1 FROM roles WHERE name='Admin'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,expires_at,created_at) VALUES('tx-auth-credential','tx-auth-human','Test',?,'{"workspaces":["tx-auth-ws"]}','{"capabilities":["team.*","task.read"]}',2000000,1)`, transactionAuthHash("transaction-bearer")); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]string{"csrf_hash": transactionAuthHash("transaction-csrf"), "username": "auth-human"})
	if _, err = db.SQL().Exec(`INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES('tx-auth-session','tx-auth-human',?,'local_password','password',1,1,1500000,2000000,?)`, transactionAuthHash("transaction-session"), string(metadata)); err != nil {
		t.Fatal(err)
	}
	return db
}

// Every verifier query must use this supplied transaction. Its Exec method is
// deliberately unusable, and the verifier authorizers have no database handle.
type transactionAuthReadOnly struct {
	storage.Tx
	queries int
}

func (r *transactionAuthReadOnly) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("authorization attempted a write")
}
func (r *transactionAuthReadOnly) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	r.queries++
	return r.Tx.QueryRowContext(ctx, q, args...)
}

func TestTransactionalAuthorizationBearer(t *testing.T) {
	db := transactionAuthFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer transaction-bearer")
	original, err := NewBearerAuthorizer(db.SQL(), transactionAuthClock{}).Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewBearerAuthorizer(nil, transactionAuthClock{})
	for _, scenario := range []string{"allowed", "array_wildcards", "revoked", "expired", "principal_disabled", "principal_type", "principal_identity", "credential_identity", "auth_method", "token", "hash_replaced", "workspace_scope", "capability_scope", "oversize_scope", "invalid_scope", "membership", "tenant", "system_without_membership"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := db.SQL().BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			request := req.Clone(context.Background())
			identity := original
			query := ""
			switch scenario {
			case "array_wildcards":
				query = `UPDATE api_credentials SET workspace_scope_json='["*"]',capability_scope_json='["*"]'`
			case "revoked":
				query = `UPDATE api_credentials SET revoked_at=2`
			case "expired":
				query = `UPDATE api_credentials SET expires_at=1000000`
			case "principal_disabled":
				query = `UPDATE principals SET status='disabled' WHERE id='tx-auth-human'`
			case "principal_type":
				query = `UPDATE principals SET principal_type='system' WHERE id='tx-auth-human'`
			case "principal_identity":
				identity.PrincipalID = "replacement"
			case "credential_identity":
				identity.CredentialID = "replacement"
			case "auth_method":
				identity.AuthMethod = "session"
			case "token":
				request.Header.Set("Authorization", "Bearer replacement")
			case "hash_replaced":
				query = `UPDATE api_credentials SET credential_hash='replacement'`
			case "workspace_scope":
				query = `UPDATE api_credentials SET workspace_scope_json='["other"]'`
			case "capability_scope":
				query = `UPDATE api_credentials SET capability_scope_json='["task.read"]'`
			case "oversize_scope":
				query = `UPDATE api_credentials SET capability_scope_json=json_array('*',printf('%17000s','x'))`
			case "invalid_scope":
				query = `UPDATE api_credentials SET workspace_scope_json='true'`
			case "membership":
				query = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tx-auth-ws'`
			case "tenant":
				query = `UPDATE workspaces SET status='suspended' WHERE id='tx-auth-ws'`
			case "system_without_membership":
				identity.PrincipalType = "system"
				query = `UPDATE principals SET principal_type='system' WHERE id='tx-auth-human'; DELETE FROM workspace_memberships WHERE workspace_id='tx-auth-ws'`
			}
			if query != "" {
				if _, err = tx.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			supplied := &transactionAuthReadOnly{Tx: tx}
			err = verifier.AuthorizeWorkspaceInTransaction(context.Background(), supplied, request, identity, "tx-auth-ws", "team.write", "task.read")
			if (err == nil) != (scenario == "allowed" || scenario == "array_wildcards") {
				t.Fatalf("authorization outcome %v", err)
			}
			if scenario == "allowed" && supplied.queries < 2 {
				t.Fatal("supplied transaction not used")
			}
		})
	}
}

func TestTransactionalAuthorizationSession(t *testing.T) {
	db := transactionAuthFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.AddCookie(&http.Cookie{Name: WebSessionCookie, Value: "transaction-session"})
	request.Header.Set("X-OnePane-CSRF", "transaction-csrf")
	real := NewHybridAuthorizer(NewBearerAuthorizer(db.SQL(), transactionAuthClock{}), webauth.NewService(db.SQL(), db, transactionAuthClock{}))
	original, err := real.Authenticate(request)
	if err != nil {
		t.Fatal(err)
	}
	// Force last_seen older than the refresh threshold after normal authentication.
	if _, err = db.SQL().Exec(`UPDATE auth_sessions SET last_seen_at=1,idle_expires_at=1500000; CREATE TRIGGER tx_auth_no_refresh BEFORE UPDATE ON auth_sessions BEGIN SELECT RAISE(ABORT,'authorization refreshed session'); END`); err != nil {
		t.Fatal(err)
	}
	verifier := NewHybridAuthorizer(NewBearerAuthorizer(nil, transactionAuthClock{}), nil)
	for _, scenario := range []string{"allowed", "read_without_csrf", "revoked", "expired", "idle_expired", "principal_disabled", "principal_type", "principal_identity", "credential_identity", "auth_method", "token", "hash_replaced", "missing_cookie", "csrf", "missing_csrf", "malformed_metadata", "oversize_metadata", "membership", "tenant", "bearer_no_fallback", "admin_other_active_membership", "admin_inactive_membership", "approver", "base_read", "base_write_denied"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := db.SQL().BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// Remove the update rejection only for deliberate test-side session changes.
			if _, err = tx.Exec(`DROP TRIGGER tx_auth_no_refresh`); err != nil {
				t.Fatal(err)
			}
			req := request.Clone(context.Background())
			identity := original
			capability := "team.write"
			query := ""
			switch scenario {
			case "read_without_csrf":
				req.Method = http.MethodGet
				req.Header.Del("X-OnePane-CSRF")
			case "revoked":
				query = `UPDATE auth_sessions SET revoked_at=2`
			case "expired":
				query = `UPDATE auth_sessions SET expires_at=1000000`
			case "idle_expired":
				query = `UPDATE auth_sessions SET idle_expires_at=1000000`
			case "principal_disabled":
				query = `UPDATE principals SET status='disabled' WHERE id='tx-auth-human'`
			case "principal_type":
				query = `UPDATE principals SET principal_type='system' WHERE id='tx-auth-human'`
			case "principal_identity":
				identity.PrincipalID = "replacement"
			case "credential_identity":
				identity.CredentialID = "replacement"
			case "auth_method":
				identity.AuthMethod = "bearer"
			case "token":
				req.Header.Set("Cookie", WebSessionCookie+"=replacement")
			case "hash_replaced":
				query = `UPDATE auth_sessions SET token_hash='replacement'`
			case "missing_cookie":
				req.Header.Del("Cookie")
			case "csrf":
				req.Header.Set("X-OnePane-CSRF", "replacement")
			case "missing_csrf":
				req.Header.Del("X-OnePane-CSRF")
			case "malformed_metadata":
				query = `UPDATE auth_sessions SET metadata_json='[]'`
			case "oversize_metadata":
				query = `UPDATE auth_sessions SET metadata_json=json_object('csrf_hash','` + transactionAuthHash("transaction-csrf") + `','padding',printf('%17000s','x'))`
			case "membership":
				query = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tx-auth-ws'`
			case "tenant":
				query = `UPDATE workspaces SET status='suspended' WHERE id='tx-auth-ws'`
			case "bearer_no_fallback":
				req.Header.Set("Authorization", "Bearer replacement")
			case "admin_inactive_membership":
				query = `UPDATE workspace_memberships SET status='suspended' WHERE workspace_id='tx-auth-other'`
			case "approver":
				capability = "verification.accept"
				query = `DELETE FROM principal_roles; INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) SELECT 'tx-auth-human',id,'tx-auth-other',1 FROM roles WHERE name='Approver'`
			case "base_read":
				capability = "project.read"
				query = `DELETE FROM principal_roles`
			case "base_write_denied":
				query = `DELETE FROM principal_roles`
			}
			if query != "" {
				if _, err = tx.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = tx.Exec(`CREATE TRIGGER tx_auth_no_refresh BEFORE UPDATE ON auth_sessions BEGIN SELECT RAISE(ABORT,'authorization refreshed session'); END`); err != nil {
				t.Fatal(err)
			}
			supplied := &transactionAuthReadOnly{Tx: tx}
			err = verifier.AuthorizeWorkspaceInTransaction(context.Background(), supplied, req, identity, "tx-auth-ws", capability)
			allowed := scenario == "allowed" || scenario == "read_without_csrf" || scenario == "admin_other_active_membership" || scenario == "approver" || scenario == "base_read"
			if (err == nil) != allowed {
				t.Fatalf("authorization outcome %v", err)
			}
			var lastSeen, idle int64
			if err = tx.QueryRow(`SELECT last_seen_at,idle_expires_at FROM auth_sessions WHERE id='tx-auth-session'`).Scan(&lastSeen, &idle); err != nil {
				t.Fatal(err)
			}
			if lastSeen != 1 || (scenario != "idle_expired" && idle != 1500000) {
				t.Fatal("authorization refreshed persisted session")
			}
			if allowed && supplied.queries < 3 {
				t.Fatal("supplied transaction not used")
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tx, err := db.SQL().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = verifier.AuthorizeWorkspaceInTransaction(canceled, tx, request, original, "tx-auth-ws", "team.write"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
