package team

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

var teamDatabaseTemplate struct {
	sync.Once
	data []byte
	err  error
}

func migratedTeamDatabase(t *testing.T) *sqlitestore.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team.db")
	// Migrate the real schema once and clone only after its WAL is checkpointed
	// and all connections close. Each test still owns an isolated durable DB.
	teamDatabaseTemplate.Do(func() {
		templatePath := path + ".template"
		template, err := sqlitestore.Open(templatePath)
		if err != nil {
			teamDatabaseTemplate.err = err
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
			teamDatabaseTemplate.data, err = os.ReadFile(templatePath)
		}
		teamDatabaseTemplate.err = err
	})
	if teamDatabaseTemplate.err != nil {
		t.Fatal(teamDatabaseTemplate.err)
	}
	if err := os.WriteFile(path, teamDatabaseTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
