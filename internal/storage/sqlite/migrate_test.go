package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateMigrationBackupProducesConsistentCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onepane.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.SQL().Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO sample(value) VALUES('before-migration');`); err != nil {
		t.Fatal(err)
	}
	backup, err := db.createMigrationBackup(context.Background(), 26)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("backup permissions too broad: %o", info.Mode().Perm())
	}
	copyDB, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var value string
	if err := copyDB.SQL().QueryRow(`SELECT value FROM sample LIMIT 1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before-migration" {
		t.Fatalf("backup value=%q", value)
	}
}

func TestAlpha31SchemaUpgradesToAlpha32WithoutLosingDurableData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "onepane-alpha31.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Recreate the schema state shipped by Alpha 3.1: migrations 0001..0025.
	if _, err := db.SQL().Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT;`); err != nil {
		t.Fatal(err)
	}
	items, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.version > 25 {
			continue
		}
		if err := db.applyMigration(ctx, item); err != nil {
			t.Fatalf("apply Alpha 3.1 migration %04d: %v", item.version, err)
		}
	}

	const workspaceID = "ws-alpha31-upgrade"
	if _, err := db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?, 'active',1,?,?)`, workspaceID, "Preserved Alpha 3.1 workspace", int64(1), int64(1)); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var name string
	if err := db.SQL().QueryRow(`SELECT name FROM workspaces WHERE id=?`, workspaceID).Scan(&name); err != nil {
		t.Fatalf("durable Alpha 3.1 workspace lost during upgrade: %v", err)
	}
	if name != "Preserved Alpha 3.1 workspace" {
		t.Fatalf("workspace mutated during upgrade: %q", name)
	}
	var applied int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=26`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("Alpha 3.2 migration not recorded: applied=%d err=%v", applied, err)
	}
	var manifestTable string
	if err := db.SQL().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='team_session_manifests'`).Scan(&manifestTable); err != nil {
		t.Fatalf("Alpha 3.2 research manifest schema missing: %v", err)
	}
	backups, err := filepath.Glob(path + ".pre-migrate-v*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one pre-migration rollback point, got %d: %v", len(backups), backups)
	}
	copyDB, err := Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if err := copyDB.SQL().QueryRow(`SELECT name FROM workspaces WHERE id=?`, workspaceID).Scan(&name); err != nil {
		t.Fatalf("rollback copy does not contain Alpha 3.1 workspace: %v", err)
	}
}
func TestRestoreMigrationBackupRestoresPreUpgradeState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "onepane.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO sample(value) VALUES('alpha31');`); err != nil {
		t.Fatal(err)
	}
	backup, err := db.createMigrationBackup(ctx, 26)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`UPDATE sample SET value='alpha32-mutated'; CREATE TABLE upgrade_only(id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate stale sidecars left by a failed upgraded process. The restore
	// path must not let them attach to the pre-migration snapshot.
	if err := os.WriteFile(path+"-wal", []byte("stale-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-shm", []byte("stale-shm"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RestoreMigrationBackup(path, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("rollback snapshot was not preserved: %v", err)
	}

	restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := restored.SQL().QueryRow(`SELECT value FROM sample LIMIT 1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "alpha31" {
		t.Fatalf("restored value=%q", value)
	}
	var upgradeOnly int
	if err := restored.SQL().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='upgrade_only'`).Scan(&upgradeOnly); err != nil {
		t.Fatal(err)
	}
	if upgradeOnly != 0 {
		t.Fatal("upgrade-only schema survived rollback")
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path+"-wal"); err == nil || !os.IsNotExist(err) {
		t.Fatalf("stale WAL survived rollback: %v", err)
	}
	if _, err := os.Stat(path+"-shm"); err == nil || !os.IsNotExist(err) {
		t.Fatalf("stale SHM survived rollback: %v", err)
	}
	failedCopies, err := filepath.Glob(path + ".failed-migrate-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(failedCopies) != 0 {
		t.Fatalf("temporary failed migration copies were not cleaned up: %v", failedCopies)
	}
}

func TestMigrationFailureExposesRollbackMetadata(t *testing.T) {
	cause := errors.New("synthetic migration failure")
	err := migrationFailure(cause, "/tmp/onepane.rollback.bak", 26)
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) {
		t.Fatalf("expected MigrationError, got %T", err)
	}
	if migrationErr.BackupPath != "/tmp/onepane.rollback.bak" || migrationErr.TargetVersion != 26 {
		t.Fatalf("unexpected migration metadata: %+v", migrationErr)
	}
	if !errors.Is(err, cause) {
		t.Fatal("migration error did not preserve original cause")
	}
}

func TestWorkspaceRuntimeV37UpgradePreservesLiveLegacyReferences(t *testing.T) {
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"pre-workspace-runtime.sqlite")
 db,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer db.Close()
 _,err=db.SQL().ExecContext(ctx,`CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL
 ) STRICT`)
 if err!=nil{t.Fatal(err)}
 migrations,err:=loadMigrations();if err!=nil{t.Fatal(err)}
 for _,m:=range migrations {
  if m.version>36{continue}
  if err=db.applyMigration(ctx,m);err!=nil{t.Fatalf("prepare v36 schema %d: %v",m.version,err)}
 }
 now:=int64(1711111111111)
 seed:=[]struct{query string;args []any}{
  {`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Old tenant','active',1,?,?)`,[]any{now,now}},
  {`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`,[]any{now,now}},
  {`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at)
 VALUES('legacy-project','tenant','Old Project','active','{}','{}',1,'admin',?,?)`,[]any{now,now}},
  {`INSERT INTO project_runtimes(
 id,project_id,isolation_mode,backend,desired_state,status,
 runtime_spec_json,resource_limits_json,network_policy_json,filesystem_policy_json,environment_bindings_json,
 revision,created_by,created_at,updated_at)
 VALUES('old-runtime','legacy-project','sandboxed_container','sandbox_runner','stopped','defined',
 '{}','{}','{}','{}','{}',1,'admin',?,?)`,[]any{now,now}},
  {`INSERT INTO project_applications(
 id,project_runtime_id,name,source_kind,source_ref,install_spec_json,runtime_spec_json,
 environment_bindings_json,desired_state,status,trust,revision,created_by,created_at,updated_at)
 VALUES('old-app','old-runtime','Old tool','oci_image','ghcr.io/legacy/tool@sha256:abc',
 '{}','{}','{}','installed','installed','untrusted_content',1,'admin',?,?)`,[]any{now,now}},
 }
 for i,q:=range seed{if _,err=db.SQL().ExecContext(ctx,q.query,q.args...);err!=nil{t.Fatalf("legacy fixture %d: %v",i,err)}}
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 var project,workspace sql.NullString
 err=db.SQL().QueryRowContext(ctx,`SELECT project_id,project_workspace_id FROM project_runtimes WHERE id='old-runtime'`).Scan(&project,&workspace)
 if err!=nil||!project.Valid||project.String!="legacy-project"||workspace.Valid{t.Fatalf("legacy runtime identity changed: %v %v %v",err,project,workspace)}
 var appRuntime string
 if err=db.SQL().QueryRowContext(ctx,`SELECT project_runtime_id FROM project_applications WHERE id='old-app'`).Scan(&appRuntime);err!=nil||appRuntime!="old-runtime"{t.Fatalf("legacy app foreign key changed: %v %s",err,appRuntime)}
 rows,err:=db.SQL().QueryContext(ctx,"PRAGMA foreign_key_check");if err!=nil{t.Fatal(err)}
 if rows.Next(){rows.Close();t.Fatal("foreign key violated by v0037 migration")}
 if err=rows.Err();err!=nil{rows.Close();t.Fatal(err)};rows.Close()
 var integrity string
 if err=db.SQL().QueryRowContext(ctx,"PRAGMA quick_check").Scan(&integrity);err!=nil||integrity!="ok"{t.Fatalf("migration integrity: %v %s",err,integrity)}
 // The backup filename identifies the final target schema version (now 0038),
 // not the first pending migration. Keep this preservation test valid as new
 // additive migrations follow the v0037 referenced-parent rebuild.
 latest:=36
 for _,migration:=range migrations{if migration.version>latest{latest=migration.version}}
 backupPattern:=fmt.Sprintf("%s.pre-migrate-v%04d-*.bak",path,latest)
 backupPaths,err:=filepath.Glob(backupPattern)
 if err!=nil||len(backupPaths)!=1{t.Fatalf("pre-migration backup not retained for target v%04d: %v %v",latest,err,backupPaths)}
 backup,err:=Open(backupPaths[0]);if err!=nil{t.Fatal(err)}
 defer backup.Close()
 // The pre-upgrade backup must remain at v0036 and include the preserved
 // legacy records, even when the final target schema is later than v0037.
 var previousVersion int
 if err=backup.SQL().QueryRowContext(ctx,"SELECT MAX(version) FROM schema_migrations").Scan(&previousVersion);err!=nil||previousVersion!=36{
  t.Fatalf("rollback snapshot is not the untouched v0036 database: %v v%d",err,previousVersion)
 }
 // The untouched rollback copy must contain the exact original runtime IDs.
 var backupApp string
 if err=backup.SQL().QueryRowContext(ctx,`SELECT project_runtime_id FROM project_applications WHERE id='old-app'`).Scan(&backupApp);err!=nil||backupApp!="old-runtime"{t.Fatalf("rollback missing prior application: %v %s",err,backupApp)}
}
