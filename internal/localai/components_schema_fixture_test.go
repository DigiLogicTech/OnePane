package localai

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/migrations"
)

var componentSchemaTemplate struct {
	sync.Once
	data []byte
	err  error
}

// Only bytes from the real migrated, checkpointed and closed database are shared.
// Every component test retains a private durable file, pool, rows and triggers.
// Migration/upgrade acceptance continues to exercise migrations independently.
func componentSchemaDatabase(t *testing.T, ctx context.Context) *sqliteStore.DB {
	t.Helper()
	componentSchemaTemplate.Do(func() {
		file := filepath.Join(t.TempDir(), "component-schema.db")
		db, err := sqliteStore.Open(file)
		if err != nil {
			componentSchemaTemplate.err = err
			return
		}
		err = db.Migrate(ctx)
		if err == nil {
			var busy, frames, saved int
			err = db.SQL().QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &frames, &saved)
			if err == nil && (busy != 0 || frames != 0 || saved != 0) {
				err = errors.New("incomplete component schema checkpoint")
			}
		}
		err = errors.Join(err, db.Close())
		if err == nil {
			componentSchemaTemplate.data, err = os.ReadFile(file)
		}
		componentSchemaTemplate.err = err
	})
	if componentSchemaTemplate.err != nil {
		t.Fatal(componentSchemaTemplate.err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "component.db")
	if err := os.WriteFile(file, componentSchemaTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqliteStore.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestManagedComponentSchemaClonesAreIndependent(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := componentSchemaDatabase(t, ctx)
			hash := sha256.Sum256(componentSchemaTemplate.data)
			conn, err := db.SQL().Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var count, foreignKeys int
			var mode string
			if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != len(files) {
				t.Fatalf("actual migrations absent: %d %v", count, err)
			}
			if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("foreign keys disabled: %d %v", foreignKeys, err)
			}
			if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("WAL absent: %s %v", mode, err)
			}
			for _, statement := range []string{
				`CREATE TABLE private_component_fixture(owner INTEGER NOT NULL) STRICT`,
				fmt.Sprintf(`INSERT INTO private_component_fixture(owner) VALUES(%d)`, i),
				`CREATE TRIGGER private_component_guard BEFORE UPDATE ON private_component_fixture BEGIN SELECT RAISE(ABORT,'private rejected'); END`,
			} {
				if _, err := conn.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.ExecContext(ctx, `UPDATE private_component_fixture SET owner=99`); err == nil {
				t.Fatal("private trigger did not reject write")
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,created_at,updated_at) VALUES('bad','foreign','install','queued','queued',1,1)`); err == nil {
				t.Fatal("clone accepted missing component FK")
			}
			if _, err := conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO private_component_fixture(owner) VALUES(99)`); err == nil {
				t.Fatal("read-only clone accepted write")
			}
			if err := conn.QueryRowContext(ctx, `SELECT owner FROM private_component_fixture`).Scan(&count); err != nil || count != i {
				t.Fatalf("clone isolation lost: %d %v", count, err)
			}
			if sha256.Sum256(componentSchemaTemplate.data) != hash {
				t.Fatal("private clone changed shared template bytes")
			}
		})
	}
}
