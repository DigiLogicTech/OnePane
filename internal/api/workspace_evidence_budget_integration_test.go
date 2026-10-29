//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestVerifiedWorkspaceEvidenceHTTPPinsVerificationToSelectedSizeAndHash(t *testing.T) {
	for _, endpoint := range []string{"evidence", "preview", "download"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, scenario := range []string{"size", "hash"} {
				t.Run(scenario, func(t *testing.T) {
					var store *inspectedVerificationAttempts
					body := []byte("PRIVATE_EVIDENCE_BUDGET original source")
					f := textLibraryHTTPFixture(t, body, "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
						store = &inspectedVerificationAttempts{BlobStore: base}
						return store
					})
					auth := textLibraryHTTPAuth(t, f, "bearer")
					query := `UPDATE artifacts SET size_bytes=size_bytes+1 WHERE id=?`
					if scenario == "hash" {
						query = `UPDATE artifacts SET content_hash='sha256:'||printf('%064x',1) WHERE id=?`
					}
					if _, err := f.db.SQL().Exec(query, f.artifactID); err != nil {
						t.Fatal(err)
					}
					input, err := json.Marshal(map[string]any{"selections": []projectworkspace.WorkspaceEvidenceSelection{{AssetID: f.assetID, Version: 1}}})
					if err != nil {
						t.Fatal(err)
					}
					store.calls, store.reads = 0, 0
					r := httptest.NewRequest("POST", "/v1/projects/"+f.projectID+"/workspaces/"+f.workspaceID+"/evidence-packets/verified-text", bytes.NewReader(input))
					if endpoint == "preview" {
						r = httptest.NewRequest("GET", textLibraryPath(f, "preview"), nil)
					} else if endpoint == "download" {
						r = httptest.NewRequest("GET", "/v1/projects/"+f.projectID+"/library/"+f.assetID+"/versions/1/content?workspace_id="+f.workspaceID, nil)
					}
					auth(r)
					w := httptest.NewRecorder()
					f.server.Handler().ServeHTTP(w, r)
					if w.Code == 200 || strings.Contains(w.Body.String(), string(body)) || store.calls != 0 || store.reads != 0 {
						t.Fatalf("changed %s enlarged operator evidence verification: status=%d calls=%d bytes=%d", scenario, w.Code, store.calls, store.reads)
					}
				})
			}
		})
	}
}

func TestVerifiedWorkspaceEvidenceHTTPRechecksRealCredentialAfterCollection(t *testing.T) {
	for _, method := range []string{"bearer", "browser"} {
		for _, stage := range []string{"verify", "open", "read"} {
			for _, scenario := range []string{"credential", "membership"} {
				t.Run(method+"/"+stage+"/"+scenario, func(t *testing.T) {
					var store *inspectedLibraryStore
					f := textLibraryHTTPFixture(t, []byte("PRIVATE_REVOKED_EVIDENCE"), "text/plain", func(base artifact.BlobStore) artifact.BlobStore {
						store = &inspectedLibraryStore{BlobStore: base}
						return store
					})
					auth := textLibraryHTTPAuth(t, f, method)
					fired := false
					store.after = func(at string) {
						if at != stage || fired {
							return
						}
						fired = true
						query := `UPDATE api_credentials SET revoked_at=1 WHERE id='text-api'`
						if method == "browser" {
							query = `UPDATE auth_sessions SET revoked_at=1 WHERE id='text-session'`
						}
						if scenario == "membership" {
							query = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tenant' AND principal_id='human'`
						}
						if _, err := f.db.SQL().Exec(query); err != nil {
							t.Fatal(err)
						}
					}
					input, err := json.Marshal(map[string]any{"selections": []projectworkspace.WorkspaceEvidenceSelection{{AssetID: f.assetID, Version: 1}}})
					if err != nil {
						t.Fatal(err)
					}
					r := httptest.NewRequest("POST", "/v1/projects/"+f.projectID+"/workspaces/"+f.workspaceID+"/evidence-packets/verified-text", bytes.NewReader(input))
					auth(r)
					w := httptest.NewRecorder()
					f.server.Handler().ServeHTTP(w, r)
					if !fired || w.Code == 200 || strings.Contains(w.Body.String(), "PRIVATE_REVOKED_EVIDENCE") {
						t.Fatalf("revocation not enforced: fired=%v status=%d body=%s", fired, w.Code, w.Body.String())
					}
				})
			}
		}
	}
}
