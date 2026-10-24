package teamworker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

var workerDatabaseTemplate struct {
	sync.Once
	data []byte
	err  error
}

func migratedWorkerDatabase(t *testing.T) *sqlitestore.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team.db")
	// Migrate the real schema once and clone only after its WAL is checkpointed
	// and all connections close. Each test still owns an isolated durable DB.
	workerDatabaseTemplate.Do(func() {
		templatePath := path + ".template"
		template, err := sqlitestore.Open(templatePath)
		if err != nil {
			workerDatabaseTemplate.err = err
			return
		}
		err = template.Migrate(context.Background())
		if err == nil {
			_, err = template.SQL().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
		closeErr := template.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			workerDatabaseTemplate.data, err = os.ReadFile(templatePath)
		}
		workerDatabaseTemplate.err = err
	})
	if workerDatabaseTemplate.err != nil {
		t.Fatal(workerDatabaseTemplate.err)
	}
	if err := os.WriteFile(path, workerDatabaseTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
