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
	// sqlite_master retains the migration's checkout line endings. Assert the
	// actual index structure instead of treating CRLF/LF as a schema change.
	var table string
	if err = db.SQL().QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type='index' AND name='idx_events_task_latest_completion'`).Scan(&table); err != nil || table != "events" {
		t.Fatalf("unexpected index table: %q err=%v", table, err)
	}
	var unique, partial int
	if err = db.SQL().QueryRow(`SELECT "unique",partial FROM pragma_index_list('events') WHERE name='idx_events_task_latest_completion'`).Scan(&unique, &partial); err != nil || unique != 0 || partial != 0 {
		t.Fatalf("unexpected index flags: unique=%d partial=%d err=%v", unique, partial, err)
	}
	rows, err := db.SQL().Query(`SELECT name,"desc",coll FROM pragma_index_xinfo('idx_events_task_latest_completion') WHERE "key"=1 ORDER BY seqno`)
	if err != nil {
		t.Fatal(err)
	}
	type indexKey struct {
		name      string
		desc      int
		collation string
	}
	var keys []indexKey
	for rows.Next() {
		var key indexKey
		if err = rows.Scan(&key.name, &key.desc, &key.collation); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	want := []indexKey{{"aggregate_type", 0, "BINARY"}, {"aggregate_id", 0, "BINARY"}, {"event_type", 0, "BINARY"}, {"sequence", 1, "BINARY"}}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("unexpected index key contract: %+v", keys)
	}
}
