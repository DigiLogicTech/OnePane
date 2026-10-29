package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLibraryDocumentRevisionMigrationPreservesV1AndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "document-upgrade.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	var upgrade migration
	latest := 116
	for _, m := range items {
		if m.version > latest {
			latest = m.version
		}
		if m.version < 117 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
		if m.version == 117 {
			upgrade = m
		}
	}
	if upgrade.filename != "0117_library_document_parser_revision.sql" {
		t.Fatal("missing reviewed document revision migration")
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('tenant','Retained','active',1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('human','human','Owner','active',1,1)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Retained','active','{"cloud_fallback":false}','{"enabled":false}','human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('source','p','Retained',1,1)`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES('document','p','Existing.docx','application/octet-stream',2,1,1)`,
		`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,provenance_json,created_at) VALUES('document',1,'sha256:'||printf('%064x',1),100,'application/octet-stream','artifact:retained-1','{"retained":true}',1),('document',2,'sha256:'||printf('%064x',2),101,'application/octet-stream','artifact:retained-2','{"retained":true}',2)`,
		`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,permissions_json,created_at,updated_at) VALUES('read','source','document',1,'latest','{"read":true,"execute":false}',1,1)`,
		`INSERT INTO project_library_document_indexes VALUES('document',1,'docx-wordml-paragraphs-v1','sha256:'||printf('%064x',1),100,'sha256:'||printf('%064x',3),'sha256:'||printf('%064x',4),8,'ready','Retained v1 coverage','[{"part_name":"word/document.xml","source_sha":"retained","paragraph_count":1}]',1,'human',1)`,
		`INSERT INTO project_library_document_units VALUES('document',1,'docx-wordml-paragraphs-v1',1,'word/document.xml',1,'retained','sha256:'||printf('%064x',5))`,
		`INSERT INTO project_library_document_indexes VALUES('document',2,'docx-wordml-paragraphs-v1','sha256:'||printf('%064x',2),101,'','sha256:'||printf('%064x',6),0,'invalid_document','','[]',0,'human',2)`,
	} {
		exec(query)
	}
	tables := []string{"projects", "project_workspaces", "project_library_assets", "project_library_asset_versions", "workspace_library_grants", "project_library_document_indexes", "project_library_document_units"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	var oldDDL string
	if err = db.SQL().QueryRow(`SELECT sql FROM sqlite_schema WHERE name='project_library_document_indexes'`).Scan(&oldDDL); err != nil {
		t.Fatal(err)
	}
	// Fail after every real copy/drop/rename/recreated trigger has executed, using
	// the production transaction owner. Original receipts and DDL must return.
	failed := upgrade
	failed.body = append(append([]byte(nil), upgrade.body...), []byte("\nSELECT deliberate_missing_upgrade_function();")...)
	if err = db.applyMigration(ctx, failed); err == nil {
		t.Fatal("deliberately invalid migration succeeded")
	}
	var rolledBackDDL string
	if err = db.SQL().QueryRow(`SELECT sql FROM sqlite_schema WHERE name='project_library_document_indexes'`).Scan(&rolledBackDDL); err != nil || rolledBackDDL != oldDDL || !reflect.DeepEqual(before, recoveryProvenanceRows(t, db.SQL(), tables)) {
		t.Fatal("failed revision upgrade changed historical data/schema", err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, recoveryProvenanceRows(t, db.SQL(), tables)) {
		t.Fatal("revision upgrade changed v1 receipts, units, grants or source/configuration")
	}
	backups, err := filepath.Glob(fmt.Sprintf("%s.pre-migrate-v%04d-*.bak", path, latest))
	if err != nil || len(backups) != 1 {
		t.Fatal("missing unique pre-revision recovery snapshot", backups, err)
	}
	backup, err := Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if !reflect.DeepEqual(before, recoveryProvenanceRows(t, backup.SQL(), tables)) {
		t.Fatal("pre-revision recovery snapshot lost v1 source/configuration/history")
	}
	var backupDDL, integrity string
	if err = backup.SQL().QueryRow(`SELECT sql FROM sqlite_schema WHERE name='project_library_document_indexes'`).Scan(&backupDDL); err != nil || backupDDL != oldDDL {
		t.Fatal("pre-revision recovery snapshot contains new parser semantics", err)
	}
	if err = backup.SQL().QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("pre-revision recovery snapshot is inconsistent", integrity, err)
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	hasViolation := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil || hasViolation {
		t.Fatal("revision upgrade broke retained foreign keys", err)
	}
	// A v2 receipt can coexist at exactly the same original asset/version; no
	// old status or parser identity is rewritten to pretend it used v2.
	exec(`INSERT INTO project_library_document_indexes SELECT asset_id,version,'docx-wordml-paragraphs-v2',raw_source_sha,raw_source_size,derived_sha,manifest_sha,derived_utf8_bytes,status,coverage,parts_json,unit_count,created_by,created_at FROM project_library_document_indexes WHERE version=1`)
	exec(`INSERT INTO project_library_document_units SELECT asset_id,version,'docx-wordml-paragraphs-v2',ordinal,part_name,paragraph_number,content,text_hash FROM project_library_document_units`)
	for _, query := range []string{
		`UPDATE project_library_document_indexes SET created_at=9`,
		`DELETE FROM project_library_document_indexes`,
		`UPDATE project_library_document_units SET content='changed'`,
		`DELETE FROM project_library_document_units`,
	} {
		if _, err = db.SQL().Exec(query); err == nil || (!strings.Contains(err.Error(), "immutable") && !strings.Contains(err.Error(), "retained")) {
			t.Fatalf("revision upgrade lost immutable retention: %v", err)
		}
	}
	if _, err = db.SQL().Exec(`INSERT INTO project_library_document_indexes SELECT asset_id,version,'unreviewed-parser',raw_source_sha,raw_source_size,derived_sha,manifest_sha,derived_utf8_bytes,status,coverage,parts_json,unit_count,created_by,created_at FROM project_library_document_indexes WHERE version=2`); err == nil {
		t.Fatal("unreviewed parser revision accepted")
	}
}
