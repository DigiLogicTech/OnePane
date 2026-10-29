package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"

	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestIsBusyRecognizesBusySnapshot(t *testing.T) {
	db, err := Open(t.TempDir() + "/busy.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SQL().Exec(`CREATE TABLE snapshot (value INTEGER); INSERT INTO snapshot VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Production write transactions are BEGIN IMMEDIATE. This case pins a
	// deferred snapshot on purpose, which is the upgrade that returns 517.
	conn, err := db.SQL().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN DEFERRED`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, `ROLLBACK`)
	reader := conn
	var value int
	if err := reader.QueryRowContext(ctx, `SELECT value FROM snapshot`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	// Commit on a second connection after the reader pins its WAL snapshot.
	// Upgrading that stale reader to a writer deterministically returns 517.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE snapshot SET value=2`); err != nil {
		t.Fatal(err)
	}
	_, err = reader.ExecContext(ctx, `UPDATE snapshot SET value=3`)
	var sqliteErr *modernsqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqlite3.SQLITE_BUSY_SNAPSHOT {
		t.Fatalf("expected SQLITE_BUSY_SNAPSHOT, got %v", err)
	}
	if !IsBusy(err) || !IsBusy(fmt.Errorf("transaction failed: %w", err)) {
		t.Fatalf("busy snapshot must be recognized directly and wrapped: %v", err)
	}
	if _, err := reader.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT value FROM snapshot`).Scan(&value); err != nil || value != 2 {
		t.Fatalf("failed snapshot writer changed committed data: value=%d err=%v", value, err)
	}
}

func TestIsBusyRejectsMisleadingText(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("SQLITE_BUSY"),
		fmt.Errorf("transaction failed: %w", errors.New("database is locked (517)")),
	} {
		if IsBusy(err) {
			t.Errorf("non-driver error classified as busy: %v", err)
		}
	}
}
