//go:build integration

package recoveryinventory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/migrations"
	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var recoverySchemaTemplate struct {
	sync.Once
	data []byte
	err  error
}

// Exported only by the integration test variant so the external black-box tests
// and internal contract tests share one immutable template without adding a
// production API. Cache only bytes from the real fully migrated, checkpointed and
// closed schema. Every caller owns a separate file, pool, transactions, rows
// and triggers. Migration/upgrade suites continue to run their own migrations.
func OpenMigratedRecoveryFixture(t *testing.T, ctx context.Context) *sqlitestore.DB {
	t.Helper()
	recoverySchemaTemplate.Do(func() {
		// Race instrumentation makes the SQLite driver's first process-wide
		// initialization slower than an ordinary connection. Initialize only
		// the disposable in-memory driver here, under the caller/test budget,
		// before production Open starts its unchanged five-second ping clock.
		// This fixture does not qualify real cold-start or installed acceptance.
		warm, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			recoverySchemaTemplate.err = err
			return
		}
		warm.SetMaxOpenConns(1)
		err = errors.Join(warm.PingContext(ctx), warm.Close())
		if err != nil {
			recoverySchemaTemplate.err = err
			return
		}
		templatePath := filepath.Join(t.TempDir(), "recovery-schema-template.db")
		template, err := sqlitestore.Open(templatePath)
		if err != nil {
			recoverySchemaTemplate.err = err
			return
		}
		err = template.Migrate(ctx)
		if err == nil {
			var busy, logFrames, checkpointed int
			err = template.SQL().QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
			if err == nil && (busy != 0 || logFrames != 0 || checkpointed != 0) {
				err = fmt.Errorf("incomplete recovery fixture checkpoint: busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointed)
			}
		}
		err = errors.Join(err, template.Close())
		if err == nil {
			recoverySchemaTemplate.data, err = os.ReadFile(templatePath)
		}
		// These exact files were created inside this test's private TempDir;
		// no recursive deletion or application/user data path is involved.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if removeErr := os.Remove(templatePath + suffix); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
		recoverySchemaTemplate.err = err
	})
	if recoverySchemaTemplate.err != nil {
		t.Fatal(recoverySchemaTemplate.err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	clonePath := filepath.Join(t.TempDir(), "recovery-contract.db")
	if err := os.WriteFile(clonePath, recoverySchemaTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(clonePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRecoverySchemaFixtureClonesKeepIsolationAndRealSchema(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("clone_%d", i), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			db := OpenMigratedRecoveryFixture(t, ctx)
			templateHash := sha256.Sum256(recoverySchemaTemplate.data)
			conn, err := db.SQL().Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var count int
			if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != len(files) {
				t.Fatalf("clone lacks embedded migrations: count=%d want=%d err=%v", count, len(files), err)
			}
			for _, pragma := range []struct {
				query string
				want  int
			}{{`PRAGMA foreign_keys`, 1}, {`PRAGMA synchronous`, 2}, {`PRAGMA busy_timeout`, 5000}} {
				var got int
				if err := conn.QueryRowContext(ctx, pragma.query).Scan(&got); err != nil || got != pragma.want {
					t.Fatalf("clone live configuration %s=%d want=%d err=%v", pragma.query, got, pragma.want, err)
				}
			}
			var mode string
			if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("clone lost live WAL: mode=%q err=%v", mode, err)
			}
			// All concurrent clones create the same keys and schema names, but
			// retain different values and independent rejected-write triggers.
			for _, statement := range []string{
				`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('recovery-clone','Private clone','active',1,1,1)`,
				`CREATE TABLE recovery_clone_marker(id TEXT PRIMARY KEY, owner INTEGER NOT NULL) STRICT`,
				fmt.Sprintf(`INSERT INTO recovery_clone_marker(id,owner) VALUES('private',%d)`, i),
				fmt.Sprintf(`CREATE TRIGGER reject_recovery_clone_update BEFORE UPDATE ON recovery_clone_marker BEGIN SELECT RAISE(ABORT,'private clone %d'); END`, i),
			} {
				if _, err := conn.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.ExecContext(ctx, `UPDATE recovery_clone_marker SET owner=99 WHERE id='private'`); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("private clone %d", i)) {
				t.Fatalf("clone-specific trigger missing or crossed databases: %v", err)
			}
			_, err = conn.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('recovery-clone','missing-principal','active',1,1)`)
			var constraint *modernsqlite.Error
			if !errors.As(err, &constraint) || constraint.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				t.Fatalf("real FK violation did not reject clone write: %v", err)
			}
			// Rejected writes do not poison the independent clone or erase its
			// original values. A read-only observation remains possible afterward.
			if _, err := conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
				t.Fatal(err)
			}
			var owner int
			if err := conn.QueryRowContext(ctx, `SELECT owner FROM recovery_clone_marker WHERE id='private'`).Scan(&owner); err != nil || owner != i {
				t.Fatalf("clone value changed after rejection: owner=%d want=%d err=%v", owner, i, err)
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO recovery_clone_marker(id,owner) VALUES('read-only-write',99)`); err == nil {
				t.Fatal("read-only clone accepted a write")
			}
			if _, err := conn.ExecContext(ctx, `PRAGMA query_only=OFF`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, `DROP TRIGGER reject_recovery_clone_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, `UPDATE recovery_clone_marker SET owner=owner+10 WHERE id='private'`); err != nil {
				t.Fatalf("clone did not recover after private failures: %v", err)
			}
			if err := conn.QueryRowContext(ctx, `SELECT owner FROM recovery_clone_marker WHERE id='private'`).Scan(&owner); err != nil || owner != i+10 {
				t.Fatalf("cross-clone state or schema mutation: owner=%d want=%d err=%v", owner, i+10, err)
			}
			if sha256.Sum256(recoverySchemaTemplate.data) != templateHash {
				t.Fatal("clone mutated shared immutable schema bytes")
			}
		})
	}
}
