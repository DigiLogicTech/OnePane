package sqlite

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestLibraryPublicationVersionIndexPreservesPopulatedUpgrade(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/publication-upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.SQL().Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	items, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var expected migration
	for _, m := range items {
		if m.version == 109 {
			expected = m
		}
		if m.version < 109 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Project','active','{}','{}','human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('target','p','Target',1,1)`,
		`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) SELECT 'history-'||x,'p','Source',1,1 FROM n`,
		`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,enabled,created_by,created_at,updated_at) SELECT id,'p',id,'target','Source',1,'human',1,1 FROM project_workspaces WHERE id<>'target'`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) SELECT id,'p','Other','text/plain',1,1,1 FROM project_workspace_links`,
		`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) SELECT id,1,printf('%064x',2048),3,'text/plain','artifact:other',1 FROM project_workspace_links`,
		`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) SELECT id,id,id,1,printf('%064x',2048),'human',1 FROM project_workspace_links`,
		`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES('direct','target','history-1',1,'pinned',1,'{"read":true}',1,1)`,
	} {
		if _, err = db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"workspaces", "principals", "projects", "project_workspaces", "project_library_assets", "project_library_asset_versions", "project_workspace_links", "project_workspace_publications", "workspace_library_grants"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("publication index upgrade altered existing scope/grant/version/publication records")
	}
	var applied int
	if expected.filename != "0109_library_publication_version_lookup.sql" {
		t.Fatal("missing expected migration109 source")
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=109 AND name=? AND checksum=?`, expected.name, expected.checksum).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration109=%d err=%v", applied, err)
	}
	var table string
	if err = db.SQL().QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type='index' AND name='idx_workspace_publications_asset_version_hash'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "project_workspace_publications" {
		t.Fatalf("wrong indexed table: %s", table)
	}
	var unique, partial int
	var origin string
	if err = db.SQL().QueryRow(`SELECT "unique",origin,partial FROM pragma_index_list('project_workspace_publications') WHERE name='idx_workspace_publications_asset_version_hash'`).Scan(&unique, &origin, &partial); err != nil || unique != 0 || partial != 0 || origin != "c" {
		t.Fatalf("unexpected index kind unique=%d partial=%d origin=%s err=%v", unique, partial, origin, err)
	}
	keyRows, err := db.SQL().Query(`PRAGMA index_xinfo('idx_workspace_publications_asset_version_hash')`)
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for keyRows.Next() {
		var sequence, cid, descending, key int
		var name, collation sql.NullString
		if err = keyRows.Scan(&sequence, &cid, &name, &descending, &collation, &key); err != nil {
			keyRows.Close()
			t.Fatal(err)
		}
		if key == 0 {
			continue
		}
		if !name.Valid || cid < 0 || descending != 0 || !collation.Valid || collation.String != "BINARY" || sequence != len(columns) {
			keyRows.Close()
			t.Fatal("unexpected expression/order/collation in publication index")
		}
		columns = append(columns, name.String)
	}
	if err = keyRows.Err(); err != nil {
		keyRows.Close()
		t.Fatal(err)
	}
	if err = keyRows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(columns, []string{"asset_id", "asset_version", "content_hash", "link_id"}) {
		t.Fatalf("unexpected ordered index columns: %v", columns)
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("upgrade left a foreign key violation")
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for _, asset := range []string{"history-1", "absent-asset"} {
		plan := recoveryDirectLookupPlan(t, db.SQL(), `SELECT link_id FROM project_workspace_publications WHERE asset_id=? AND asset_version=1 AND content_hash=printf('%064x',2048) LIMIT 1`, asset)
		if !regexp.MustCompile(`SEARCH project_workspace_publications USING (?:COVERING )?INDEX [^ ]+ \(asset_id=\? AND asset_version=\? AND content_hash=\?\)`).MatchString(plan) || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "AUTOMATIC") {
			t.Fatalf("exact version/hash lookup not indexed: %s", plan)
		}
	}
}
