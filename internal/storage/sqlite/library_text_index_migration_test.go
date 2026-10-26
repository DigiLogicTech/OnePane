package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This is a populated source-database upgrade and rollback-copy check. It does
// not install OnePane or read, move or alter any real Library/user data roots.
func TestLibraryTextIndexMigrationPreservesPopulatedUpgradeAndBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "text-upgrade.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT`)
	items, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var expected migration
	latest := 113
	for _, m := range items {
		if m.version < 114 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
		if m.version == 114 {
			expected = m
		}
		if m.version > latest {
			latest = m.version
		}
	}
	if expected.filename != "0114_library_text_index.sql" {
		t.Fatal("missing reviewed Library text migration114")
	}
	raw := []byte("Existing immutable café\r\n")
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Retained tenant','active',7,1,2)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Retained owner','active',4,1,2)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,2)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Existing Project','active','{"cloud_fallback":false,"mode":"L4"}','{"enabled":false,"excluded":["secrets"]}','human',1,2)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('source','p','Existing source',1,2),('target','p','Existing target',1,2)`,
		`INSERT INTO project_storage_locations(project_id,storage_root,previous_root,move_state,move_detail_json,revision,updated_at) VALUES('p','D:/Chosen OnePane/Projects/p','E:/PriorData/p','ready','{"approved":true}',9,2)`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES('retained','p','Existing note.md','text/markdown',2,1,2)`,
		`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES('pinned','target','retained',1,'pinned',1,'{"read":true,"execute":false}',1,2),('latest','source','retained',1,'latest',NULL,'{"read":true,"execute":false}',1,2)`,
		`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,enabled,created_by,created_at,updated_at) VALUES('published-link','p','source','target','Retained sharing',1,'human',1,2)`,
	} {
		exec(query)
	}
	for version := 1; version <= 2; version++ {
		id := fmt.Sprintf("old-artifact-%d", version)
		exec(`INSERT INTO artifacts(id,workspace_id,project_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_by,created_at) VALUES(?,'tenant','p',?,'text/markdown',?,'retained-local-ref','internal','any','untrusted_content','active','{"source":"existing_alpha_upload"}','human',1)`, id, hash, len(raw))
		exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,provenance_json,created_at) VALUES('retained',?,?,?,'text/markdown',?,'{"origin":"existing_alpha_upload"}',1)`, version, hash, len(raw), "artifact:"+id)
	}
	exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES('old-publication','published-link','retained',1,?,'human',2)`, hash)
	tables := []string{"workspaces", "principals", "workspace_memberships", "projects", "project_workspaces", "project_storage_locations", "project_library_assets", "project_library_asset_versions", "workspace_library_grants", "project_workspace_links", "project_workspace_publications", "artifacts"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatalf("idempotent upgrade: %v", err)
	}
	if got := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, got) {
		t.Fatal("additive text upgrade changed existing Library/Project/configuration/artifact records")
	}
	var headers, lines, applied int
	if err = db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_text_indexes),(SELECT COUNT(*) FROM project_library_text_lines),(SELECT COUNT(*) FROM schema_migrations WHERE version=114 AND name=? AND checksum=?)`, expected.name, expected.checksum).Scan(&headers, &lines, &applied); err != nil || headers != 0 || lines != 0 || applied != 1 {
		t.Fatalf("upgrade silently indexed old bytes or missed exact migration headers=%d lines=%d applied=%d err=%v", headers, lines, applied, err)
	}
	backups, err := filepath.Glob(fmt.Sprintf("%s.pre-migrate-v%04d-*.bak", path, latest))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one retained v113 backup: %v %v", backups, err)
	}
	if err = verifyPrivateBackupFile(backups[0]); err != nil {
		t.Fatalf("rollback backup permissions: %v", err)
	}
	backup, err := Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	var oldVersion, newTables int
	if err = backup.SQL().QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&oldVersion); err != nil || oldVersion != 113 {
		backup.Close()
		t.Fatalf("rollback version=%d err=%v", oldVersion, err)
	}
	if got := recoveryProvenanceRows(t, backup.SQL(), tables); !reflect.DeepEqual(before, got) {
		backup.Close()
		t.Fatal("rollback copy lost populated records or configuration")
	}
	if err = backup.SQL().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('project_library_text_indexes','project_library_text_lines')`).Scan(&newTables); err != nil || newTables != 0 {
		backup.Close()
		t.Fatalf("backup contains post-upgrade tables count=%d err=%v", newTables, err)
	}
	var integrity string
	if err = backup.SQL().QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		backup.Close()
		t.Fatalf("backup integrity %q %v", integrity, err)
	}
	if err = backup.Close(); err != nil {
		t.Fatal(err)
	}
	// New derived records are insert-only, scope/version-linked and bounded.
	exec(`INSERT INTO project_library_text_indexes(asset_id,version,parser_version,content_hash,size_bytes,status,line_count,created_by,created_at) VALUES('retained',1,1,?,?,'ready',1,'human',3)`, hash, len(raw))
	text := strings.TrimSuffix(string(raw), "\n")
	lineHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text)))
	exec(`INSERT INTO project_library_text_lines(asset_id,version,parser_version,line_number,byte_start,byte_end,content,line_hash) VALUES('retained',1,1,1,0,?,?,?)`, len(text), text, lineHash)
	reject := func(query string, args ...any) {
		t.Helper()
		if _, err := db.SQL().ExecContext(ctx, query, args...); err == nil {
			t.Fatalf("new text schema accepted invalid mutation: %s", query)
		}
	}
	for _, query := range []string{
		`UPDATE project_library_text_indexes SET status='unsupported' WHERE asset_id='retained'`,
		`DELETE FROM project_library_text_indexes WHERE asset_id='retained'`,
		`UPDATE project_library_text_lines SET content='changed' WHERE asset_id='retained'`,
		`DELETE FROM project_library_text_lines WHERE asset_id='retained'`,
		`INSERT INTO project_library_text_indexes VALUES('missing',1,1,'` + hash + `',1,'ready',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',3,1,'` + hash + `',1,'ready',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,2,'` + hash + `',1,'ready',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'bad-hash',1,'ready',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',1,'invented',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',1,'unsupported',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',-1,'ready',1,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',1,'ready',16385,'human',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',1,'ready',1,'missing',3)`,
		`INSERT INTO project_library_text_indexes VALUES('retained',2,1,'` + hash + `',1,'ready',1,'human',0)`,
		`INSERT INTO project_library_text_lines VALUES('retained',2,1,1,0,1,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,0,0,1,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,16385,0,1,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,2,-1,1,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,2,2,1,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,2,0,16385,'x','` + lineHash + `')`,
		`INSERT INTO project_library_text_lines VALUES('retained',1,1,2,0,1,'x','bad-hash')`,
	} {
		reject(query)
	}
	reject(`INSERT INTO project_library_text_lines VALUES('retained',1,1,2,0,16384,?,'`+lineHash+`')`, strings.Repeat("界", 5462))
	for _, test := range []struct {
		table   string
		columns []string
	}{
		{"project_library_text_indexes", []string{"asset_id", "version", "parser_version"}},
		{"project_library_text_lines", []string{"asset_id", "version", "parser_version", "line_number"}},
	} {
		var index string
		if err = db.SQL().QueryRow(`SELECT name FROM pragma_index_list(?) WHERE origin='pk' AND "unique"=1`, test.table).Scan(&index); err != nil {
			t.Fatal(err)
		}
		rows, err := db.SQL().Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{}
		for rows.Next() {
			var column string
			if err = rows.Scan(&column); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			columns = append(columns, column)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if !reflect.DeepEqual(columns, test.columns) {
			t.Fatalf("wrong persistent prefix for %s: %v", test.table, columns)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatalf("reopened idempotent upgrade: %v", err)
	}
	if got := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, got) {
		t.Fatal("close/reopen changed populated source records")
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM project_library_text_lines WHERE asset_id='retained' AND version=1 AND content=? AND line_hash=?`, text, lineHash).Scan(&lines); err != nil || lines != 1 {
		t.Fatalf("derived line lost on reopen %d %v", lines, err)
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("upgrade/reopen left foreign-key violations")
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if _, err = os.Stat(backups[0]); err != nil {
		t.Fatalf("rollback copy removed: %v", err)
	}
	afterBackups, err := filepath.Glob(fmt.Sprintf("%s.pre-migrate-v%04d-*.bak", path, latest))
	if err != nil || !reflect.DeepEqual(afterBackups, backups) {
		t.Fatalf("idempotent reopen made duplicate backup %v %v", afterBackups, err)
	}
}
