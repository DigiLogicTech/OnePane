package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/migrations"
)

type migration struct {
	version  int
	name     string
	filename string
	body     []byte
	checksum string
}

// MigrationError reports a failed schema upgrade together with the durable
// rollback snapshot created immediately before pending migrations were applied.
type MigrationError struct {
	Cause         error
	BackupPath    string
	TargetVersion int
}

func (e *MigrationError) Error() string {
	if e == nil {
		return "migration failed"
	}
	return fmt.Sprintf("migration to schema v%04d failed: %v; pre-migration backup preserved at %s", e.TargetVersion, e.Cause, e.BackupPath)
}

func (e *MigrationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func migrationFailure(cause error, backupPath string, targetVersion int) error {
	if strings.TrimSpace(backupPath) == "" {
		return cause
	}
	return &MigrationError{Cause: cause, BackupPath: backupPath, TargetVersion: targetVersion}
}

func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at INTEGER NOT NULL
) STRICT;`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	items, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := d.appliedMigrationVersions(ctx)
	if err != nil {
		return err
	}
	pending := make([]migration, 0, len(items))
	for _, item := range items {
		if _, ok := applied[item.version]; !ok {
			pending = append(pending, item)
		}
	}
	backupPath := ""
	targetVersion := 0
	if len(pending) > 0 {
		targetVersion = pending[len(pending)-1].version
	}
	if len(applied) > 0 && len(pending) > 0 {
		backupPath, err = d.createMigrationBackup(ctx, targetVersion)
		if err != nil {
			return fmt.Errorf("create pre-migration backup: %w", err)
		}
	}
	for _, item := range items {
		if err := d.applyMigration(ctx, item); err != nil {
			return migrationFailure(err, backupPath, targetVersion)
		}
	}
	if len(pending) > 0 {
		var check string
		if err := d.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil || !strings.EqualFold(strings.TrimSpace(check), "ok") {
			if err == nil {
				err = fmt.Errorf("quick_check returned %q", check)
			}
			return migrationFailure(fmt.Errorf("post-migration validation failed: %w", err), backupPath, targetVersion)
		}
	}
	return nil
}

func (d *DB) appliedMigrationVersions(ctx context.Context) (map[int]struct{}, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()
	out := map[int]struct{}{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out[version] = struct{}{}
	}
	return out, rows.Err()
}

func (d *DB) createMigrationBackup(ctx context.Context, targetVersion int) (string, error) {
	rows, err := d.db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var mainPath string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			mainPath = file
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(mainPath) == "" {
		return "", errors.New("main sqlite database has no durable path")
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backup := fmt.Sprintf("%s.pre-migrate-v%04d-%s.bak", mainPath, targetVersion, stamp)
	escaped := strings.ReplaceAll(backup, "'", "''")
	if _, err := d.db.ExecContext(ctx, "VACUUM INTO '"+escaped+"'"); err != nil {
		return "", err
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		_ = os.Remove(backup)
		return "", err
	}
	return backup, nil
}

// RestoreMigrationBackup atomically replaces a failed upgraded database with
// the pre-migration snapshot. The caller must close every connection to the
// target database before invoking this function.
func RestoreMigrationBackup(targetPath, backupPath string) error {
	targetAbs, err := filepath.Abs(strings.TrimSpace(targetPath))
	if err != nil {
		return fmt.Errorf("resolve migration rollback target: %w", err)
	}
	backupAbs, err := filepath.Abs(strings.TrimSpace(backupPath))
	if err != nil {
		return fmt.Errorf("resolve migration rollback backup: %w", err)
	}
	if targetAbs == backupAbs {
		return errors.New("migration rollback target and backup are the same file")
	}
	info, err := os.Stat(backupAbs)
	if err != nil {
		return fmt.Errorf("stat migration rollback backup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("migration rollback backup is not a regular file")
	}
	if err := verifyDatabaseFile(backupAbs); err != nil {
		return fmt.Errorf("pre-migration backup failed integrity validation: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o700); err != nil {
		return err
	}

	in, err := os.Open(backupAbs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetAbs), "."+filepath.Base(targetAbs)+".restore-*")
	if err != nil {
		_ = in.Close()
		return err
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		_ = in.Close()
		_ = tmp.Close()
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		return fmt.Errorf("copy migration rollback snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}

	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	failedPath := targetAbs + ".failed-migrate-" + stamp
	targetExisted := false
	if _, err := os.Stat(targetAbs); err == nil {
		if err := os.Rename(targetAbs, failedPath); err != nil {
			return fmt.Errorf("preserve failed migrated database: %w", err)
		}
		targetExisted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	type movedSidecar struct{ original, failed string }
	var sidecars []movedSidecar
	for _, suffix := range []string{"-wal", "-shm"} {
		original := targetAbs + suffix
		failed := failedPath + suffix
		if _, err := os.Stat(original); err == nil {
			if err := os.Rename(original, failed); err != nil {
				if targetExisted {
					_ = os.Rename(failedPath, targetAbs)
				}
				return fmt.Errorf("preserve failed migration sidecar %s: %w", suffix, err)
			}
			sidecars = append(sidecars, movedSidecar{original: original, failed: failed})
		} else if !errors.Is(err, os.ErrNotExist) {
			if targetExisted {
				_ = os.Rename(failedPath, targetAbs)
			}
			return err
		}
	}

	rollbackOriginal := func() {
		_ = os.Remove(targetAbs)
		if targetExisted {
			_ = os.Rename(failedPath, targetAbs)
		}
		for _, s := range sidecars {
			_ = os.Rename(s.failed, s.original)
		}
	}
	if err := os.Rename(tmpPath, targetAbs); err != nil {
		rollbackOriginal()
		return fmt.Errorf("activate migration rollback snapshot: %w", err)
	}
	cleanupTmp = false
	if err := os.Chmod(targetAbs, 0o600); err != nil {
		rollbackOriginal()
		return err
	}
	if err := verifyDatabaseFile(targetAbs); err != nil {
		rollbackOriginal()
		return fmt.Errorf("restored database failed integrity validation: %w", err)
	}

	if targetExisted {
		_ = os.Remove(failedPath)
	}
	for _, s := range sidecars {
		_ = os.Remove(s.failed)
	}
	return nil
}

func verifyDatabaseFile(path string) error {
	db, err := Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err := db.SQL().QueryRow("PRAGMA quick_check").Scan(&check); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(check), "ok") {
		return fmt.Errorf("quick_check returned %q", check)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var out []migration
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(e.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", e.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", e.Name(), err)
		}
		body, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			version:  version,
			name:     parts[1],
			filename: e.Name(),
			body:     body,
			checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i-1].version == out[i].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

func (d *DB) applyMigration(ctx context.Context, m migration) error {
	var storedChecksum string
	err := d.db.QueryRowContext(ctx,
		`SELECT checksum FROM schema_migrations WHERE version = ?`, m.version,
	).Scan(&storedChecksum)

	if err == nil {
		if storedChecksum != m.checksum {
			return fmt.Errorf("migration %s checksum mismatch", m.filename)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect migration %s: %w", m.filename, err)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", m.filename, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(m.body)); err != nil {
		return fmt.Errorf("execute migration %s: %w", m.filename, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,?,?,?)`,
		m.version, m.name, m.checksum, time.Now().UTC().UnixMilli(),
	); err != nil {
		return fmt.Errorf("record migration %s: %w", m.filename, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.filename, err)
	}
	return nil
}
