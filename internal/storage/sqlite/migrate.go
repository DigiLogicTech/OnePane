package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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
	if len(applied) > 0 && len(pending) > 0 {
		backupPath, err = d.createMigrationBackup(ctx, pending[len(pending)-1].version)
		if err != nil {
			return fmt.Errorf("create pre-migration backup: %w", err)
		}
	}
	for _, item := range items {
		if err := d.applyMigration(ctx, item); err != nil {
			if backupPath != "" {
				return fmt.Errorf("%w; pre-migration backup preserved at %s", err, backupPath)
			}
			return err
		}
	}
	if len(pending) > 0 {
		var check string
		if err := d.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil || !strings.EqualFold(strings.TrimSpace(check), "ok") {
			if err == nil {
				err = fmt.Errorf("quick_check returned %q", check)
			}
			if backupPath != "" {
				return fmt.Errorf("post-migration validation failed: %w; pre-migration backup preserved at %s", err, backupPath)
			}
			return fmt.Errorf("post-migration validation failed: %w", err)
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
