package sqlite

import (
	"context"
	"reflect"
	"testing"
)

// This upgrades real populated prior-source storage. A new help request cannot
// be inferred from an old wait or interrupted Attempt during migration.
func TestTeamAssistanceMigrationPreservesPopulatedHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/assistance-upgrade.db")
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
	var expected migration
	for _, m := range items {
		if m.version == 110 {
			expected = m
		}
		if m.version < 110 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Preserved','active',1,1,1)`,
		`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024)
		 INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at)
		 SELECT 'task-'||x,'tenant','Preserve original history','waiting_approval','normal_task',0,'{}',7,1,1 FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) SELECT id,id,1,'waiting','{"original_wait":true}' FROM tasks WHERE workspace_id='tenant'`,
		`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at)
		 SELECT id,'tenant','task.waiting','task',id,json_object('task_id',id,'revision',7),1 FROM tasks WHERE workspace_id='tenant'`,
	} {
		if _, err = db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"workspaces", "tasks", "task_attempts", "events", "principals"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("help migration changed prior Task/Attempt/wait history or identity")
	}
	var count int
	if expected.filename != "0110_team_assistance_requests.sql" {
		t.Fatal("missing reviewed assistance migration")
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=110 AND name=? AND checksum=?`, expected.name, expected.checksum).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration110=%d err=%v", count, err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assistance_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration invented help requests: %d %v", count, err)
	}
	var table string
	if err = db.SQL().QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type='index' AND name='idx_team_assistance_session'`).Scan(&table); err != nil || table != "team_assistance_requests" {
		t.Fatalf("unexpected index table %q: %v", table, err)
	}
	var unique, partial int
	if err = db.SQL().QueryRow(`SELECT "unique",partial FROM pragma_index_list('team_assistance_requests') WHERE name='idx_team_assistance_session'`).Scan(&unique, &partial); err != nil || unique != 0 || partial != 0 {
		t.Fatalf("unexpected index flags %d %d: %v", unique, partial, err)
	}
	rows, err := db.SQL().Query(`SELECT name,"desc",coll FROM pragma_index_xinfo('idx_team_assistance_session') WHERE "key"=1 ORDER BY seqno`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var key, collation string
		var descending int
		if err = rows.Scan(&key, &descending, &collation); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if descending != 0 || collation != "BINARY" {
			rows.Close()
			t.Fatal("changed help index ordering or collation")
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(keys, []string{"workspace_id", "session_id", "created_at", "id"}) {
		t.Fatalf("changed help index keys: %v", keys)
	}
	rows, err = db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("help upgrade left foreign key violation")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
