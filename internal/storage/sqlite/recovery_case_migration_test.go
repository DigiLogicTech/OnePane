package sqlite

import (
	"context"
	"testing"
)

func TestRecoveryCaptureMigrationPreservesPopulatedInterruptedIncarnation(t *testing.T) {
	ctx := context.Background()
	db, e := Open(t.TempDir() + "/pre-recovery.db")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.SQL().Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT`); e != nil {
		t.Fatal(e)
	}
	items, e := loadMigrations()
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range items {
		if m.version >= 102 {
			continue
		}
		if e = db.applyMigration(ctx, m); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Preserved','active',1,1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','Preserve interrupted effects','blocked','normal_task',0,'{}',7,1,1)`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('attempt','task',1,'interrupted','{"unknown_effect":true}')`,
		`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('event','ws','task.attempt_interrupted','task','task','{"task_id":"task","attempt_id":"attempt"}',1)`,
		`INSERT INTO outbox_jobs(id,workspace_id,job_type,payload_json,status,available_at,max_attempts,created_at,updated_at) VALUES('job','ws','task.recovery.required','{"task_id":"task","attempt_id":"attempt"}','pending',1,10,1,1)`,
	} {
		if _, e = db.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	var state, status, metadata, jobStatus string
	var revision, cases int
	if e = db.SQL().QueryRow(`SELECT t.state,t.revision,a.status,a.metadata_json,j.status FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN outbox_jobs j ON j.id='job' WHERE t.id='task'`).Scan(&state, &revision, &status, &metadata, &jobStatus); e != nil || state != "blocked" || revision != 7 || status != "interrupted" || metadata != `{"unknown_effect":true}` || jobStatus != "pending" {
		t.Fatalf("upgrade changed incarnation: %s %d %s %s %s %v", state, revision, status, metadata, jobStatus, e)
	}
	if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_recovery_cases`).Scan(&cases); e != nil || cases != 0 {
		t.Fatalf("migration invented recovery evidence: %d %v", cases, e)
	}
	rows, e := db.SQL().Query(`PRAGMA foreign_key_check`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after additive upgrade")
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}
