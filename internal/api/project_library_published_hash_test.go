//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/workspacepublisher"
)

type publishedLibraryFixture struct {
	server                                                   *Server
	db                                                       *sqlitestore.DB
	projects                                                 *projectworkspace.Service
	artifacts                                                *artifact.Service
	store                                                    artifact.BlobStore
	projectID, workspaceID, assetID, artifactID, contentHash string
	version                                                  int64
	body                                                     []byte
}

func TestLibraryArtifactPublishedWorkspaceBytesRemainResearchEvidence(t *testing.T) {
	f := publishedLibraryHTTPFixture(t, nil)
	input, _ := json.Marshal(map[string]any{"selections": []projectworkspace.WorkspaceEvidenceSelection{{AssetID: f.assetID, Version: 1}}})
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/v1/projects/"+f.projectID+"/workspaces/"+f.workspaceID+"/evidence-packets/verified-text", bytes.NewReader(input)))
	if w.Code != 200 {
		t.Fatalf("genuine published Research evidence unavailable: status=%d body=%s", w.Code, w.Body.String())
	}
	var result verifiedWorkspaceEvidence
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Text != string(f.body) || result.Entries[0].ContentHash != f.contentHash || result.Entries[0].Trust != "untrusted_library_content" {
		t.Fatalf("published evidence not exact hash-bound untrusted bytes: %+v", result)
	}
}

// Real Task/Attempt, pinned OCI declaration, publisher, grants and managed bytes;
// no OCI process or physical model is executed by this fixture.
func publishedLibraryHTTPFixture(t *testing.T, wrap func(artifact.BlobStore) artifact.BlobStore) *publishedLibraryFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "published-library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('local-node','Local Node',1,'publisher-api-local-node','local','{}','{}',1,1,1)`,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Operator','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
	} {
		if _, err = db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	projects := projectworkspace.NewService(db.SQL(), db, clock.Real{})
	p, err := projects.CreateProject(ctx, projectworkspace.CreateProjectCommand{WorkspaceID: "tenant", Name: "Published Project", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := projects.CreateWorkspaceView(ctx, projectworkspace.CreateWorkspaceViewCommand{ProjectID: p.ID, Name: "Source", ActorPrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := projects.CreateRuntime(ctx, projectworkspace.CreateRuntimeCommand{ProjectID: p.ID, ProjectWorkspaceID: &w.ID, CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	app, err := projects.DeclareApplication(ctx, projectworkspace.DeclareApplicationCommand{RuntimeID: runtime.ID, Name: "Builder", SourceKind: projectworkspace.AppOCIImage, SourceRef: "ghcr.io/example/builder@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`UPDATE project_runtimes SET status='running' WHERE id=?`, runtime.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`UPDATE project_applications SET status='running' WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	local, err := artifact.NewLocalStore(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	var store artifact.BlobStore = local
	if wrap != nil {
		store = wrap(store)
	}
	artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	row, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", ProjectID: &p.ID, ProjectWorkspaceID: &w.ID, Objective: "Publish checked result"})
	if err != nil {
		t.Fatal(err)
	}
	row, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	actor := "human"
	_, attempt, err := tasks.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision, WorkerPrincipalID: &actor})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("Published checked result\n<script>untrusted text</script>\n")
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	receipt, err := workspacepublisher.New(db.SQL(), artifacts, projects, "local-node").PublishWorkspaceFile(ctx, sandboxrunner.WorkspacePublicationRequest{WorkspaceID: "tenant", TaskID: row.ID, AttemptID: attempt.ID, RuntimeID: runtime.ID, ApplicationID: app.ID, Path: "outputs/result.txt", Name: "Checked result", MediaType: "text/plain", Content: body, ContentHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(projects, fakeEvents{}, &liveRoomAuth{})
	server.SetLibraryArtifacts(artifacts)
	return &publishedLibraryFixture{server: server, db: db, projects: projects, artifacts: artifacts, store: store, projectID: p.ID, workspaceID: w.ID, assetID: receipt.LibraryAssetID, artifactID: receipt.ArtifactID, contentHash: hash, version: receipt.Version, body: body}
}

func TestLibraryArtifactPublishedWorkspaceBytesRemainDownloadable(t *testing.T) {
	f := publishedLibraryHTTPFixture(t, nil)
	selected, err := f.projects.ResolveWorkspaceLibraryVersion(context.Background(), f.projectID, f.workspaceID, f.assetID, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.artifacts.Get(context.Background(), f.artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ContentHash != f.contentHash || raw.ContentHash != "sha256:"+f.contentHash {
		t.Fatalf("fixture does not exercise genuine publisher representations: library=%q artifact=%q", selected.ContentHash, raw.ContentHash)
	}
	for _, route := range []struct{ name, path string }{
		{"preview", "/v1/projects/" + f.projectID + "/workspaces/" + f.workspaceID + "/library/" + f.assetID + "/versions/1/preview"},
		{"download", "/v1/projects/" + f.projectID + "/library/" + f.assetID + "/versions/1/content?workspace_id=" + f.workspaceID},
	} {
		t.Run(route.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, httptest.NewRequest("GET", route.path, nil))
			if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), f.body) {
				t.Fatalf("genuine published bytes unavailable: status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store, private" {
				t.Fatalf("unsafe content headers: %v", w.Header())
			}
		})
	}
}
