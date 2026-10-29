package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const transactionBusyAttempts = 5

type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}

	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	// WAL + NORMAL is crash-consistent for process death. A power loss can
	// roll back the last committed transactions; it does not corrupt the
	// file. FULL fsyncs every commit, which kept idle heartbeats waking a
	// weak laptop disk. Unknown in-flight work is still reconciled on restart.
	q.Add("_pragma", "synchronous(NORMAL)")
	// Deferred BEGIN upgrades to a write after another commit and returns
	// SQLITE_BUSY_SNAPSHOT, which the busy handler does not retry. Immediate
	// transactions take the write reservation at BEGIN, so contention waits
	// on busy_timeout instead of failing the upgrade.
	q.Add("_txlock", "immediate")

	dsn := "file:" + filepath.ToSlash(abs) + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite is the serialized durable state boundary for v0.1. Keeping the
	// pool deliberately small reduces accidental writer contention while WAL
	// still permits concurrent readers.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &DB{db: db}, nil
}

func (d *DB) SQL() *sql.DB { return d.db }

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	var err error
	for attempt := 1; attempt <= transactionBusyAttempts; attempt++ {
		err = d.withinOnce(ctx, fn)
		// Retry only when BEGIN itself could not reserve the write lock.
		// A busy error from inside fn is returned as-is: the callback may
		// already have performed a non-database effect, so it is not replayed.
		if err == nil || !beginWasBusy(err) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
		}
	}
	return err
}

func beginWasBusy(err error) bool {
	return err != nil && IsBusy(err) && strings.Contains(err.Error(), "begin transaction:")
}

func (d *DB) withinOnce(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil && IsReadOnly(err) {
		// Immediate reservation is a write. A query_only connection, used by
		// read-only discovery, cannot take it. Deferred BEGIN still serves the read.
		tx, err = d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	}
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction failed: %w; rollback failed: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
