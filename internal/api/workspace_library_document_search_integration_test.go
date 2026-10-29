//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func workspaceDocumentSearchHTTP(t *testing.T, f *publishedLibraryFixture, query, cursor string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	path := "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/search-documents?q=" + url.QueryEscape(query)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	r := httptest.NewRequest("GET", path, nil)
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

type decodedDocumentSearch struct {
	Query      string
	Matches    []projectworkspace.LibraryWorkspaceDocumentMatch
	HasMore    bool
	NextCursor string
}

func decodeWorkspaceDocumentSearch(t *testing.T, w *httptest.ResponseRecorder) decodedDocumentSearch {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("document discovery %d: %s", w.Code, w.Body.String())
	}
	assertDocumentPrivate(t, w)
	for _, field := range []string{"Candidates", "NextPosition", "Scanned", "next_position", "scanned_", "storage_uri"} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("private discovery verification/navigation exposed", field, w.Body.String())
		}
	}
	var result workspaceIndexedDocumentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Matches) > 8 || len(result.Sources) > 2 {
		t.Fatal(result, err)
	}
	out := decodedDocumentSearch{Query: result.Query, HasMore: result.HasMore, NextCursor: result.NextCursor}
	for _, match := range result.Matches {
		if match.Source < 0 || match.Source >= len(result.Sources) {
			t.Fatal("invalid document source reference", result)
		}
		out.Matches = append(out.Matches, projectworkspace.LibraryWorkspaceDocumentMatch{Index: result.Sources[match.Source], Unit: match.Unit})
	}
	return out
}

func TestLibraryDocumentSearchHTTPRealAuthAndOpaqueParagraphContinuation(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, method)
			if before := decodeWorkspaceDocumentSearch(t, workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)); len(before.Matches) != 0 || before.HasMore {
				t.Fatal("discovery implicitly indexed source", before)
			}
			index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			first := decodeWorkspaceDocumentSearch(t, workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth))
			if len(first.Matches) != 8 || !first.HasMore || len(first.NextCursor) != 43 {
				t.Fatal("missing bounded opaque first page", first)
			}
			second := decodeWorkspaceDocumentSearch(t, workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth))
			if len(second.Matches) != 2 || second.HasMore || second.NextCursor != "" {
				t.Fatal("lost paragraph continuation", second)
			}
			for i, match := range append(first.Matches, second.Matches...) {
				if match.Index.AssetID != f.assetID || match.Index.RawSourceSHA != f.contentHash || match.Index.DerivedSHA != index.DerivedSHA || match.Unit.Ordinal != i+1 || match.Unit.ParagraphNumber != i+1 || match.Unit.PartName != "word/document.xml" {
					t.Fatal("source/derived/paragraph discovery identity changed", match)
				}
			}
			if w := workspaceDocumentSearchHTTP(t, f, "different phrase", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("document cursor transferred to another query", w.Code)
			}
			if w := workspaceTextSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("document cursor transferred to raw-line search", w.Code)
			}
			f.server.libraryDocumentSearchCursors.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
			if w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", first.NextCursor, auth); w.Code != 409 {
				t.Fatal("document cursor did not expire", w.Code)
			}
			if err := f.projects.RevokeLibraryAsset(context.Background(), f.projectID, f.assetID, f.workspaceID, "human"); err != nil {
				t.Fatal(err)
			}
			if result := decodeWorkspaceDocumentSearch(t, workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)); len(result.Matches) != 0 {
				t.Fatal("discovery laundered revoked source access", result)
			}
		})
	}
}

type documentSearchCollectionHook struct {
	*projectworkspace.Service
	calls int
	after func(int)
}

func (h *documentSearchCollectionHook) SearchWorkspaceIndexedLibraryDocuments(ctx context.Context, c projectworkspace.LibraryTextSelection, query string, position projectworkspace.LibraryDocumentSearchPosition) (projectworkspace.LibraryWorkspaceDocumentSearch, error) {
	page, err := h.Service.SearchWorkspaceIndexedLibraryDocuments(ctx, c, query, position)
	if err == nil {
		h.calls++
		h.after(h.calls)
	}
	return page, err
}

func TestLibraryDocumentSearchHTTPCollectionAndPublicationDrift(t *testing.T) {
	for _, stage := range []int{1, 2} {
		for _, scenario := range []string{"credential", "grant", "project", "workspace", "source_uri"} {
			t.Run(strconv.Itoa(stage)+"/"+scenario, func(t *testing.T) {
				f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
				auth := textLibraryHTTPAuth(t, f, "bearer")
				decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
				hook := &documentSearchCollectionHook{Service: f.projects}
				hook.after = func(call int) {
					if call != stage {
						return
					}
					query := map[string]string{
						"credential": `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`,
						"grant":      `UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`,
						"project":    `UPDATE projects SET status='archived' WHERE id=?`,
						"workspace":  `UPDATE project_workspaces SET status='archived' WHERE id=?`,
						"source_uri": `UPDATE project_library_asset_versions SET storage_uri='artifact:rebound-with-same-hash' WHERE asset_id=?`,
					}[scenario]
					var args []any
					switch scenario {
					case "grant", "source_uri":
						args = []any{f.assetID}
					case "project":
						args = []any{f.projectID}
					case "workspace":
						args = []any{f.workspaceID}
					}
					if _, err := f.db.SQL().Exec(query, args...); err != nil {
						t.Fatal(err)
					}
				}
				f.server.projects = hook
				w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
				if w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") || strings.Contains(w.Body.String(), f.contentHash) {
					t.Fatal("stale discovery source/scope published", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestLibraryDocumentSearchHTTPReparsesEvenAuthorizedNonmatches(t *testing.T) {
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	first := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text", auth))
	second := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=8", auth))
	units := append(first.Units, second.Units...)
	var canonical bytes.Buffer
	size := 0
	for i := range units {
		units[i].Text = "forged text omitting every original matching phrase"
		hash := sha256.Sum256([]byte(units[i].Text))
		units[i].TextHash = "sha256:" + hex.EncodeToString(hash[:])
		canonical.WriteString(units[i].PartName)
		canonical.WriteByte(0)
		canonical.WriteString(strconv.Itoa(units[i].ParagraphNumber))
		canonical.WriteByte(0)
		canonical.WriteString(units[i].Text)
		canonical.WriteByte(0)
		size += len(units[i].Text)
	}
	hash := sha256.Sum256(canonical.Bytes())
	index.DerivedSHA, index.DerivedUTF8Bytes, index.CreatedAt = "sha256:"+hex.EncodeToString(hash[:]), size, 0
	manifest, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(manifest)
	for _, query := range []string{`DROP TRIGGER library_document_unit_immutable`, `DROP TRIGGER library_document_index_immutable`} {
		if _, err = f.db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, unit := range units {
		if _, err = f.db.SQL().Exec(`UPDATE project_library_document_units SET content=?,text_hash=? WHERE asset_id=? AND ordinal=?`, unit.Text, unit.TextHash, f.assetID, unit.Ordinal); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.db.SQL().Exec(`UPDATE project_library_document_indexes SET derived_sha=?,derived_utf8_bytes=?,manifest_sha=? WHERE asset_id=?`, index.DerivedSHA, size, "sha256:"+hex.EncodeToString(hash[:]), f.assetID); err != nil {
		t.Fatal(err)
	}
	w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
	if w.Code != 409 {
		t.Fatal("self-consistent forged nonmatches skipped original-byte verification", w.Code, w.Body.String())
	}
}

func TestLibraryDocumentSearchHTTPEmptyCollectionCredentialFences(t *testing.T) {
	for _, stage := range []int{1, 2} {
		t.Run(strconv.Itoa(stage), func(t *testing.T) {
			f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			hook := &documentSearchCollectionHook{Service: f.projects}
			hook.after = func(call int) {
				if call == stage {
					if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.server.projects = hook
			w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
			if w.Code != 401 || hook.calls != stage || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
				t.Fatal("empty discovery bypassed original credential fence", stage, hook.calls, w.Code, w.Body.String())
			}
		})
	}
}

func TestLibraryDocumentSearchHTTPBoundedNonmatchWorkAndMetadataOnlyInputs(t *testing.T) {
	var store *inspectedVerificationAttempts
	body := documentHTTPDOCX(t)
	f := textLibraryHTTPFixture(t, body, projectworkspace.LibraryDocumentMIME, func(base artifact.BlobStore) artifact.BlobStore {
		store = &inspectedVerificationAttempts{BlobStore: base}
		return store
	})
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	for i := 0; i < 2; i++ {
		actor := "human"
		raw, err := f.artifacts.Create(context.Background(), artifact.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, MediaType: projectworkspace.LibraryDocumentMIME, CreatedBy: &actor, Label: policy.DataLabel{WorkspaceID: "tenant", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUntrustedContent}}, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		asset, err := f.projects.ImportLibraryAsset(context.Background(), projectworkspace.ImportLibraryAssetCommand{ProjectID: f.projectID, SourceWorkspaceID: f.workspaceID, Name: "Another actual DOCX", MIMEType: projectworkspace.LibraryDocumentMIME, ArtifactID: raw.ID, ContentHash: raw.ContentHash, SizeBytes: raw.SizeBytes, ActorPrincipalID: actor})
		if err != nil {
			t.Fatal(err)
		}
		f.assetID = asset.ID
		decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	}
	store.calls, store.reads = 0, 0
	w := workspaceDocumentSearchHTTP(t, f, "phrase absent from all documents", "", auth)
	page := decodeWorkspaceDocumentSearch(t, w)
	if len(page.Matches) != 0 || !page.HasMore || store.calls != 2 || store.reads != 2*len(body) {
		t.Fatal("nonmatching verification omitted or exceeded two-source page budget", page, store.calls, store.reads)
	}
	if strings.Contains(w.Body.String(), f.assetID) || !strings.Contains(w.Body.String(), `"sources":[]`) {
		t.Fatal("nonmatching source identities leaked", w.Body.String())
	}
	for _, media := range []string{"application/pdf", projectworkspace.LibraryDocumentMIME} {
		input := []byte("unsupported binary source")
		if media == projectworkspace.LibraryDocumentMIME {
			input = bytes.Repeat([]byte("x"), projectworkspace.LibraryDocumentIndexLimit+1)
		}
		f := textLibraryHTTPFixture(t, input, media, func(base artifact.BlobStore) artifact.BlobStore {
			store = &inspectedVerificationAttempts{BlobStore: base}
			return store
		})
		auth := textLibraryHTTPAuth(t, f, "bearer")
		decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
		store.calls, store.reads = 0, 0
		page := decodeWorkspaceDocumentSearch(t, workspaceDocumentSearchHTTP(t, f, "binary", "", auth))
		if len(page.Matches) != 0 || store.calls != 0 || store.reads != 0 {
			t.Fatal("metadata-only discovery performed binary I/O", store.calls, store.reads, page)
		}
	}
}

type changingDocumentReadStore struct {
	artifact.BlobStore
	changed   bool
	readBytes int
	closed    bool
}

func (s *changingDocumentReadStore) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	if s.changed {
		return &unendingDocumentSource{store: s}, nil
	}
	return s.BlobStore.Open(ctx, ref)
}

type unendingDocumentSource struct{ store *changingDocumentReadStore }

func (r *unendingDocumentSource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.store.readBytes += len(p)
	return len(p), nil
}
func (r *unendingDocumentSource) Close() error { r.store.closed = true; return nil }

func TestLibraryDocumentSearchHTTPReadBoundUsesSelectedSourcePlusSentinel(t *testing.T) {
	var store *changingDocumentReadStore
	body := documentHTTPDOCX(t)
	f := textLibraryHTTPFixture(t, body, projectworkspace.LibraryDocumentMIME, func(base artifact.BlobStore) artifact.BlobStore {
		store = &changingDocumentReadStore{BlobStore: base}
		return store
	})
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	// Verification still reads the unchanged real source through the embedded
	// store. Only the subsequent Open changes into a stream with no EOF.
	store.changed = true
	w := workspaceDocumentSearchHTTP(t, f, "PRIVATE_DOCUMENT_TEXT", "", auth)
	if w.Code != 409 || store.readBytes != len(body)+1 || !store.closed || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
		t.Fatal("post-verification growth escaped selected source budget", w.Code, store.readBytes, len(body), store.closed, w.Body.String())
	}
}
