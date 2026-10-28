//go:build integration

package watchdog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteContentionExpiresAndRecoversDurableHeartbeat(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "watchdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.SQL().ExecContext(ctx, `CREATE TABLE watchdog_states(id TEXT PRIMARY KEY, watchdog_name TEXT UNIQUE, state_json TEXT, heartbeat_at INTEGER, revision INTEGER, updated_at INTEGER)`)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db.SQL(), db, clock.Real{}, "", 100*time.Millisecond)
	if err := s.Heartbeat(ctx, nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `UPDATE watchdog_states SET state_json='{}'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, 20*time.Millisecond, nil) }()
	await := func(predicate func() bool) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for !predicate() {
			select {
			case err := <-done:
				t.Fatalf("watchdog exited before recovery: %v", err)
			case <-ctx.Done():
				t.Fatal("heartbeat transition timed out")
			case <-ticker.C:
			}
		}
	}
	await(func() bool { return !s.Healthy(ctx) })
	stale, err := s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Revision != before.Revision || stale.HeartbeatAt != before.HeartbeatAt {
		t.Fatal("failed writes must not refresh heartbeat or revision")
	}
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	await(func() bool { return s.Healthy(ctx) })
	after, err := s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision <= before.Revision {
		t.Fatal("watchdog did not commit a recovered heartbeat")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}
