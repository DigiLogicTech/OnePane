package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrationBackupRefusesWhenFreeSpaceIsShort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onepane.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL().Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO sample(value) VALUES('keep');`); err != nil {
		t.Fatal(err)
	}
	previous := availableBytesFn
	availableBytesFn = func(string) (uint64, error) { return 1, nil }
	t.Cleanup(func() { availableBytesFn = previous })

	backup, err := db.createMigrationBackup(context.Background(), 26)
	if err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("short volume created backup %q: %v", backup, err)
	}
	matches, globErr := filepath.Glob(path + ".pre-migrate-v*.bak")
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("rejected backup left files: %v", matches)
	}
	var value string
	if err := db.SQL().QueryRow(`SELECT value FROM sample`).Scan(&value); err != nil || value != "keep" {
		t.Fatalf("space check mutated the database: %q %v", value, err)
	}
}

func TestMigrationBackupsStayBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onepane.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL().Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO sample(value) VALUES('v');`); err != nil {
		t.Fatal(err)
	}
	previous := availableBytesFn
	availableBytesFn = func(string) (uint64, error) { return 1 << 40, nil }
	t.Cleanup(func() { availableBytesFn = previous })

	for i := 0; i < maxRetainedMigrationBackups+2; i++ {
		time.Sleep(2 * time.Millisecond)
		if _, err := db.SQL().Exec(`UPDATE sample SET value=?`, "v"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.createMigrationBackup(context.Background(), 26+i); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(path + ".pre-migrate-v*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != maxRetainedMigrationBackups {
		t.Fatalf("retained %d backups, want %d: %v", len(matches), maxRetainedMigrationBackups, matches)
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.Size() == 0 {
			t.Fatalf("retained backup is empty: %s %v", match, err)
		}
	}
}

func TestMigrationBackupSpaceProbeErrorIsNotASnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onepane.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL().Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	previous := availableBytesFn
	availableBytesFn = func(string) (uint64, error) { return 0, errors.New("statfs denied") }
	t.Cleanup(func() { availableBytesFn = previous })
	_, err = db.createMigrationBackup(context.Background(), 2)
	if err == nil || !strings.Contains(err.Error(), "statfs denied") {
		t.Fatalf("probe error was hidden: %v", err)
	}
}
