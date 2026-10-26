//go:build integration

package projectworkspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

var libraryTextSchemaTemplate struct {
	sync.Once
	data []byte
	err  error
}

// Migrate/checkpoint/close the real schema once, retaining only immutable file
// bytes. Each case still opens a separate real database/pool/transaction/file;
// mutable state and fault-injection triggers are never shared between cases.
// Upgrade tests continue to run actual historical migrations independently.
func libraryTextFixtureDatabase(t *testing.T) *sqlitestore.DB {
	t.Helper()
	libraryTextSchemaTemplate.Do(func() {
		template, err := sqlitestore.Open(filepath.Join(t.TempDir(), "text-schema-template.db"))
		if err != nil {
			libraryTextSchemaTemplate.err = err
			return
		}
		err = template.Migrate(context.Background())
		if err == nil {
			var busy, logFrames, checkpointed int
			err = template.SQL().QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
			if err == nil && (busy != 0 || logFrames != 0 || checkpointed != 0) {
				err = fmt.Errorf("incomplete text schema checkpoint: %d/%d/%d", busy, logFrames, checkpointed)
			}
		}
		var path string
		if err == nil {
			err = template.SQL().QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path)
		}
		err = errors.Join(err, template.Close())
		if err == nil {
			libraryTextSchemaTemplate.data, err = os.ReadFile(path)
		}
		libraryTextSchemaTemplate.err = err
	})
	if libraryTextSchemaTemplate.err != nil {
		t.Fatal(libraryTextSchemaTemplate.err)
	}
	path := filepath.Join(t.TempDir(), "text-case.db")
	if err := os.WriteFile(path, libraryTextSchemaTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
