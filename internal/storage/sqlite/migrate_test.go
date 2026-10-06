package sqlite

import (
	"context"
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
