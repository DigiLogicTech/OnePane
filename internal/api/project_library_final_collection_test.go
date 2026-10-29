//go:build integration

package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Retain the real grant/version lookup, then simulate a concurrent Project
// change while that final collection was in progress. No content or grant is
// fabricated by the wrapper.
type finalCollectionLibrary struct {
	*projectworkspace.Service
	afterFinalResolve func()
	resolves          int
}

func (s *finalCollectionLibrary) ResolveWorkspaceLibraryVersion(ctx context.Context, project, workspace, asset string, version int64) (projectworkspace.LibraryVersion, error) {
	s.resolves++
	selected, err := s.Service.ResolveWorkspaceLibraryVersion(ctx, project, workspace, asset, version)
	if err == nil && s.resolves == 2 && s.afterFinalResolve != nil {
		s.afterFinalResolve()
	}
	return selected, err
}

func TestLibraryContentReadsRevalidateProjectAfterFinalVersionCollection(t *testing.T) {
	for _, endpoint := range []string{"preview", "download"} {
		for _, scenario := range []string{"allowed", "project_archived", "project_tenant_changed"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				f := publishedLibraryHTTPFixture(t, nil)
				reader := &finalCollectionLibrary{Service: f.projects}
				reader.afterFinalResolve = func() {
					switch scenario {
					case "project_archived":
						if _, err := f.db.SQL().Exec(`UPDATE projects SET status='archived' WHERE id=?`, f.projectID); err != nil {
							t.Fatal(err)
						}
					case "project_tenant_changed":
						if _, err := f.db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign-tenant','Foreign tenant','active',1,1,1)`); err != nil {
							t.Fatal(err)
						}
						if _, err := f.db.SQL().Exec(`UPDATE projects SET workspace_id='foreign-tenant' WHERE id=?`, f.projectID); err != nil {
							t.Fatal(err)
						}
					}
				}
				f.server.projects = reader
				path := "/v1/projects/" + f.projectID + "/library/" + f.assetID + "/versions/1/content?workspace_id=" + f.workspaceID
				if endpoint == "preview" {
					path = "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/" + f.assetID + "/versions/1/preview"
				}
				w := httptest.NewRecorder()
				f.server.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if reader.resolves != 2 {
					t.Fatalf("final current version collection not exercised: %d", reader.resolves)
				}
				if scenario == "allowed" {
					if w.Code != 200 || w.Body.String() != string(f.body) || w.Header().Get("Cache-Control") != "no-store, private" {
						t.Fatalf("allowed content read failed: %d %s", w.Code, w.Body.String())
					}
				} else if w.Code != 403 || strings.Contains(w.Body.String(), string(f.body)) || w.Header().Get("X-OnePane-Content-Hash") != "" {
					t.Fatalf("changed Project content published after final collection: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
