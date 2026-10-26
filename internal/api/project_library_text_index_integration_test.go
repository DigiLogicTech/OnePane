//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

func textLibraryHTTPFixture(t *testing.T, body []byte, media string, wrap func(artifact.BlobStore) artifact.BlobStore) *publishedLibraryFixture {
	t.Helper()
	f := publishedLibraryHTTPFixture(t, wrap)
	actor := "human"
	raw, err := f.artifacts.Create(context.Background(), artifact.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, MediaType: media, CreatedBy: &actor, Label: policy.DataLabel{WorkspaceID: "tenant", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUntrustedContent}}, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := f.projects.ImportLibraryAsset(context.Background(), projectworkspace.ImportLibraryAssetCommand{ProjectID: f.projectID, SourceWorkspaceID: f.workspaceID, Name: "Source text", MIMEType: media, ArtifactID: raw.ID, ContentHash: raw.ContentHash, SizeBytes: raw.SizeBytes, ActorPrincipalID: actor})
	if err != nil {
		t.Fatal(err)
	}
	f.assetID, f.artifactID, f.contentHash, f.body = asset.ID, raw.ID, raw.ContentHash, body
	return f
}

func textLibraryHTTPAuth(t *testing.T, f *publishedLibraryFixture, method string) func(*http.Request) {
	t.Helper()
	now := clock.Real{}.UnixMilli()
	bearer := NewBearerAuthorizer(f.db.SQL(), clock.Real{})
	if method == "bearer" {
		if _, err := f.db.SQL().Exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at) VALUES('text-api','human','Disposable text test',?,'["tenant"]','["project.read","project.write"]',?)`, transactionAuthHash("disposable-text-token"), now); err != nil {
			t.Fatal(err)
		}
		f.server = NewServer(f.projects, fakeEvents{}, bearer)
		f.server.SetLibraryArtifacts(f.artifacts)
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer disposable-text-token") }
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO roles(id,role_class,name,definition_json,revision,created_at,updated_at) VALUES('text-admin','human','Admin','{}',1,?,?)`, []any{now, now}},
		{`INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) VALUES('human','text-admin','tenant',?)`, []any{now}},
		{`INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES('text-session','human',?,'password','single_factor',?,?,?,?,?)`, []any{transactionAuthHash("disposable-text-session"), now, now, now + 600000, now + 3600000, `{"csrf_hash":"` + transactionAuthHash("disposable-text-csrf") + `"}`}},
	} {
		if _, err := f.db.SQL().Exec(statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.server = NewServer(f.projects, fakeEvents{}, NewHybridAuthorizer(bearer, webauth.NewService(f.db.SQL(), f.db, clock.Real{})))
	f.server.SetLibraryArtifacts(f.artifacts)
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: WebSessionCookie, Value: "disposable-text-session"})
		r.Header.Set("X-OnePane-CSRF", "disposable-text-csrf")
	}
}

func textLibraryPath(f *publishedLibraryFixture, tail string) string {
	return "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/" + f.assetID + "/versions/1/" + tail
}

func textLibraryHTTP(t *testing.T, f *publishedLibraryFixture, method, tail string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, textLibraryPath(f, tail), nil)
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func decodeTextIndex(t *testing.T, w *httptest.ResponseRecorder) projectworkspace.LibraryTextIndex {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("index HTTP status=%d body=%s", w.Code, w.Body.String())
	}
	var index projectworkspace.LibraryTextIndex
	if err := json.Unmarshal(w.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("text index lacks private headers")
	}
	if strings.Contains(w.Body.String(), "storage_uri") || strings.Contains(w.Body.String(), "artifact:") {
		t.Fatal("private storage identity exposed")
	}
	return index
}

func TestLibraryTextIndexHTTPRealAuthPersistedCitationFlow(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			body := []byte("Opening line\r\nPRIVATE_LIBRARY_TEXT café <script>literal</script>\r\nLast line\n")
			f := textLibraryHTTPFixture(t, body, "text/markdown; charset=utf-8", nil)
			auth := textLibraryHTTPAuth(t, f, method)
			before := decodeTextIndex(t, textLibraryHTTP(t, f, "GET", "index", auth))
			if before.Status != "not_indexed" || before.LineCount != 0 {
				t.Fatalf("missing index invented content %+v", before)
			}
			if w := textLibraryHTTP(t, f, "GET", "search?q=PRIVATE_LIBRARY_TEXT", auth); w.Code != 409 {
				t.Fatalf("unindexed content search status=%d body=%s", w.Code, w.Body.String())
			}
			indexed := decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			if indexed.Status != "ready" || indexed.LineCount != 3 || indexed.AssetID != f.assetID || indexed.ContentHash != f.contentHash || indexed.SizeBytes != int64(len(body)) || indexed.ParserVersion != 1 {
				t.Fatalf("wrong persisted text identity %+v", indexed)
			}
			retry := decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			if retry != indexed {
				t.Fatalf("identical retry replaced index %+v %+v", indexed, retry)
			}
			// Reconstruct the actual domain service before reading persisted text.
			f.projects = projectworkspace.NewService(f.db.SQL(), f.db, clock.Real{})
			f.server.projects = f.projects
			w := textLibraryHTTP(t, f, "GET", "search?q="+url.QueryEscape("PRIVATE_LIBRARY_TEXT café"), auth)
			if w.Code != 200 {
				t.Fatalf("persisted search status=%d body=%s", w.Code, w.Body.String())
			}
			var got projectworkspace.LibraryTextSearch
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Index != indexed || got.Query != "PRIVATE_LIBRARY_TEXT café" || len(got.Lines) != 1 || got.HasMore {
				t.Fatalf("wrong scoped text match %+v", got)
			}
			line := got.Lines[0]
			if line.LineNumber != 2 || line.ByteStart != 14 || line.Text != "PRIVATE_LIBRARY_TEXT café <script>literal</script>\r" || line.ByteEnd != line.ByteStart+int64(len(line.Text)) || string(body[line.ByteStart:line.ByteEnd]) != line.Text {
				t.Fatalf("citation not original byte-bound line %+v", line)
			}
			digest := sha256.Sum256([]byte(line.Text))
			if line.LineHash != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatalf("line citation hash mismatch %+v", line)
			}
			var indexes, lines, audits int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?),(SELECT COUNT(*) FROM project_library_text_lines WHERE asset_id=?),(SELECT COUNT(*) FROM events WHERE event_type='project.library_text_indexed' AND aggregate_id=?)`, f.assetID, f.assetID, f.assetID).Scan(&indexes, &lines, &audits); err != nil || indexes != 1 || lines != 3 || audits != 1 {
				t.Fatalf("index/line/audit atomic/idempotent counts %d/%d/%d %v", indexes, lines, audits, err)
			}
		})
	}
}

func TestLibraryTextIndexHTTPTruthfulBoundedStatusesAndLiteralSearch(t *testing.T) {
	for _, tc := range []struct {
		name, media, status string
		body                []byte
	}{
		{"unsupported_pdf", "application/pdf", "unsupported", []byte("%PDF synthetic bytes")},
		{"invalid_utf8", "text/plain", "invalid_text", []byte{0xff, 0xfe}},
		{"binary_nul", "text/plain", "invalid_text", []byte("private\x00text")},
		{"oversize_source", "text/plain", "too_large", bytes.Repeat([]byte("x"), (1<<20)+1)},
		{"oversize_line", "text/plain", "too_large", bytes.Repeat([]byte("x"), 16385)},
		{"too_many_lines", "text/plain", "too_large", bytes.Repeat([]byte("\n"), 16385)},
		{"empty", "text/plain", "ready", []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, tc.body, tc.media, nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			got := decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			if got.Status != tc.status || got.LineCount != 0 {
				t.Fatalf("incorrect parser disposition %+v", got)
			}
			if strings.Contains(string(tc.body), "private") && strings.Contains(textLibraryHTTP(t, f, "GET", "index", auth).Body.String(), "private") {
				t.Fatal("status exposed source text")
			}
			w := textLibraryHTTP(t, f, "GET", "search?q=x", auth)
			if tc.status != "ready" && w.Code != 409 {
				t.Fatalf("nonready index searchable status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	f := textLibraryHTTPFixture(t, []byte(strings.Repeat("match % _ SELECT café\n", 10)), "text/plain", nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	for _, q := range []string{"match", "%", "_", "SELECT", "café"} {
		w := textLibraryHTTP(t, f, "GET", "search?q="+url.QueryEscape(q), auth)
		var got projectworkspace.LibraryTextSearch
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Lines) != 8 || !got.HasMore {
			t.Fatalf("literal bounded match q=%q status=%d body=%s", q, w.Code, w.Body.String())
		}
	}
	for _, q := range []string{"", " ", strings.Repeat("a", 257), "\x00", "\xff"} {
		w := textLibraryHTTP(t, f, "GET", "search?q="+url.QueryEscape(q), auth)
		if w.Code != 400 {
			t.Fatalf("invalid query accepted %q status=%d", q, w.Code)
		}
	}
}

func TestLibraryTextIndexHTTPRevocationDuringManagedSourceCollection(t *testing.T) {
	for _, stage := range []string{"verify", "open", "read"} {
		for _, scenario := range []string{"credential", "grant", "project", "workspace", "membership", "write_scope", "project_tenant", "artifact_archived", "artifact_quarantined", "artifact_hash", "artifact_storage"} {
			t.Run(stage+"/"+scenario, func(t *testing.T) {
				var store *inspectedLibraryStore
				f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT never leak revoked source"), "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
					store = &inspectedLibraryStore{BlobStore: base}
					return store
				})
				auth := textLibraryHTTPAuth(t, f, "bearer")
				if scenario == "project_tenant" {
					for _, query := range []string{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('text-foreign','Foreign','active',1,1,1)`, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('text-foreign','human','active',1,1)`} {
						if _, err := f.db.SQL().Exec(query); err != nil {
							t.Fatal(err)
						}
					}
				}
				fired := false
				store.after = func(at string) {
					if fired || at != stage {
						return
					}
					fired = true
					query := ""
					var args []any
					switch scenario {
					case "credential":
						query = `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`
					case "grant":
						query = `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`
						args = []any{f.assetID}
					case "project":
						query = `UPDATE projects SET status='archived' WHERE id=?`
						args = []any{f.projectID}
					case "workspace":
						query = `UPDATE project_workspaces SET status='archived' WHERE id=?`
						args = []any{f.workspaceID}
					case "membership":
						query = `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`
					case "write_scope":
						query = `UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='text-api'`
					case "project_tenant":
						query = `UPDATE projects SET workspace_id='text-foreign' WHERE id=?`
						args = []any{f.projectID}
					case "artifact_archived":
						query = `UPDATE artifacts SET status='archived' WHERE id=?`
						args = []any{f.artifactID}
					case "artifact_quarantined":
						query = `UPDATE artifacts SET status='quarantined' WHERE id=?`
						args = []any{f.artifactID}
					case "artifact_hash":
						query = `UPDATE artifacts SET content_hash='sha256:'||printf('%064x',1) WHERE id=?`
						args = []any{f.artifactID}
					case "artifact_storage":
						query = `UPDATE artifacts SET storage_ref='private/replaced-source' WHERE id=?`
						args = []any{f.artifactID}
					}
					if _, err := f.db.SQL().Exec(query, args...); err != nil {
						t.Fatal(err)
					}
				}
				w := textLibraryHTTP(t, f, "POST", "index", auth)
				if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") {
					t.Fatalf("changed source authority accepted fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
				}
				var count int
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("revoked source indexed count=%d err=%v", count, err)
				}
			})
		}
	}
}

type textLibraryTransactionHook struct {
	*projectworkspace.Service
	inTransaction func(context.Context, storage.Tx) error
	after         func()
}

func (s *textLibraryTransactionHook) IndexWorkspaceLibraryText(ctx context.Context, c projectworkspace.IndexLibraryTextCommand) (projectworkspace.LibraryTextIndex, error) {
	if s.inTransaction != nil {
		original := c.Selection.Authorize
		c.Selection.Authorize = func(ctx context.Context, tx storage.Tx) error {
			if err := s.inTransaction(ctx, tx); err != nil {
				return err
			}
			return original(ctx, tx)
		}
	}
	result, err := s.Service.IndexWorkspaceLibraryText(ctx, c)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}
func (s *textLibraryTransactionHook) WorkspaceLibraryTextIndex(ctx context.Context, selection projectworkspace.LibraryTextSelection) (projectworkspace.LibraryTextIndex, error) {
	result, err := s.Service.WorkspaceLibraryTextIndex(ctx, selection)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}
func (s *textLibraryTransactionHook) SearchWorkspaceLibraryText(ctx context.Context, selection projectworkspace.LibraryTextSelection, q string) (projectworkspace.LibraryTextSearch, error) {
	result, err := s.Service.SearchWorkspaceLibraryText(ctx, selection, q)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}

func TestLibraryTextIndexHTTPSameTransactionCredentialAndFinalPublicationFences(t *testing.T) {
	for _, scenario := range []string{"revoked", "changed_scope", "changed_token", "changed_identity"} {
		t.Run("transaction/"+scenario, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT actual transaction"), "text/plain", nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			query := map[string]string{
				"revoked":          `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`,
				"changed_scope":    `UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='text-api'`,
				"changed_token":    `UPDATE api_credentials SET credential_hash='replaced' WHERE id='text-api'`,
				"changed_identity": `UPDATE principals SET principal_type='agent' WHERE id='human'`,
			}[scenario]
			called := false
			f.server.projects = &textLibraryTransactionHook{Service: f.projects, inTransaction: func(ctx context.Context, tx storage.Tx) error {
				called = true
				_, err := tx.ExecContext(ctx, query)
				return err
			}}
			w := textLibraryHTTP(t, f, "POST", "index", auth)
			if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") {
				t.Fatalf("same transaction credential bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
			var count int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed verifier persisted index count=%d %v", count, err)
			}
		})
	}
	for _, operation := range []string{"index", "status", "search"} {
		t.Run("final/"+operation, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT final response"), "text/plain", nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			if operation != "index" {
				decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			}
			called := false
			f.server.projects = &textLibraryTransactionHook{Service: f.projects, after: func() {
				called = true
				if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`); err != nil {
					t.Fatal(err)
				}
			}}
			method, tail := "GET", "index"
			if operation == "index" {
				method = "POST"
			}
			if operation == "search" {
				tail = "search?q=PRIVATE_LIBRARY_TEXT"
			}
			w := textLibraryHTTP(t, f, method, tail, auth)
			if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") {
				t.Fatalf("final credential bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
			var count int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("successful index disappeared count=%d %v", count, err)
			}
		})
	}
}

func TestLibraryTextIndexHTTPWithholdsAfterSourceMetadataChanges(t *testing.T) {
	for _, operation := range []string{"index", "status", "search"} {
		for _, scenario := range []string{"artifact_archived", "artifact_hash", "artifact_storage", "project_tenant", "grant"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT final metadata"), "text/plain", nil)
				auth := textLibraryHTTPAuth(t, f, "bearer")
				if operation != "index" {
					decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
				}
				if scenario == "project_tenant" {
					for _, query := range []string{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('text-foreign','Foreign','active',1,1,1)`, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('text-foreign','human','active',1,1)`} {
						if _, err := f.db.SQL().Exec(query); err != nil {
							t.Fatal(err)
						}
					}
				}
				called := false
				f.server.projects = &textLibraryTransactionHook{Service: f.projects, after: func() {
					called = true
					query := map[string]string{"artifact_archived": `UPDATE artifacts SET status='archived' WHERE id=?`, "artifact_hash": `UPDATE artifacts SET content_hash='sha256:'||printf('%064x',1) WHERE id=?`, "artifact_storage": `UPDATE artifacts SET storage_ref='private/replaced-source' WHERE id=?`, "project_tenant": `UPDATE projects SET workspace_id='text-foreign' WHERE id=?`, "grant": `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`}[scenario]
					id := f.artifactID
					if scenario == "project_tenant" {
						id = f.projectID
					}
					if scenario == "grant" {
						id = f.assetID
					}
					if _, err := f.db.SQL().Exec(query, id); err != nil {
						t.Fatal(err)
					}
				}}
				method, tail := "GET", "index"
				if operation == "index" {
					method = "POST"
				}
				if operation == "search" {
					tail = "search?q=PRIVATE_LIBRARY_TEXT"
				}
				w := textLibraryHTTP(t, f, method, tail, auth)
				if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") {
					t.Fatalf("final source metadata bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
				}
				var count int
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("committed index changed count=%d %v", count, err)
				}
			})
		}
	}
}

func TestLibraryTextIndexHTTPRejectsMissingVerifierClientBytesAndBrowserCSRF(t *testing.T) {
	f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT original"), "text/plain", nil)
	if w := textLibraryHTTP(t, f, "POST", "index", func(*http.Request) {}); w.Code != 503 {
		t.Fatalf("missing transaction verifier accepted status=%d", w.Code)
	}
	auth := textLibraryHTTPAuth(t, f, "session")
	r := httptest.NewRequest("POST", textLibraryPath(f, "index"), nil)
	auth(r)
	r.Header.Del("X-OnePane-CSRF")
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("missing browser CSRF accepted status=%d body=%s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", textLibraryPath(f, "index"), strings.NewReader(`{"content":"replacement","path":"C:/private"}`))
	auth(r)
	w = httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("client replacement bytes accepted status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid request persisted index count=%d %v", count, err)
	}
}
