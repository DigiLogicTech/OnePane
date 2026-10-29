//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Start at the same multipart upload boundary as Workspace's File/FormData
// control. Directly creating an artifact cannot prove that the upload preserves
// the MIME, version, source grant and managed-byte identity extraction requires.
func TestLibraryDocumentHTTPMultipartUploadToSourceBoundParagraphs(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f := publishedLibraryHTTPFixture(t, nil)
			auth := textLibraryHTTPAuth(t, f, method)
			raw := documentHTTPDOCX(t)
			upload := func(csrf bool) *httptest.ResponseRecorder {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				header := make(textproto.MIMEHeader)
				header.Set("Content-Disposition", `form-data; name="file"; filename="Evidence café.docx"`)
				header.Set("Content-Type", projectworkspace.LibraryDocumentMIME)
				part, err := writer.CreatePart(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = part.Write(raw); err != nil {
					t.Fatal(err)
				}
				if err = writer.Close(); err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodPost, "/v1/projects/"+f.projectID+"/library?workspace_id="+url.QueryEscape(f.workspaceID), &body)
				r.Header.Set("Content-Type", writer.FormDataContentType())
				auth(r)
				if !csrf {
					r.Header.Del("X-OnePane-CSRF")
				}
				w := httptest.NewRecorder()
				f.server.Handler().ServeHTTP(w, r)
				return w
			}
			if method == "session" {
				var before, after int
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&before); err != nil {
					t.Fatal(err)
				}
				if w := upload(false); w.Code != http.StatusForbidden {
					t.Fatalf("multipart upload without CSRF: %d %s", w.Code, w.Body.String())
				}
				if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&after); err != nil || after != before {
					t.Fatalf("denied upload created an artifact: %d -> %d: %v", before, after, err)
				}
			}
			w := upload(true)
			if w.Code != http.StatusCreated {
				t.Fatalf("multipart upload: %d %s", w.Code, w.Body.String())
			}
			var asset projectworkspace.LibraryAsset
			if err := json.Unmarshal(w.Body.Bytes(), &asset); err != nil || asset.ID == "" || asset.Name != "Evidence café.docx" || asset.CurrentVersion != 1 {
				t.Fatalf("uploaded Library identity: %+v %v", asset, err)
			}
			if asset.ID == f.assetID {
				t.Fatal("upload replaced the pre-existing Library asset")
			}
			f.assetID = asset.ID
			selected, err := f.projects.ResolveBoundedWorkspaceLibraryVersion(context.Background(), f.projectID, f.workspaceID, asset.ID, 1)
			if err != nil || selected.MIMEType != projectworkspace.LibraryDocumentMIME || selected.SizeBytes != int64(len(raw)) {
				t.Fatalf("upload lost canonical MIME/source grant/size: %+v %v", selected, err)
			}
			digest := sha256.Sum256(raw)
			if selected.ContentHash != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatal("upload changed original DOCX bytes")
			}
			if idx := decodeDocumentIndex(t, textLibraryHTTP(t, f, "GET", "document-index", auth)); idx.Status != "not_indexed" {
				t.Fatal("upload implicitly extracted document content")
			}
			idx := decodeDocumentIndex(t, textLibraryHTTP(t, f, "POST", "document-index", auth))
			if idx.Status != "ready" || idx.RawSourceSHA != selected.ContentHash || idx.UnitCount != 11 {
				t.Fatalf("uploaded DOCX did not extract faithfully: %+v", idx)
			}
			page := decodeDocumentPage(t, textLibraryHTTP(t, f, "GET", "document-text?q=Table", auth))
			if len(page.Units) != 1 || page.HasMore || page.Units[0].ParagraphNumber != 11 || page.Units[0].PartName != "word/document.xml" || page.Units[0].Text != "Table literal <script>" {
				t.Fatalf("uploaded paragraph citation: %+v", page)
			}
			reader, stored, err := f.artifacts.Open(context.Background(), strings.TrimPrefix(selected.StorageURI, "artifact:"))
			if err != nil {
				t.Fatal(err)
			}
			retained, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(retained, raw) || stored.ContentHash != idx.RawSourceSHA {
				t.Fatal("extracting multipart-uploaded DOCX altered its original artifact", err)
			}
			revoke := httptest.NewRequest(http.MethodDelete, "/v1/projects/"+f.projectID+"/library/"+asset.ID+"/grants/"+f.workspaceID, nil)
			auth(revoke)
			w = httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, revoke)
			if w.Code < 200 || w.Code >= 300 {
				t.Fatalf("source-grant revocation: %d %s", w.Code, w.Body.String())
			}
			w = textLibraryHTTP(t, f, "GET", "document-text", auth)
			if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "PRIVATE_DOCUMENT_TEXT") || strings.Contains(w.Body.String(), "Table literal") {
				t.Fatalf("uploaded content survived source-grant revocation: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
