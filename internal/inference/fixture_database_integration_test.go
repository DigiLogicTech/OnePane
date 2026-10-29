//go:build integration

package inference

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/migrations"
)

var serviceInferenceDatabaseTemplate struct {
	sync.Once
	data []byte
	err  error
}

// Migrate the actual schema once per test process. Cache only the bytes of a
// checkpointed, closed database: every caller gets its own file and connections,
// so rejected-write triggers, transactions and fixture mutations remain isolated.
func migratedInferenceDispatchDatabase(t *testing.T, ctx context.Context) *sqlite.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inference-dispatch.db")
	serviceInferenceDatabaseTemplate.Do(func() {
		templatePath := path + ".template"
		template, err := sqlite.Open(templatePath)
		if err != nil {
			serviceInferenceDatabaseTemplate.err = err
			return
		}
		err = template.Migrate(ctx)
		if err == nil {
			var busy, logFrames, checkpointed int
			err = template.SQL().QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
			if err == nil && (busy != 0 || logFrames != 0 || checkpointed != 0) {
				err = fmt.Errorf("incomplete fixture checkpoint: busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointed)
			}
		}
		closeErr := template.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			serviceInferenceDatabaseTemplate.data, err = os.ReadFile(templatePath)
		}
		serviceInferenceDatabaseTemplate.err = err
	})
	if serviceInferenceDatabaseTemplate.err != nil {
		t.Fatal(serviceInferenceDatabaseTemplate.err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, serviceInferenceDatabaseTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInferencePreDispatchDatabaseClonesKeepIsolationAndRealSchema(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprintf("clone_%d", i), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			db := migratedInferenceDispatchDatabase(t, ctx)
			db.SQL().SetMaxOpenConns(1)
			var count, foreignKeys int
			if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != len(files) {
				t.Fatalf("clone lacks full migrated schema: got=%d want=%d err=%v", count, len(files), err)
			}
			if err := db.SQL().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("clone lost FK enforcement: enabled=%d err=%v", foreignKeys, err)
			}
			// Identical keys and schema mutations succeed independently even when
			// the other clones retain open connections and rejected-write triggers.
			for _, statement := range []string{
				`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('isolated-clone','Clone','active',1,1,1)`,
				`CREATE TABLE isolated_clone_marker(id TEXT PRIMARY KEY) STRICT`,
				`INSERT INTO isolated_clone_marker(id) VALUES('private')`,
				`CREATE TRIGGER reject_clone_update BEFORE UPDATE ON workspaces BEGIN SELECT RAISE(ABORT,'isolated rejection'); END`,
			} {
				if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.SQL().ExecContext(ctx, `UPDATE workspaces SET name='forbidden' WHERE id='isolated-clone'`); err == nil {
				t.Fatal("clone-specific rejected-write trigger missing")
			}
			if _, err := db.SQL().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) VALUES('foreign','missing','must reject','created','{}',1,1)`); err == nil {
				t.Fatal("clone accepted foreign Task despite real schema")
			}
		})
	}
}
