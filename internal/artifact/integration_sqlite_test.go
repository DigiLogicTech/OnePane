//go:build integration

package artifact

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type artifactTestClock struct{ ms int64 }

func (c *artifactTestClock) Now() time.Time   { return time.UnixMilli(c.ms).UTC() }
func (c *artifactTestClock) UnixMilli() int64 { return c.ms }

func TestSQLiteArtifactMetadataEventAndContentVerification(t *testing.T) {
	ctx := context.Background()
	db := openArtifactTestDB(t)
	clk := &artifactTestClock{ms: 1_700_000_000_000}
	seedArtifactFixture(t, db.SQL(), clk.ms)
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, store, clk)
	creator := "agent1"
	project := "project1"
	content := []byte("artifact payload")
	a, err := svc.Create(ctx, CreateCommand{
		WorkspaceID: "ws1", ProjectID: &project, MediaType: "text/plain", CreatedBy: &creator,
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if a.SizeBytes != int64(len(content)) || a.ContentHash == "" || a.StorageRef == "" {
		t.Fatalf("artifact=%+v", a)
	}
	if err := svc.VerifyContent(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	r, gotMeta, err := svc.Open(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) || gotMeta.ContentHash != a.ContentHash {
		t.Fatal("artifact content/metadata mismatch")
	}
	var eventCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='artifact.created'`, a.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("artifact events=%d", eventCount)
	}
}

func TestSQLiteArtifactRejectsCrossWorkspaceCreatorAndRollsBackMetadataOnEventFailure(t *testing.T) {
	ctx := context.Background()
	db := openArtifactTestDB(t)
	clk := &artifactTestClock{ms: 1_700_000_000_000}
	seedArtifactFixture(t, db.SQL(), clk.ms)
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, store, clk)
	outsider := "outsider"
	_, err = svc.Create(ctx, CreateCommand{
		WorkspaceID: "ws1", MediaType: "text/plain", CreatedBy: &outsider,
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction},
	}, bytes.NewReader([]byte("x")))
	if !errors.Is(err, ErrCreatorIneligible) {
		t.Fatalf("cross-workspace creator err=%v", err)
	}

	creator := "agent1"
	missingActor := "missing"
	_, err = svc.Create(ctx, CreateCommand{
		WorkspaceID: "ws1", MediaType: "text/plain", CreatedBy: &creator, ActorPrincipalID: &missingActor,
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction},
	}, bytes.NewReader([]byte("rollback")))
	if err == nil {
		t.Fatal("expected event foreign-key failure")
	}
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("artifact metadata was not rolled back, count=%d", count)
	}
}

func openArtifactTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "artifact.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedArtifactFixture(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	stmts := []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws1','One','active',1,?,?)`,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws2','Two','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent1','agent','Agent','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Outsider','active',1,?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws1','admin','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws1','agent1','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws2','outsider','active',?,?)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at) VALUES('project1','ws1','P','active','{}','{}',1,'admin',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
}
