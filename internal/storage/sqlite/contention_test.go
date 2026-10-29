package sqlite

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func TestWithinSurvivesWriteContention(t *testing.T) {
	db, err := Open(t.TempDir() + "/contention.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE counters(id INTEGER PRIMARY KEY, n INTEGER NOT NULL)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO counters(id, n) VALUES (1, 0)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	const writers = 8
	const each = 40
	var failures atomic.Int64
	var busy atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < each; n++ {
				err := db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
					var current int
					if err := tx.QueryRowContext(ctx, `SELECT n FROM counters WHERE id=1`).Scan(&current); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, `UPDATE counters SET n=n+1 WHERE id=1`)
					return err
				})
				if err != nil {
					failures.Add(1)
					if IsBusy(err) {
						busy.Add(1)
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("contention caused %d write failures (%d busy)", failures.Load(), busy.Load())
	}
	var got int
	if err := db.SQL().QueryRowContext(ctx, `SELECT n FROM counters WHERE id=1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != writers*each {
		t.Fatalf("committed increments = %d, want %d", got, writers*each)
	}
	var mode int
	if err := db.SQL().QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&mode); err != nil || mode != 1 {
		t.Fatalf("WAL synchronous=%d err=%v", mode, err)
	}
}
