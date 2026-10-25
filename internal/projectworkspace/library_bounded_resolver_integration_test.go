//go:build integration

package projectworkspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func boundedLibraryFixture(t *testing.T) (*sqlitestore.DB, *Service, func(string, ...any)) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "bounded-library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Project','active','{}','{}','human',1,1)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('other','tenant','Other','active','{}','{}','human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('target','p','Target',1,1),('other-target','other','Other',1,1)`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES('wanted','p','Wanted','text/plain',2,1,1)`,
		`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) VALUES('wanted',1,printf('%064x',2048),3,'text/plain','artifact:wanted',1),('wanted',2,printf('%064x',4096),4,'text/plain','artifact:wanted-v2',1)`,
		`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<129) INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) SELECT printf('source-%03d',x),'p','Source',1,1 FROM n`,
		`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,enabled,created_by,created_at,updated_at) SELECT id,'p',id,'target','Source',0,'human',1,1 FROM project_workspaces WHERE id LIKE 'source-%'`,
		`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) SELECT id,id,'wanted',1,printf('%064x',2048),'human',1 FROM project_workspace_links WHERE id<>'source-129'`,
		`UPDATE project_workspace_links SET enabled=1 WHERE id='source-128'`,
	} {
		exec(q)
	}
	return db, NewService(db.SQL(), db, clock.Real{}), exec
}

func TestWorkspaceLibraryPublicationBoundedFanout(t *testing.T) {
	db, svc, exec := boundedLibraryFixture(t)
	ctx := context.Background()
	resolve := func(want error) {
		t.Helper()
		got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1)
		if !errors.Is(err, want) {
			t.Fatalf("resolve error %v, want %v", err, want)
		}
		if want != nil {
			if got != (LibraryVersion{}) {
				t.Fatalf("denied/overflow resolution leaked version: %#v", got)
			}
		} else if got.AssetID != "wanted" || got.Version != 1 || got.ContentHash != fmt.Sprintf("%064x", 2048) || got.StorageURI != "artifact:wanted" {
			t.Fatalf("wrong exact version: %#v", got)
		}
	}
	resolve(nil) // The only valid link is the 128th raw receipt.
	exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES('overflow','source-129','wanted',1,printf('%064x',2048),'human',1)`)
	resolve(ErrLibraryPublicationLimit) // Never authorize from a partial set, even with a visible valid link.
	exec(`UPDATE project_workspace_links SET enabled=0 WHERE id='source-128'`)
	exec(`UPDATE project_workspace_links SET enabled=1 WHERE id='source-129'`)
	resolve(ErrLibraryPublicationLimit) // Valid link beyond the cap is not silently reported absent.
	legacy, err := svc.ResolveWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1)
	if err != nil || legacy.AssetID != "wanted" {
		t.Fatalf("legacy semantics changed: %#v %v", legacy, err)
	}
	exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES('direct','target','wanted',1,'pinned',1,'{"read":true}',1,1)`)
	resolve(nil) // A complete direct grant is independent of directional fan-out.
	exec(`UPDATE workspace_library_grants SET version_policy='latest',pinned_version=NULL WHERE id='direct'`)
	resolve(ErrLibraryPublicationLimit) // Latest must not expose old version 1.
	if got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 2); err != nil || got.Version != 2 {
		t.Fatalf("latest grant: %#v %v", got, err)
	}
	exec(`UPDATE workspace_library_grants SET version_policy='pinned',pinned_version=1,enabled=0 WHERE id='direct'`)
	resolve(ErrLibraryPublicationLimit)
	exec(`UPDATE workspace_library_grants SET enabled=1,permissions_json='{"read":false}' WHERE id='direct'`)
	resolve(ErrLibraryPublicationLimit)
	exec(`UPDATE workspace_library_grants SET permissions_json='{"read":true}' WHERE id='direct'`)
	exec(`UPDATE projects SET status='archived' WHERE id='p'`)
	resolve(ErrCrossWorkspace)
	exec(`UPDATE projects SET status='active' WHERE id='p'`)
	exec(`UPDATE project_workspaces SET status='archived' WHERE id='target'`)
	resolve(ErrCrossWorkspace)
	exec(`UPDATE project_workspaces SET status='active' WHERE id='target'`)
	exec(`UPDATE project_library_assets SET archived=1 WHERE id='wanted'`)
	resolve(ErrCrossWorkspace)
	exec(`UPDATE project_library_assets SET archived=0 WHERE id='wanted'`)
	resolve(nil)
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM project_workspace_publications`).Scan(&count); err != nil || count != 129 {
		t.Fatalf("resolver mutated receipt history: %d %v", count, err)
	}
}

func TestWorkspaceLibraryPublicationBoundedPermissions(t *testing.T) {
	_, svc, exec := boundedLibraryFixture(t)
	ctx := context.Background()
	check := func(want error) {
		t.Helper()
		got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1)
		if !errors.Is(err, want) || (want != nil && got != (LibraryVersion{})) {
			t.Fatalf("version %#v error %v, want %v", got, err, want)
		}
	}
	check(nil)
	for _, pair := range []struct{ name, change, restore string }{
		{"disabled", `UPDATE project_workspace_links SET enabled=0 WHERE id='source-128'`, `UPDATE project_workspace_links SET enabled=1 WHERE id='source-128'`},
		{"expired", `UPDATE project_workspace_links SET expires_at_ms=1 WHERE id='source-128'`, `UPDATE project_workspace_links SET expires_at_ms=NULL WHERE id='source-128'`},
		{"source archived", `UPDATE project_workspaces SET status='archived' WHERE id='source-128'`, `UPDATE project_workspaces SET status='active' WHERE id='source-128'`},
		{"source foreign", `UPDATE project_workspaces SET project_id='other' WHERE id='source-128'`, `UPDATE project_workspaces SET project_id='p' WHERE id='source-128'`},
		{"link foreign", `UPDATE project_workspace_links SET project_id='other' WHERE id='source-128'`, `UPDATE project_workspace_links SET project_id='p' WHERE id='source-128'`},
		{"wrong target", `UPDATE project_workspace_links SET target_workspace_id='other-target' WHERE id='source-128'`, `UPDATE project_workspace_links SET target_workspace_id='target' WHERE id='source-128'`},
		{"wrong hash", `UPDATE project_workspace_publications SET content_hash=printf('%064x',9) WHERE id='source-128'`, `UPDATE project_workspace_publications SET content_hash=printf('%064x',2048) WHERE id='source-128'`},
	} {
		t.Run(pair.name, func(t *testing.T) { exec(pair.change); check(ErrCrossWorkspace); exec(pair.restore); check(nil) })
	}
	for _, args := range []struct {
		project, workspace, asset string
		version                   int64
		want                      error
	}{
		{"p", "other-target", "wanted", 1, ErrCrossWorkspace}, {"other", "target", "wanted", 1, ErrCrossWorkspace},
		{"p", "target", "wanted", 2, ErrCrossWorkspace}, {"p", "target", "unknown", 1, ErrCrossWorkspace},
		{"", "target", "wanted", 1, ErrInvalidCommand}, {"p", "", "wanted", 1, ErrInvalidCommand},
		{"p", "target", "", 1, ErrInvalidCommand}, {"p", "target", "wanted", 0, ErrInvalidCommand},
	} {
		got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, args.project, args.workspace, args.asset, args.version)
		if !errors.Is(err, args.want) || got != (LibraryVersion{}) {
			t.Fatalf("invalid scope %#v returned %#v %v", args, got, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := svc.ResolveBoundedWorkspaceLibraryVersion(cancelled, "p", "target", "wanted", 1); !errors.Is(err, context.Canceled) || got != (LibraryVersion{}) {
		t.Fatalf("cancelled: %#v %v", got, err)
	}
}

func TestWorkspaceLibraryPublicationBoundedProductionPlan(t *testing.T) {
	db, svc, exec := boundedLibraryFixture(t)
	// Add unrelated publication history with the same hash to challenge both
	// index selection and accidentally Project-wide joins, before and after stats.
	exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) SELECT 'history-'||x,'p','History','text/plain',1,1,1 FROM n`)
	exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) SELECT id,1,printf('%064x',2048),3,'text/plain','artifact:other',1 FROM project_library_assets WHERE id LIKE 'history-%'`)
	exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) SELECT id,'source-001',id,1,printf('%064x',2048),'human',1 FROM project_library_assets WHERE id LIKE 'history-%'`)
	for _, stats := range []bool{false, true} {
		if stats {
			exec(`ANALYZE`)
		}
		plan := workspaceLibraryResolverPlan(t, db.SQL(), boundedWorkspaceLibraryVersionSQL, "target", "target", "p", "wanted", int64(1), "target", int64(2))
		if !regexp.MustCompile(`SEARCH pub USING (?:COVERING )?INDEX [^ ]+ \(asset_id=\? AND asset_version=\? AND content_hash=\?\)`).MatchString(plan) || !strings.Contains(plan, "MATERIALIZE candidates") || !regexp.MustCompile(`SEARCH l USING (?:COVERING )?INDEX [^ ]+ \(id=\?\)`).MatchString(plan) || strings.Contains(plan, "SCAN pub") || strings.Contains(plan, "SCAN l\n") || strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("unbounded actual production plan (stats=%v):\n%s", stats, plan)
		}
		for _, line := range strings.Split(plan, "\n") {
			if strings.HasPrefix(line, "SCAN ") && !regexp.MustCompile(`^SCAN (?:selected|candidates|c|s)$`).MatchString(line) {
				t.Fatalf("persistent-table scan in bounded production plan (stats=%v):\n%s", stats, plan)
			}
		}
		// Inspect the actual shipped SQL's bytecode too: query-plan SEARCH alone
		// cannot prove a bound on rows sharing every indexed lookup key.
		rows, err := db.SQL().Query(`EXPLAIN `+boundedWorkspaceLibraryVersionSQL, "target", "target", "p", "wanted", int64(1), "target", int64(2))
		if err != nil {
			t.Fatal(err)
		}
		limitRegister, guarded := int64(-1), false
		for rows.Next() {
			var addr, p1, p2, p3, p5 int64
			var op string
			var p4, comment any
			if err := rows.Scan(&addr, &op, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if op == "Integer" && p1 == 129 {
				limitRegister = p2
			}
			if op == "DecrJumpZero" && p1 == limitRegister && limitRegister >= 0 {
				guarded = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if !guarded {
			t.Fatalf("production VM lacks 129-candidate limit (stats=%v)", stats)
		}
		if _, err := svc.ResolveBoundedWorkspaceLibraryVersion(context.Background(), "p", "target", "wanted", 1); err != nil {
			t.Fatal(err)
		}
	}
	exec(`PRAGMA query_only=ON`)
	if _, err := svc.ResolveBoundedWorkspaceLibraryVersion(context.Background(), "p", "target", "wanted", 1); err != nil {
		t.Fatalf("read-only resolution: %v", err)
	}
}

func TestWorkspaceLibraryPublicationBoundedMetadata(t *testing.T) {
	_, svc, exec := boundedLibraryFixture(t)
	ctx := context.Background()
	for _, entry := range []struct{ column, value, restore string }{
		{"content_hash", strings.Repeat("h", 72), fmt.Sprintf("%064x", 2048)},
		{"mime_type", strings.Repeat("m", 257), "text/plain"},
		{"mime_type", strings.Repeat("界", 86), "text/plain"},
		{"storage_uri", strings.Repeat("s", 266), "artifact:wanted"},
		{"storage_uri", strings.Repeat("界", 89), "artifact:wanted"},
	} {
		t.Run(entry.column+fmt.Sprint(len(entry.value)), func(t *testing.T) {
			exec("UPDATE project_library_asset_versions SET "+entry.column+"=? WHERE asset_id='wanted' AND version=1", entry.value)
			got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1)
			if err != ErrLibraryMetadataLimit || got != (LibraryVersion{}) {
				t.Fatalf("oversized persisted metadata returned %#v %v", got, err)
			}
			exec("UPDATE project_library_asset_versions SET "+entry.column+"=? WHERE asset_id='wanted' AND version=1", entry.restore)
		})
	}
	// Boundaries count UTF-8 bytes, not characters, and do not reject exact limits.
	exec(`UPDATE project_library_asset_versions SET mime_type=?,storage_uri=? WHERE asset_id='wanted' AND version=1`, strings.Repeat("界", 85)+"x", strings.Repeat("界", 88)+"x")
	if got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); err != nil || len(got.MIMEType) != 256 || len(got.StorageURI) != 265 {
		t.Fatalf("exact metadata limits %#v %v", got, err)
	}
	exec(`UPDATE project_library_asset_versions SET mime_type='text/plain',storage_uri='artifact:wanted' WHERE asset_id='wanted' AND version=1`)
	for _, id := range []string{strings.Repeat("a", 257), strings.Repeat("界", 86)} {
		if got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", id, 1); err != ErrInvalidCommand || got != (LibraryVersion{}) {
			t.Fatalf("oversized input %#v %v", got, err)
		}
	}
	// A malformed persisted link ID remains a raw candidate; it must neither be
	// materialized unboundedly nor removed to manufacture a complete inventory.
	exec(`DELETE FROM project_workspace_links WHERE id='source-129'`)
	link := strings.Repeat("界", 100)
	exec(`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,enabled,created_by,created_at,updated_at) VALUES(?,'p','source-129','target','Oversized',1,'human',1,1)`, link)
	exec(`DELETE FROM project_workspace_publications WHERE id='source-001'`)
	exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES('oversized',?,'wanted',1,printf('%064x',2048),'human',1)`, link)
	if got, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); err != ErrLibraryMetadataLimit || got != (LibraryVersion{}) {
		t.Fatalf("oversized stored link %#v %v", got, err)
	}
	exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES('direct','target','wanted',1,'pinned',1,'{"read":true}',1,1)`)
	if _, err := svc.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); err != nil {
		t.Fatalf("direct grant should bypass malformed unrelated directional set: %v", err)
	}
}
