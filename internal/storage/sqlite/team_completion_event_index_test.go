package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestTeamCompletionEventIndexPreservesPopulatedUpgrade(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/completion-upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.SQL().Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	items, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range items {
		if m.version < 108 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 512; i++ {
		if _, err = db.SQL().Exec(`INSERT INTO events(id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'task.completed','task','historical-task',?,?)`, fmt.Sprintf("history-%d", i), fmt.Sprintf(`{"task_id":"historical-task","revision":%d}`, i+1), i+1); err != nil {
			t.Fatal(err)
		}
	}
	before := recoveryProvenanceRows(t, db.SQL(), []string{"events"})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := recoveryProvenanceRows(t, db.SQL(), []string{"events"}); !reflect.DeepEqual(before, after) {
		t.Fatal("completion index migration altered historical ledger")
	}
	var applied int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=108`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration108=%d err=%v", applied, err)
	}
	var definition string
	if err = db.SQL().QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_events_task_latest_completion'`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if definition != "CREATE INDEX idx_events_task_latest_completion\nON events(aggregate_type,aggregate_id,event_type,sequence DESC)" {
		t.Fatalf("unexpected index contract: %q", definition)
	}
}
