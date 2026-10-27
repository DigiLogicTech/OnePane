//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func contentSearchHTTP(t *testing.T, f *publishedLibraryFixture, query, cursor string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	path := "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/search-content?q=" + url.QueryEscape(query)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	r := httptest.NewRequest("GET", path, nil)
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func mixedContentSearchFixture(t *testing.T, method string, wrap func(artifact.BlobStore) artifact.BlobStore) (*publishedLibraryFixture, func(*http.Request), string) {
	t.Helper()
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, wrap)
	auth := textLibraryHTTPAuth(t, f, method)
	decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	textID := appendWorkspaceTextSearchAsset(t, f, []byte(strings.Repeat("PRIVATE_DOCUMENT_TEXT <script>raw source</script>\n", 10)))
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	return f, auth, textID
}

func decodeContentSearch(t *testing.T, w *httptest.ResponseRecorder) workspaceIndexedContentResponse {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("content discovery %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing private content headers")
	}
	for _, field := range []string{"storage_uri", "artifact:", "Candidates", "NextPosition", "Scanned", "next_position", "scanned_"} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("private source/navigation fields leaked", field)
		}
	}
	var out workspaceIndexedContentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.TextMatches) > 8 || len(out.WordMatches) > 8 || len(out.WordSources) > 2 || out.HasMore != (out.NextCursor != "") || len(w.Body.Bytes()) > 2<<20 {
		t.Fatal("unbounded content response")
	}
	for _, m := range out.WordMatches {
		if m.Source < 0 || m.Source >= len(out.WordSources) {
			t.Fatal("invalid Word source receipt")
		}
	}
	return out
}

func TestLibraryDocumentContentSearchRealAuthBothFormatsAndCompoundPaging(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f, auth, _ := mixedContentSearchFixture(t, method, nil)
			first := decodeContentSearch(t, contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth))
			if len(first.TextMatches) != 8 || len(first.WordMatches) != 8 || len(first.WordSources) != 1 || !first.HasMore || len(first.NextCursor) != 43 {
				t.Fatalf("both formats not actually searched: %+v", first)
			}
			for _, m := range first.TextMatches {
				if m.Line.LineNumber < 1 || m.Line.LineNumber > 8 || m.Index.ContentHash == "" {
					t.Fatal("raw line coordinates changed")
				}
			}
			for _, m := range first.WordMatches {
				if m.Unit.PartName != "word/document.xml" || m.Unit.ParagraphNumber < 1 || m.Unit.ParagraphNumber > 8 || first.WordSources[m.Source].RawSourceSHA == first.WordSources[m.Source].DerivedSHA {
					t.Fatal("Word/source provenance conflated")
				}
			}
			if w := contentSearchHTTP(t, f, "changed-query", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("compound cursor transferred to another query")
			}
			if w := workspaceTextSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("compound cursor transferred to raw-text registry")
			}
			if w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("compound cursor transferred to Word registry")
			}
			second := decodeContentSearch(t, contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth))
			if len(second.TextMatches) != 2 || len(second.WordMatches) != 2 || second.HasMore || second.NextCursor != "" {
				t.Fatalf("compound continuation lost or repeated matches: %+v", second)
			}
			if second.TextMatches[0].Line.LineNumber != 9 || second.WordMatches[0].Unit.ParagraphNumber != 9 {
				t.Fatal("format cursor restarted instead of resuming")
			}
			if strings.Contains(fmt.Sprintf("%+v", f.server.libraryContentSearchCursors.entries), "PRIVATE_DOCUMENT_TEXT") {
				t.Fatal("navigation registry retained query/excerpts")
			}
		})
	}
}

func TestLibraryDocumentContentSearchRevokesFirstFormatDuringSecondSourceRead(t *testing.T) {
	for _, scenario := range []string{"grant", "artifact_storage", "credential", "membership"} {
		t.Run(scenario, func(t *testing.T) {
			var store *inspectedLibraryStore
			f, auth, textID := mixedContentSearchFixture(t, "bearer", func(base artifact.BlobStore) artifact.BlobStore {
				store = &inspectedLibraryStore{BlobStore: base}
				return store
			})
			fired := false
			store.after = func(at string) {
				if at != "read" || fired {
					return
				}
				fired = true
				query := `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`
				args := []any{textID}
				switch scenario {
				case "artifact_storage":
					query = `UPDATE artifacts SET storage_ref='changed-private-storage' WHERE id=?`
					args = []any{f.artifactID}
				case "credential":
					query = `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`
					args = nil
				case "membership":
					query = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tenant' AND principal_id='human'`
					args = nil
				}
				if _, err := f.db.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			w := contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
			if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") || strings.Contains(w.Body.String(), "raw source") {
				t.Fatalf("stale first-format excerpt published after second-source I/O: fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
			}
		})
	}
}

func TestLibraryDocumentContentSearchExpiresNavigationAndRejectsInvalidInputs(t *testing.T) {
	f, auth, _ := mixedContentSearchFixture(t, "bearer", nil)
	now := time.Now()
	f.server.libraryContentSearchCursors.now = func() time.Time { return now }
	first := decodeContentSearch(t, contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth))
	now = now.Add(5 * time.Minute)
	if w := contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth); w.Code != 409 {
		t.Fatal("expired compound cursor retained navigation")
	}
	for _, q := range []string{"", " ", strings.Repeat("é", 129), "needle\x00"} {
		if w := contentSearchHTTP(t, f, q, "", auth); w.Code != 400 {
			t.Fatal("invalid query accepted", w.Code)
		}
	}
	if w := contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", strings.Repeat("x", 129), auth); w.Code != 400 {
		t.Fatal("oversized cursor accepted")
	}
}

func TestLibraryDocumentContentSearchDoesNotRestartFinishedFormat(t *testing.T) {
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	appendWorkspaceTextSearchAsset(t, f, []byte("PRIVATE_DOCUMENT_TEXT one raw line"))
	decodeTextIndex(t, textLibraryHTTP(t, f, "POST", "index", auth))
	first := decodeContentSearch(t, contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth))
	if len(first.TextMatches) != 1 || len(first.WordMatches) != 8 || !first.HasMore {
		t.Fatal("mixed first page incorrect")
	}
	second := decodeContentSearch(t, contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth))
	if len(second.TextMatches) != 0 || len(second.WordMatches) != 2 || second.HasMore {
		t.Fatal("completed raw-text scan restarted while Word continued")
	}
}

type contentSearchCollectionHook struct {
	*projectworkspace.Service
	calls int
	after func(int)
}

func (h *contentSearchCollectionHook) SearchWorkspaceIndexedLibraryText(ctx context.Context, c projectworkspace.LibraryTextSelection, q string, p projectworkspace.LibraryTextSearchPosition) (projectworkspace.LibraryWorkspaceTextSearch, error) {
	page, err := h.Service.SearchWorkspaceIndexedLibraryText(ctx, c, q, p)
	if err == nil {
		h.calls++
		h.after(h.calls)
	}
	return page, err
}
func (h *contentSearchCollectionHook) SearchWorkspaceIndexedLibraryDocuments(ctx context.Context, c projectworkspace.LibraryTextSelection, q string, p projectworkspace.LibraryDocumentSearchPosition) (projectworkspace.LibraryWorkspaceDocumentSearch, error) {
	page, err := h.Service.SearchWorkspaceIndexedLibraryDocuments(ctx, c, q, p)
	if err == nil {
		h.calls++
		h.after(h.calls)
	}
	return page, err
}
func TestLibraryDocumentContentSearchEmptyCollectionsKeepOriginalCredentialFences(t *testing.T) {
	for stage := 1; stage <= 4; stage++ {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			hook := &contentSearchCollectionHook{Service: f.projects}
			hook.after = func(call int) {
				if call == stage {
					if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.server.projects = hook
			w := contentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
			if w.Code != 401 || hook.calls != stage || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
				t.Fatalf("empty combined page bypassed original credential at collection %d: calls=%d status=%d", stage, hook.calls, w.Code)
			}
		})
	}
}
