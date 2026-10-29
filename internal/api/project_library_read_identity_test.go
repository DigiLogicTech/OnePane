//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type inspectedLibraryStore struct {
	artifact.BlobStore
	after func(string)
}

func (s *inspectedLibraryStore) Verify(ctx context.Context, ref, hash string, size int64) error {
	err := s.BlobStore.Verify(ctx, ref, hash, size)
	if err == nil && s.after != nil {
		s.after("verify")
	}
	return err
}
func (s *inspectedLibraryStore) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	reader, err := s.BlobStore.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	if s.after != nil {
		s.after("open")
	}
	return &inspectedLibraryReader{ReadCloser: reader, after: func() {
		if s.after != nil {
			s.after("read")
		}
	}}, nil
}

type inspectedLibraryReader struct {
	io.ReadCloser
	after func()
}

func (r *inspectedLibraryReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if r.after != nil {
		r.after()
	}
	return n, err
}

func TestLibraryContentReadsRevalidateCredentialAndGrantAfterCollection(t *testing.T) {
	for _, endpoint := range []string{"preview", "download", "evidence"} {
		stages := []string{"verify", "open"}
		if endpoint != "download" {
			stages = append(stages, "read")
		}
		for _, stage := range stages {
			t.Run(endpoint+"/"+stage, func(t *testing.T) {
				var store *inspectedLibraryStore
				f := publishedLibraryHTTPFixture(t, func(base artifact.BlobStore) artifact.BlobStore {
					store = &inspectedLibraryStore{BlobStore: base}
					return store
				})
				for _, scenario := range []string{"allowed", "revoked", "principal_changed", "credential_changed", "principal_type_changed", "auth_failed", "permission_revoked", "grant_revoked", "project_archived", "workspace_archived"} {
					t.Run(scenario, func(t *testing.T) {
						auth := &inspectedTaskAuth{identity: Identity{PrincipalID: "human", CredentialID: "original", PrincipalType: "human"}}
						f.server.auth = auth
						fired := false
						store.after = func(at string) {
							if at != stage || fired {
								return
							}
							fired = true
							switch scenario {
							case "revoked":
								auth.err = ErrUnauthenticated
							case "principal_changed":
								auth.identity.PrincipalID = "replacement"
							case "credential_changed":
								auth.identity.CredentialID = "replacement"
							case "principal_type_changed":
								auth.identity.PrincipalType = "agent"
							case "auth_failed":
								auth.err = errors.New("PRIVATE_AUTH_CANARY")
							case "permission_revoked":
								auth.revoked = true
							case "grant_revoked":
								if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID); err != nil {
									t.Fatal(err)
								}
							case "project_archived":
								if _, err := f.db.SQL().Exec(`UPDATE projects SET status='archived' WHERE id=?`, f.projectID); err != nil {
									t.Fatal(err)
								}
							case "workspace_archived":
								if _, err := f.db.SQL().Exec(`UPDATE project_workspaces SET status='archived' WHERE id=?`, f.workspaceID); err != nil {
									t.Fatal(err)
								}
							}
						}
						t.Cleanup(func() {
							store.after = nil
							if _, err := f.db.SQL().Exec(`UPDATE projects SET status='active' WHERE id=?`, f.projectID); err != nil {
								t.Fatal(err)
							}
							if _, err := f.db.SQL().Exec(`UPDATE project_workspaces SET status='active' WHERE id=?`, f.workspaceID); err != nil {
								t.Fatal(err)
							}
							if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=1 WHERE asset_id=?`, f.assetID); err != nil {
								t.Fatal(err)
							}
						})
						path := "/v1/projects/" + f.projectID + "/library/" + f.assetID + "/versions/1/content?workspace_id=" + f.workspaceID
						if endpoint == "preview" {
							path = "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/" + f.assetID + "/versions/1/preview"
						}
						request := httptest.NewRequest("GET", path, nil)
						if endpoint == "evidence" {
							input, err := json.Marshal(map[string]any{"selections": []projectworkspace.WorkspaceEvidenceSelection{{AssetID: f.assetID, Version: 1}}})
							if err != nil {
								t.Fatal(err)
							}
							request = httptest.NewRequest("POST", "/v1/projects/"+f.projectID+"/workspaces/"+f.workspaceID+"/evidence-packets/verified-text", bytes.NewReader(input))
						}
						w := httptest.NewRecorder()
						f.server.Handler().ServeHTTP(w, request)
						text := w.Body.String()
						if endpoint == "evidence" && w.Code == 200 {
							var result verifiedWorkspaceEvidence
							if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
								t.Fatal(err)
							}
							if len(result.Entries) != 1 {
								t.Fatal("missing evidence entry")
							}
							text = result.Entries[0].Text
						}
						if !fired {
							t.Fatal("slow collection stage not exercised")
						}
						if scenario == "allowed" {
							if w.Code != 200 || text != string(f.body) || w.Header().Get("Cache-Control") != "no-store, private" {
								t.Fatalf("valid content read failed: %d %s", w.Code, w.Body.String())
							}
						} else {
							if w.Code == 200 || strings.Contains(w.Body.String(), string(f.body)) || strings.Contains(w.Body.String(), "CANARY") {
								t.Fatalf("revoked read disclosed content: %d %s", w.Code, w.Body.String())
							}
						}
					})
				}
			})
		}
	}
}
