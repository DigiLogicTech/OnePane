//go:build integration

package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// The fixture is a valid, in-memory OPC package. It requires no Word installation.
func documentHTTPDOCX(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	var paragraphs strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&paragraphs, `<w:p><w:r><w:t>PRIVATE_DOCUMENT_TEXT paragraph %d café %% _ SELECT</w:t></w:r></w:p>`, i)
	}
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + paragraphs.String() + `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Table literal &lt;script&gt;</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:sectPr/></w:body></w:document>`},
	}
	for _, p := range parts {
		w, err := z.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(p.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func assertDocumentPrivate(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing private document headers: %v", w.Header())
	}
	for _, secret := range []string{"storage_uri", "storage_ref", "artifact:", "byte_start", "byte_end", "page_number"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("private or unsupported citation exposed %q: %s", secret, w.Body.String())
		}
	}
}
func decodeDocumentIndex(t *testing.T, w *httptest.ResponseRecorder) projectworkspace.LibraryDocumentIndex {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("document index status=%d body=%s", w.Code, w.Body.String())
	}
	assertDocumentPrivate(t, w)
	var index projectworkspace.LibraryDocumentIndex
	if err := json.Unmarshal(w.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	return index
}
func decodeDocumentPage(t *testing.T, w *httptest.ResponseRecorder) projectworkspace.LibraryDocumentPage {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("document text status=%d body=%s", w.Code, w.Body.String())
	}
	assertDocumentPrivate(t, w)
	var page projectworkspace.LibraryDocumentPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Units) > 8 {
		t.Fatal("unbounded document units")
	}
	return page
}

func TestLibraryDocumentHTTPRealAuthPersistedCitationFlow(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			raw := documentHTTPDOCX(t)
			f := textLibraryHTTPFixture(t, raw, projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, method)
			var assetsBefore, artifactsBefore, grantsBefore int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_assets),(SELECT COUNT(*) FROM artifacts),(SELECT COUNT(*) FROM workspace_library_grants)`).Scan(&assetsBefore, &artifactsBefore, &grantsBefore); err != nil {
				t.Fatal(err)
			}
			before := decodeDocumentIndex(t, textLibraryHTTP(t, f, "GET", "document-index", auth))
			if before.Status != "not_indexed" || before.UnitCount != 0 {
				t.Fatalf("invented document content %+v", before)
			}
			if w := textLibraryHTTP(t, f, "GET", "document-text?after=0", auth); w.Code != 409 {
				t.Fatalf("unindexed read status=%d body=%s", w.Code, w.Body.String())
			}
			index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			if index.Status != "ready" || index.UnitCount != 11 || index.AssetID != f.assetID || index.Version != 1 || index.RawSourceSHA != f.contentHash || index.RawSourceSize != int64(len(raw)) || index.DerivedSHA == index.RawSourceSHA || index.DerivedSHA == "" || index.DerivedUTF8Bytes <= 0 || index.ParserID != "docx-wordml-paragraphs-v2" || index.Coverage == "" {
				t.Fatalf("wrong raw/derived document identity %+v", index)
			}
			retry := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			if !reflect.DeepEqual(index, retry) {
				t.Fatalf("retry changed index %+v %+v", index, retry)
			}
			f.projects = projectworkspace.NewService(f.db.SQL(), f.db, clock.Real{})
			f.server.projects = f.projects
			first := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=0", auth))
			if !reflect.DeepEqual(first.Index, index) || len(first.Units) != 8 || !first.HasMore || first.NextAfter != 8 {
				t.Fatalf("wrong first page %+v", first)
			}
			second := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=8", auth))
			if len(second.Units) != 3 || second.HasMore || second.NextAfter != 11 {
				t.Fatalf("wrong second page %+v", second)
			}
			totalBytes := 0
			for i, u := range append(first.Units, second.Units...) {
				if u.Ordinal != i+1 || u.PartName != "word/document.xml" || u.ParagraphNumber != i+1 {
					t.Fatalf("incorrect part coordinate %+v", u)
				}
				hash := sha256.Sum256([]byte(u.Text))
				if u.TextHash != "sha256:"+hex.EncodeToString(hash[:]) {
					t.Fatalf("incorrect paragraph hash %+v", u)
				}
				totalBytes += len(u.Text)
			}
			if totalBytes != index.DerivedUTF8Bytes {
				t.Fatalf("derived text bytes mismatch %d != %d", totalBytes, index.DerivedUTF8Bytes)
			}
			if second.Units[2].Text != "Table literal <script>" {
				t.Fatalf("table paragraph lost %+v", second.Units[2])
			}
			for _, q := range []string{"PRIVATE_DOCUMENT_TEXT", "%", "_", "SELECT", "café"} {
				page := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=0&q="+url.QueryEscape(q), auth))
				if page.Query != q || len(page.Units) != 8 || !page.HasMore {
					t.Fatalf("literal search %q incorrect %+v", q, page)
				}
			}
			if page := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=11", auth)); len(page.Units) != 0 || page.HasMore || page.NextAfter != 11 {
				t.Fatalf("terminal page %+v", page)
			}
			var indexes, units, audits int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?),(SELECT COUNT(*) FROM project_library_document_units WHERE asset_id=?),(SELECT COUNT(*) FROM events WHERE event_type='project.library_document_indexed' AND aggregate_id=?)`, f.assetID, f.assetID, f.assetID).Scan(&indexes, &units, &audits); err != nil || indexes != 1 || units != 11 || audits != 1 {
				t.Fatalf("atomic retry counts %d/%d/%d %v", indexes, units, audits, err)
			}
			var assetsAfter, artifactsAfter, grantsAfter int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_assets),(SELECT COUNT(*) FROM artifacts),(SELECT COUNT(*) FROM workspace_library_grants)`).Scan(&assetsAfter, &artifactsAfter, &grantsAfter); err != nil {
				t.Fatal(err)
			}
			if assetsAfter != assetsBefore || artifactsAfter != artifactsBefore || grantsAfter != grantsBefore {
				t.Fatal("document extraction created an asset, artifact or grant")
			}
			reader, original, err := f.artifacts.Open(context.Background(), f.artifactID)
			if err != nil {
				t.Fatal(err)
			}
			retained, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(retained, raw) || original.ContentHash != index.RawSourceSHA || original.SizeBytes != index.RawSourceSize {
				t.Fatalf("original managed DOCX changed: %v", err)
			}
		})
	}
}

func TestLibraryDocumentHTTPDeclaredStoriesKeepPartCoordinates(t *testing.T) {
	raw := documentHTTPDOCX(t)
	source, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, entry := range source.File {
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "[Content_Types].xml" {
			body = []byte(strings.Replace(string(body), "</Types>", `<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/></Types>`, 1))
		}
		if entry.Name == "word/document.xml" {
			body = []byte(strings.Replace(string(body), "<w:sectPr/>", `<w:sectPr xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:headerReference w:type="default" r:id="rHeader"/></w:sectPr>`, 1))
		}
		w, err := z.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	for _, part := range []struct{ name, content string }{
		{"word/_rels/document.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rHeader" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/><Relationship Id="rFootnotes" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes" Target="footnotes.xml"/></Relationships>`},
		{"word/header1.xml", `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Header stored text</w:t></w:r></w:p></w:hdr>`},
		{"word/footnotes.xml", `<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:footnote w:id="1"><w:p><w:r><w:t>Footnote stored text</w:t></w:r></w:p></w:footnote></w:footnotes>`},
	} {
		w, err := z.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(part.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	f := textLibraryHTTPFixture(t, out.Bytes(), projectworkspace.LibraryDocumentMIME, nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	if index.Status != "ready" || index.UnitCount != 13 || len(index.Parts) != 3 || index.Coverage != projectworkspace.LibraryDocumentCoverage {
		t.Fatalf("story coverage receipt incorrect %+v", index)
	}
	page := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=11", auth))
	if len(page.Units) != 2 || page.Units[0].PartName != "word/footnotes.xml" || page.Units[1].PartName != "word/header1.xml" || page.Units[0].Ordinal != 12 || page.Units[1].Ordinal != 13 || page.Units[0].ParagraphNumber != 1 || page.Units[1].ParagraphNumber != 1 {
		t.Fatalf("story coordinates incorrect %+v", page)
	}
}

func TestLibraryDocumentIndexHTTPRevocationDuringManagedSourceCollection(t *testing.T) {
	for _, stage := range []string{"verify", "open", "read"} {
		for _, scenario := range []string{"credential", "grant", "project", "workspace", "membership", "write_scope", "project_tenant", "artifact_archived", "artifact_quarantined", "artifact_hash", "artifact_size", "artifact_storage"} {
			t.Run(stage+"/"+scenario, func(t *testing.T) {
				var store *inspectedLibraryStore
				f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, func(base artifact.BlobStore) artifact.BlobStore {
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
					case "artifact_size":
						query = `UPDATE artifacts SET size_bytes=size_bytes+1 WHERE id=?`
						args = []any{f.artifactID}
					}
					if _, err := f.db.SQL().Exec(query, args...); err != nil {
						t.Fatal(err)
					}
				}
				w := textLibraryHTTP(t, f, "POST", "document-index", auth)
				if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
					t.Fatalf("changed source authority accepted fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
				}
				var count int
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("revoked source indexed count=%d err=%v", count, err)
				}
			})
		}
	}
}

type documentLibraryTransactionHook struct {
	*projectworkspace.Service
	inTransaction func(context.Context, storage.Tx) error
	after         func()
}

func (s *documentLibraryTransactionHook) IndexWorkspaceLibraryDocument(ctx context.Context, c projectworkspace.IndexLibraryDocumentCommand) (projectworkspace.LibraryDocumentIndex, error) {
	if s.inTransaction != nil {
		original := c.Selection.Authorize
		c.Selection.Authorize = func(ctx context.Context, tx storage.Tx) error {
			if err := s.inTransaction(ctx, tx); err != nil {
				return err
			}
			return original(ctx, tx)
		}
	}
	result, err := s.Service.IndexWorkspaceLibraryDocument(ctx, c)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}
func (s *documentLibraryTransactionHook) WorkspaceLibraryDocumentIndex(ctx context.Context, selection projectworkspace.LibraryTextSelection) (projectworkspace.LibraryDocumentIndex, error) {
	result, err := s.Service.WorkspaceLibraryDocumentIndex(ctx, selection)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}
func (s *documentLibraryTransactionHook) ReadWorkspaceLibraryDocument(ctx context.Context, selection projectworkspace.LibraryTextSelection, q string, after int) (projectworkspace.LibraryDocumentPage, error) {
	result, err := s.Service.ReadWorkspaceLibraryDocument(ctx, selection, q, after)
	if err == nil && s.after != nil {
		s.after()
	}
	return result, err
}

func TestLibraryDocumentIndexHTTPSameTransactionCredentialAndFinalPublicationFences(t *testing.T) {
	for _, scenario := range []string{"revoked", "changed_scope", "changed_token", "changed_identity"} {
		t.Run("transaction/"+scenario, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			query := map[string]string{
				"revoked":          `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`,
				"changed_scope":    `UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='text-api'`,
				"changed_token":    `UPDATE api_credentials SET credential_hash='replaced' WHERE id='text-api'`,
				"changed_identity": `UPDATE principals SET principal_type='agent' WHERE id='human'`,
			}[scenario]
			called := false
			f.server.projects = &documentLibraryTransactionHook{Service: f.projects, inTransaction: func(ctx context.Context, tx storage.Tx) error {
				called = true
				_, err := tx.ExecContext(ctx, query)
				return err
			}}
			w := textLibraryHTTP(t, f, "POST", "document-index", auth)
			if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
				t.Fatalf("same transaction credential bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
			var count int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed verifier persisted index count=%d %v", count, err)
			}
		})
	}
	for _, operation := range []string{"index", "status", "search"} {
		t.Run("final/"+operation, func(t *testing.T) {
			f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
			auth := textLibraryHTTPAuth(t, f, "bearer")
			if operation != "index" {
				decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			}
			called := false
			f.server.projects = &documentLibraryTransactionHook{Service: f.projects, after: func() {
				called = true
				if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`); err != nil {
					t.Fatal(err)
				}
			}}
			method, tail := "GET", "document-index"
			if operation == "index" {
				method = "POST"
			}
			if operation == "search" {
				tail = "document-text?q=PRIVATE_DOCUMENT_TEXT"
			}
			w := textLibraryHTTP(t, f, method, tail, auth)
			if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
				t.Fatalf("final credential bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
			var count int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("successful index disappeared count=%d %v", count, err)
			}
		})
	}
}

func TestLibraryDocumentIndexHTTPWithholdsAfterSourceMetadataChanges(t *testing.T) {
	for _, operation := range []string{"index", "status", "search"} {
		for _, scenario := range []string{"artifact_archived", "artifact_hash", "artifact_storage", "project_tenant", "grant"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
				auth := textLibraryHTTPAuth(t, f, "bearer")
				if operation != "index" {
					decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
				}
				if scenario == "project_tenant" {
					for _, query := range []string{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('text-foreign','Foreign','active',1,1,1)`, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('text-foreign','human','active',1,1)`} {
						if _, err := f.db.SQL().Exec(query); err != nil {
							t.Fatal(err)
						}
					}
				}
				called := false
				f.server.projects = &documentLibraryTransactionHook{Service: f.projects, after: func() {
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
				method, tail := "GET", "document-index"
				if operation == "index" {
					method = "POST"
				}
				if operation == "search" {
					tail = "document-text?q=PRIVATE_DOCUMENT_TEXT"
				}
				w := textLibraryHTTP(t, f, method, tail, auth)
				if !called || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
					t.Fatalf("final source metadata bypass called=%v status=%d body=%s", called, w.Code, w.Body.String())
				}
				var count int
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("committed index changed count=%d %v", count, err)
				}
			})
		}
	}
}

func TestLibraryDocumentIndexHTTPRejectsMissingVerifierClientBytesAndBrowserCSRF(t *testing.T) {
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
	if w := textLibraryHTTP(t, f, "POST", "document-index", func(*http.Request) {}); w.Code != 503 {
		t.Fatalf("missing transaction verifier accepted status=%d", w.Code)
	}
	auth := textLibraryHTTPAuth(t, f, "session")
	r := httptest.NewRequest("POST", textLibraryPath(f, "document-index"), nil)
	auth(r)
	r.Header.Del("X-OnePane-CSRF")
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("missing browser CSRF accepted status=%d body=%s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", textLibraryPath(f, "document-index"), strings.NewReader(`{"content":"replacement","path":"C:/private"}`))
	auth(r)
	w = httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("client replacement bytes accepted status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_document_indexes WHERE asset_id=?`, f.assetID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid request persisted index count=%d %v", count, err)
	}
}

func TestLibraryDocumentHTTPTruthfulStatusesAvoidBinaryReads(t *testing.T) {
	for _, tc := range []struct {
		name, media, status string
		body                []byte
		metadataOnly        bool
	}{
		{"pdf", "application/pdf", "unsupported", []byte("%PDF private unsupported content"), true},
		{"oversized", projectworkspace.LibraryDocumentMIME, "too_large", bytes.Repeat([]byte("x"), (8<<20)+1), true},
		{"invalid_zip", projectworkspace.LibraryDocumentMIME, "invalid_document", []byte("not a ZIP archive"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var store *inspectedLibraryStore
			f := textLibraryHTTPFixture(t, tc.body, tc.media, func(base artifact.BlobStore) artifact.BlobStore {
				store = &inspectedLibraryStore{BlobStore: base}
				return store
			})
			auth := textLibraryHTTPAuth(t, f, "bearer")
			reads := 0
			verifies := 0
			store.after = func(stage string) {
				if stage == "read" {
					reads++
				}
				if stage == "verify" {
					verifies++
				}
			}
			index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			if index.Status != tc.status || index.UnitCount != 0 || index.DerivedSHA != "" || index.DerivedUTF8Bytes != 0 || index.Coverage != "" || len(index.Parts) != 0 {
				t.Fatalf("untruthful nonready receipt %+v", index)
			}
			if tc.metadataOnly && (reads != 0 || verifies != 0) {
				t.Fatalf("metadata-only status read binary: reads=%d verifies=%d", reads, verifies)
			}
			if w := textLibraryHTTP(t, f, "GET", "document-text?after=0", auth); w.Code != 409 {
				t.Fatalf("nonready read status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLibraryDocumentHTTPRejectsInvalidReadInputsAndRequiresSourceGrant(t *testing.T) {
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	for _, tail := range []string{"document-text?after=-1", "document-text?after=12", "document-text?after=8193", "document-text?after=1.5", "document-text?after=overflow", "document-text?q=" + strings.Repeat("a", 257), "document-text?q=%00", "document-text?q=%FF", "document-text?q=%20"} {
		if w := textLibraryHTTP(t, f, "GET", tail, auth); w.Code != 400 {
			t.Fatalf("invalid input accepted %q status=%d body=%s", tail, w.Code, w.Body.String())
		}
	}
	if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='text-api'`); err != nil {
		t.Fatal(err)
	}
	decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text", auth))
	if w := textLibraryHTTP(t, f, "POST", "document-index", auth); w.Code != 403 {
		t.Fatalf("read-only credential wrote status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["project.write"]' WHERE id='text-api'`); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		if w := textLibraryHTTP(t, f, method, "document-index", auth); w.Code != 403 {
			t.Fatalf("write-only credential bypassed read requirement status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["project.read"]' WHERE id='text-api'`); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0", "-1", "not-a-version"} {
		r := httptest.NewRequest("GET", strings.Replace(textLibraryPath(f, "document-index"), "/versions/1/", "/versions/"+version+"/", 1), nil)
		auth(r)
		w := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid version %q status=%d body=%s", version, w.Code, w.Body.String())
		}
	}
	if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID); err != nil {
		t.Fatal(err)
	}
	for _, tail := range []string{"document-index", "document-text?q=PRIVATE_DOCUMENT_TEXT"} {
		w := textLibraryHTTP(t, f, "GET", tail, auth)
		if w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") {
			t.Fatalf("derived text laundered revoked source grant: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestLibraryDocumentHTTPReparsesSourceBeforePublishingSelfConsistentDerivedForgery(t *testing.T) {
	f := textLibraryHTTPFixture(t, documentHTTPDOCX(t), projectworkspace.LibraryDocumentMIME, nil)
	auth := textLibraryHTTPAuth(t, f, "bearer")
	index := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
	first := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text", auth))
	second := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?after=8", auth))
	units := append(first.Units, second.Units...)
	units[0].Text = "FORGED_DERIVED_DOCUMENT secret replacement"
	hash := sha256.Sum256([]byte(units[0].Text))
	units[0].TextHash = "sha256:" + hex.EncodeToString(hash[:])
	var canonical bytes.Buffer
	size := 0
	for _, unit := range units {
		canonical.WriteString(unit.PartName)
		canonical.WriteByte(0)
		canonical.WriteString(strconv.Itoa(unit.ParagraphNumber))
		canonical.WriteByte(0)
		canonical.WriteString(unit.Text)
		canonical.WriteByte(0)
		size += len(unit.Text)
	}
	hash = sha256.Sum256(canonical.Bytes())
	index.DerivedSHA = "sha256:" + hex.EncodeToString(hash[:])
	index.DerivedUTF8Bytes = size
	index.CreatedAt = 0
	manifest, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(manifest)
	manifestSHA := "sha256:" + hex.EncodeToString(hash[:])
	if _, err = f.db.SQL().Exec(`UPDATE project_library_document_units SET content=? WHERE asset_id=? AND ordinal=1`, units[0].Text, f.assetID); err == nil {
		t.Fatal("retained units were mutable")
	}
	// Simulate storage corruption beyond normal SQL writes, which immutability
	// triggers already deny. Recomputed local seals must not replace raw evidence.
	for _, statement := range []string{`DROP TRIGGER library_document_unit_immutable`, `DROP TRIGGER library_document_index_immutable`} {
		if _, err = f.db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.db.SQL().Exec(`UPDATE project_library_document_units SET content=?,text_hash=? WHERE asset_id=? AND ordinal=1`, units[0].Text, units[0].TextHash, f.assetID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.SQL().Exec(`UPDATE project_library_document_indexes SET derived_sha=?,derived_utf8_bytes=?,manifest_sha=? WHERE asset_id=?`, index.DerivedSHA, size, manifestSHA, f.assetID); err != nil {
		t.Fatal(err)
	}
	for _, tail := range []string{"document-index", "document-text?q=FORGED_DERIVED_DOCUMENT"} {
		w := textLibraryHTTP(t, f, "GET", tail, auth)
		if w.Code != 409 || strings.Contains(w.Body.String(), "FORGED_DERIVED_DOCUMENT") {
			t.Fatalf("self-consistent derived forgery published status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
