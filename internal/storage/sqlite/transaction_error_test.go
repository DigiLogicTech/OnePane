package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func TestWithinPreservesFailureWhenRollbackAlreadyFinished(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		name := "callback_failure"
		if cancellation {
			name = "context_cancellation"
		}
		t.Run(name, func(t *testing.T) {
			db, err := Open(t.TempDir() + "/transaction.db")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.SQL().Exec(`CREATE TABLE prepared(value TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("callback rejected prepared write")
			if cancellation {
				cause = context.Canceled
			}
			err = db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
				if _, err := tx.ExecContext(ctx, `INSERT INTO prepared(value) VALUES('must roll back')`); err != nil {
					t.Fatal(err)
				}
				if cancellation {
					cancel()
				}
				// Make the database/sql cancellation race deterministic: its
				// automatic rollback can finish before Within calls Rollback.
				// Explicit closure produces the same already-done transaction
				// without sleeps or scheduling assumptions. Production callbacks
				// receive only storage.Tx, whose contract has no lifecycle method.
				if rollbackErr := tx.(*sql.Tx).Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
					t.Fatal(rollbackErr)
				}
				return cause
			})
			if !errors.Is(err, cause) {
				t.Fatalf("original callback cause lost: %v", err)
			}
			if !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("secondary rollback cause lost: %v", err)
			}
			var count int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM prepared`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("uncommitted write survived: %d %v", count, err)
			}
		})
	}
}

func TestWithinPreservesOrdinaryFailureAndCommitsOnlySuccess(t *testing.T) {
	db, err := Open(t.TempDir() + "/transaction.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.SQL().Exec(`CREATE TABLE prepared(value INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("reject write")
	err = db.Within(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prepared(value) VALUES(1)`); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) || errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("ordinary rollback cause changed: %v", err)
	}
	if err = db.Within(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO prepared(value) VALUES(2)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var value int
	if err = db.SQL().QueryRow(`SELECT value FROM prepared`).Scan(&value); err != nil || value != 2 {
		t.Fatalf("wrong durable result %d %v", value, err)
	}
}
