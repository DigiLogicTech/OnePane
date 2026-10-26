//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type workspaceTextSearchHTTPResult struct {
	Query   string `json:"query"`
	Matches []struct {
		Index projectworkspace.LibraryTextIndex `json:"index"`
		Line  projectworkspace.LibraryTextLine  `json:"line"`
	} `json:"matches"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

func appendWorkspaceTextSearchAsset(t *testing.T, f *publishedLibraryFixture, body []byte) string {
	t.Helper()
	actor := "human"
	raw, err := f.artifacts.Create(context.Background(), artifact.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, MediaType: "text/plain", CreatedBy: &actor, Label: policy.DataLabel{WorkspaceID: "tenant", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUntrustedContent}}, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := f.projects.ImportLibraryAsset(context.Background(), projectworkspace.ImportLibraryAssetCommand{ProjectID: f.projectID, SourceWorkspaceID: f.workspaceID, Name: "Workspace text search source", MIMEType: "text/plain", ArtifactID: raw.ID, ContentHash: raw.ContentHash, SizeBytes: raw.SizeBytes, ActorPrincipalID: actor})
	if err != nil {
		t.Fatal(err)
	}
	f.assetID, f.artifactID, f.contentHash, f.body = asset.ID, raw.ID, raw.ContentHash, body
	return asset.ID
}

func workspaceTextSearchPath(f *publishedLibraryFixture, query, cursor string) string {
	path := "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/search-text?q=" + url.QueryEscape(query)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	return path
}

func workspaceTextSearchHTTP(t *testing.T, f *publishedLibraryFixture, query, cursor string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", workspaceTextSearchPath(f, query, cursor), nil)
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func decodeWorkspaceTextSearch(t *testing.T, w *httptest.ResponseRecorder) workspaceTextSearchHTTPResult {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("workspace text search status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("private search headers missing %+v", w.Header())
	}
	if strings.Contains(w.Body.String(), "storage_uri") || strings.Contains(w.Body.String(), "artifact:") || strings.Contains(w.Body.String(), "next_position") || strings.Contains(w.Body.String(), "asset_after") || strings.Contains(w.Body.String(), "version_after") {
		t.Fatalf("internal Library navigation/storage leaked: %s", w.Body.String())
	}
	var result workspaceTextSearchHTTPResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) > 8 {
		t.Fatalf("unbounded text matches %d", len(result.Matches))
	}
	if result.HasMore != (result.NextCursor != "") {
		t.Fatalf("continuation token inconsistent %+v", result)
	}
	return result
}

// The first eight metadata assets are deliberately unindexed. They must consume
// the page budget rather than letting an unlimited search skip past history.
func workspaceTextSearchPagedFixture(t *testing.T, method string) (*publishedLibraryFixture, func(*http.Request), []string) {
	t.Helper()
	f := textLibraryHTTPFixture(t, []byte("UNINDEXED_PRIVATE_SOURCE"), "text/plain", nil)
	var hidden []string
	rows, err := f.db.SQL().Query(`SELECT id FROM project_library_assets WHERE project_id=? ORDER BY id`, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		hidden = append(hidden, id)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	auth := textLibraryHTTPAuth(t, f, method)
	for i := 0; i < 7; i++ {
		body := []byte(fmt.Sprintf("UNINDEXED_PRIVATE_SOURCE %d", i))
		if i == 6 {
			body = []byte(strings.Repeat("WORKSPACE_LITERAL_MATCH café <script>plain</script>\n", 10))
		}
		id := appendWorkspaceTextSearchAsset(t, f, body)
		if i < 6 {
			hidden = append(hidden, id)
		}
	}
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	if len(hidden) != 8 {
		t.Fatalf("expected eight hidden unindexed assets, got%d", len(hidden))
	}
	return f, auth, hidden
}

func TestWorkspaceLibraryTextSearchHTTPRealAuthEmptyPagesAndOpaqueContinuation(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f, auth, hidden := workspaceTextSearchPagedFixture(t, method)
			query := "WORKSPACE_LITERAL_MATCH café"
			firstRaw := workspaceTextSearchHTTP(t, f, query, "", auth)
			first := decodeWorkspaceTextSearch(t, firstRaw)
			if first.Query != query || len(first.Matches) != 0 || !first.HasMore {
				t.Fatalf("raw asset budget silently skipped unindexed assets %+v", first)
			}
			for _, id := range hidden {
				if strings.Contains(firstRaw.Body.String(), id) || strings.Contains(first.NextCursor, id) {
					t.Fatal("unindexed private asset leaked through empty-page navigation")
				}
			}
			if strings.Contains(firstRaw.Body.String(), "UNINDEXED_PRIVATE_SOURCE") || strings.Contains(first.NextCursor, query) || len(first.NextCursor) < 24 {
				t.Fatal("opaque empty-page navigation exposed source or position")
			}
			cursor := first.NextCursor
			seen := map[int]bool{}
			pages := 0
			for cursor != "" && pages < 6 {
				page := decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, query, cursor, auth))
				pages++
				for _, match := range page.Matches {
					if match.Index.AssetID != f.assetID || match.Index.ContentHash != f.contentHash || match.Index.Status != "ready" || match.Line.LineNumber < 1 || match.Line.LineNumber > 10 || seen[match.Line.LineNumber] || !strings.Contains(match.Line.Text, query) {
						t.Fatalf("invalid or duplicate persisted citation %+v", match)
					}
					seen[match.Line.LineNumber] = true
				}
				cursor = page.NextCursor
			}
			if cursor != "" || len(seen) != 10 || pages < 2 {
				t.Fatalf("bounded same-version continuation lost work seen=%v pages=%d cursor=%q", seen, pages, cursor)
			}
		})
	}
}

func TestWorkspaceLibraryTextSearchHTTPBindsCursorToQueryScopeAndOriginalCredential(t *testing.T) {
	f, auth, _ := workspaceTextSearchPagedFixture(t, "bearer")
	query := "WORKSPACE_LITERAL_MATCH"
	first := decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, query, "", auth))
	for _, scenario := range []string{"query", "workspace", "project", "credential", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			cursor := first.NextCursor
			path := workspaceTextSearchPath(f, query, cursor)
			requestAuth := auth
			switch scenario {
			case "query":
				path = workspaceTextSearchPath(f, "different private query", cursor)
			case "workspace":
				workspace, err := f.projects.CreateWorkspaceView(context.Background(), projectworkspace.CreateWorkspaceViewCommand{ProjectID: f.projectID, Name: "Other search view", ActorPrincipalID: "human"})
				if err != nil {
					t.Fatal(err)
				}
				path = strings.Replace(path, "/workspaces/"+f.workspaceID+"/", "/workspaces/"+workspace.ID+"/", 1)
			case "project":
				project, err := f.projects.CreateProject(context.Background(), projectworkspace.CreateProjectCommand{WorkspaceID: "tenant", Name: "Other search project", CreatedBy: "human"})
				if err != nil {
					t.Fatal(err)
				}
				workspaces, err := f.projects.WorkspaceViews(context.Background(), project.ID)
				if err != nil || len(workspaces) != 1 {
					t.Fatalf("new project Workspace %v %+v", err, workspaces)
				}
				path = "/v1/projects/" + project.ID + "/workspaces/" + workspaces[0].ID + "/library/search-text?q=" + url.QueryEscape(query) + "&cursor=" + url.QueryEscape(cursor)
			case "credential":
				if _, err := f.db.SQL().Exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at) VALUES('text-second-api','human','Second valid credential',?,'["tenant"]','["project.read"]',1)`, transactionAuthHash("second-text-token")); err != nil {
					t.Fatal(err)
				}
				requestAuth = func(r *http.Request) { r.Header.Set("Authorization", "Bearer second-text-token") }
			case "unknown":
				path = workspaceTextSearchPath(f, query, "unknown-cursor-must-restart")
			}
			r := httptest.NewRequest("GET", path, nil)
			requestAuth(r)
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, r)
			if (w.Code != 403 && w.Code != 409) || strings.Contains(w.Body.String(), "WORKSPACE_LITERAL_MATCH") || strings.Contains(w.Body.String(), f.assetID) {
				t.Fatalf("cursor binding accepted %s status=%d body=%s", scenario, w.Code, w.Body.String())
			}
		})
	}
	// Invalid cursor reuse must not destroy the original authorized navigation.
	valid := decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, query, first.NextCursor, auth))
	if len(valid.Matches) != 8 {
		t.Fatalf("invalid cursor attempt destroyed original navigation %+v", valid)
	}
}

type workspaceTextSearchHook struct {
	*projectworkspace.Service
	before, after func()
}

func (s *workspaceTextSearchHook) SearchWorkspaceIndexedLibraryText(ctx context.Context, c projectworkspace.LibraryTextSelection, q string, p projectworkspace.LibraryTextSearchPosition) (projectworkspace.LibraryWorkspaceTextSearch, error) {
	if s.before != nil {
		s.before()
	}
	result, err := s.Service.SearchWorkspaceIndexedLibraryText(ctx, c, q, p)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}

func TestWorkspaceLibraryTextSearchHTTPRevalidatesRevocationDuringSourceVerification(t *testing.T) {
	for _, stage := range []string{"verify", "open"} {
		for _, scenario := range []string{"credential", "grant", "project", "workspace", "raw_archived", "raw_hash", "raw_storage"} {
			t.Run(stage+"/"+scenario, func(t *testing.T) {
				var store *inspectedLibraryStore
				f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT search-only evidence"), "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
					store = &inspectedLibraryStore{BlobStore: base}
					return store
				})
				auth := textLibraryHTTPAuth(t, f, "bearer")
				decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
				fired := false
				store.after = func(at string) {
					if fired || at != stage {
						return
					}
					fired = true
					query := map[string]string{"credential": `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`, "grant": `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, "project": `UPDATE projects SET status='archived' WHERE id=?`, "workspace": `UPDATE project_workspaces SET status='archived' WHERE id=?`, "raw_archived": `UPDATE artifacts SET status='archived' WHERE id=?`, "raw_hash": `UPDATE artifacts SET content_hash='sha256:'||printf('%064x',1) WHERE id=?`, "raw_storage": `UPDATE artifacts SET storage_ref='private/replaced-search-source' WHERE id=?`}[scenario]
					var args []any
					switch scenario {
					case "grant":
						args = []any{f.assetID}
					case "project":
						args = []any{f.projectID}
					case "workspace":
						args = []any{f.workspaceID}
					case "raw_archived", "raw_hash", "raw_storage":
						args = []any{f.artifactID}
					}
					if _, err := f.db.SQL().Exec(query, args...); err != nil {
						t.Fatal(err)
					}
				}
				w := workspaceTextSearchHTTP(t, f, "PRIVATE_LIBRARY_TEXT", "", auth)
				if !fired || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") || strings.Contains(w.Body.String(), f.assetID) {
					t.Fatalf("revoked slow verification result disclosed fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
				}
				if w.Code == 200 {
					var got workspaceTextSearchHTTPResult
					if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Matches) != 0 || got.NextCursor != "" {
						t.Fatalf("revoked slow verification returned actionable page %+v %v", got, err)
					}
				}
			})
		}
	}
}

func TestWorkspaceLibraryTextSearchHTTPRechecksOriginalTenantAndComparableCollection(t *testing.T) {
	for _, scenario := range []string{"before_tenant", "after_grant", "after_credential", "after_named_workspace", "after_raw"} {
		t.Run(scenario, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT comparable collection"), "text/plain", nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			if scenario == "before_tenant" {
				for _, q := range []string{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('search-foreign','Foreign','active',1,1,1)`, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('search-foreign','human','active',1,1)`} {
					if _, err := f.db.SQL().Exec(q); err != nil {
						t.Fatal(err)
					}
				}
			}
			fired := false
			hook := func() {
				if fired {
					return
				}
				fired = true
				query := map[string]string{"before_tenant": `UPDATE projects SET workspace_id='search-foreign' WHERE id=?`, "after_grant": `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, "after_credential": `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`, "after_named_workspace": `UPDATE project_workspaces SET status='archived' WHERE id=?`, "after_raw": `UPDATE artifacts SET status='archived' WHERE id=?`}[scenario]
				var args []any
				switch scenario {
				case "before_tenant":
					args = []any{f.projectID}
				case "after_grant":
					args = []any{f.assetID}
				case "after_named_workspace":
					args = []any{f.workspaceID}
				case "after_raw":
					args = []any{f.artifactID}
				}
				if _, err := f.db.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			wrapped := &workspaceTextSearchHook{Service: f.projects}
			if scenario == "before_tenant" {
				wrapped.before = hook
			} else {
				wrapped.after = hook
			}
			f.server.projects = wrapped
			w := workspaceTextSearchHTTP(t, f, "PRIVATE_LIBRARY_TEXT", "", auth)
			if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") || strings.Contains(w.Body.String(), f.assetID) {
				t.Fatalf("changed comparable collection disclosed fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
			}
		})
	}
}

func TestWorkspaceLibraryTextSearchHTTPCursorCannotRetainRevokedGrant(t *testing.T) {
	f, auth, _ := workspaceTextSearchPagedFixture(t, "bearer")
	query := "WORKSPACE_LITERAL_MATCH"
	first := decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, query, "", auth))
	if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID); err != nil {
		t.Fatal(err)
	}
	w := workspaceTextSearchHTTP(t, f, query, first.NextCursor, auth)
	if strings.Contains(w.Body.String(), f.assetID) || strings.Contains(w.Body.String(), "<script>") {
		t.Fatalf("retained navigation disclosed revoked source status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Code == 200 {
		result := decodeWorkspaceTextSearch(t, w)
		if len(result.Matches) != 0 {
			t.Fatalf("cursor retained revoked content grant %+v", result)
		}
	}
}

func TestWorkspaceLibraryTextSearchHTTPExpiredNavigationAndInvalidQueries(t *testing.T) {
	f, auth, _ := workspaceTextSearchPagedFixture(t, "bearer")
	now := time.Now()
	f.server.libraryTextSearchCursors.now = func() time.Time { return now }
	first := decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, "WORKSPACE_LITERAL_MATCH", "", auth))
	now = now.Add(5 * time.Minute)
	w := workspaceTextSearchHTTP(t, f, "WORKSPACE_LITERAL_MATCH", first.NextCursor, auth)
	if w.Code != 409 || strings.Contains(w.Body.String(), f.assetID) {
		t.Fatalf("expired navigation retained sources %d %s", w.Code, w.Body.String())
	}
	decodeWorkspaceTextSearch(t, workspaceTextSearchHTTP(t, f, "WORKSPACE_LITERAL_MATCH", "", auth))
	for _, q := range []string{"", " ", strings.Repeat("😀", 65), "\x00", string([]byte{0xff})} {
		w := workspaceTextSearchHTTP(t, f, q, "", auth)
		if w.Code != 400 {
			t.Fatalf("invalid literal %q accepted: %d %s", q, w.Code, w.Body.String())
		}
	}
}

func TestWorkspaceLibraryTextSearchHTTPSameHashSourceRebindingDenied(t *testing.T) {
	var store *inspectedLibraryStore
	f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT original source"), "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
		store = &inspectedLibraryStore{BlobStore: base}
		return store
	})
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	actor := "human"
	replacement, err := f.artifacts.Create(context.Background(), artifact.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, MediaType: "text/plain", CreatedBy: &actor, Label: policy.DataLabel{WorkspaceID: "tenant", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUntrustedContent}}, bytes.NewReader(f.body))
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	store.after = func(stage string) {
		if fired || stage != "open" {
			return
		}
		fired = true
		if _, err := f.db.SQL().Exec(`UPDATE project_library_asset_versions SET storage_uri=? WHERE asset_id=? AND version=1`, "artifact:"+replacement.ID, f.assetID); err != nil {
			t.Fatal(err)
		}
	}
	w := workspaceTextSearchHTTP(t, f, "PRIVATE_LIBRARY_TEXT", "", auth)
	if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") || strings.Contains(w.Body.String(), f.assetID) {
		t.Fatalf("same-hash source rebound after verification: %d %s", w.Code, w.Body.String())
	}
}

type inspectedVerificationAttempts struct {
	artifact.BlobStore
	calls int
	reads int
}

func (s *inspectedVerificationAttempts) Verify(ctx context.Context, ref, hash string, size int64) error {
	s.calls++
	return s.BlobStore.Verify(ctx, ref, hash, size)
}

type inspectedBudgetReadCloser struct {
	io.ReadCloser
	read *int
}

func (r *inspectedBudgetReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	*r.read += n
	return n, err
}
func (s *inspectedVerificationAttempts) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	r, err := s.BlobStore.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &inspectedBudgetReadCloser{ReadCloser: r, read: &s.reads}, nil
}

func TestLibraryTextIndexHTTPNonReadyMetadataDoesNotReadBinaryContent(t *testing.T) {
	for _, scenario := range []string{"unsupported", "too_large"} {
		t.Run(scenario, func(t *testing.T) {
			var store *inspectedVerificationAttempts
			body, media := []byte("binary non-text input"), "application/pdf"
			if scenario == "too_large" {
				body = bytes.Repeat([]byte("x"), projectworkspace.LibraryTextIndexLimit+1)
				media = "text/plain"
			}
			f := textLibraryHTTPFixture(t, body, media, func(base artifact.BlobStore) artifact.BlobStore {
				store = &inspectedVerificationAttempts{BlobStore: base}
				return store
			})
			auth := textLibraryHTTPAuth(t, f, "bearer")
			store.calls = 0
			store.reads = 0
			indexed := decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
			if indexed.Status != scenario {
				t.Fatalf("nonready input labelled ready %+v", indexed)
			}
			decodeTextIndex(t, textLibraryHTTP(t, f, "GET", "index", auth))
			if w := textLibraryHTTP(t, f, "GET", "search?q=private", auth); w.Code != 409 {
				t.Fatalf("nonready search status%d", w.Code)
			}
			if store.calls != 0 || store.reads != 0 {
				t.Fatalf("nonready metadata read binary bytes verify=%d bytes=%d", store.calls, store.reads)
			}
		})
	}
}
func TestWorkspaceLibraryTextSearchHTTPChangedSizeCannotEnlargeVerificationBudget(t *testing.T) {
	var store *inspectedVerificationAttempts
	f := textLibraryHTTPFixture(t, []byte("PRIVATE_LIBRARY_TEXT bounded source"), "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
		store = &inspectedVerificationAttempts{BlobStore: base}
		return store
	})
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	store.calls = 0
	fired := false
	f.server.projects = &workspaceTextSearchHook{Service: f.projects, after: func() {
		if fired {
			return
		}
		fired = true
		if _, err := f.db.SQL().Exec(`UPDATE artifacts SET size_bytes=67108864 WHERE id=?`, f.artifactID); err != nil {
			t.Fatal(err)
		}
	}}
	w := workspaceTextSearchHTTP(t, f, "PRIVATE_LIBRARY_TEXT", "", auth)
	if !fired || w.Code == 200 || store.calls != 0 || strings.Contains(w.Body.String(), "PRIVATE_LIBRARY_TEXT") {
		t.Fatalf("changed metadata enlarged verification work called=%d status=%d body=%s", store.calls, w.Code, w.Body.String())
	}
}
