package sqlite

import (
	"context"
	"errors"
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
