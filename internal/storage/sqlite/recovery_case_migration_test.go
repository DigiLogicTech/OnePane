package sqlite

import (
	"context"
	"strings"
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

func TestRecoveryDiscoveryMigrationPreservesExistingIntakeAndProvenance(t *testing.T) {
	ctx := context.Background()
	db, e := Open(t.TempDir() + "/v102.db")
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
		if m.version < 103 {
			if e = db.applyMigration(ctx, m); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Preserved','active',1,1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','Preserve interrupted effects','blocked','normal_task',0,'{}',7,1,1)`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('attempt','task',1,'interrupted','{"unknown_effect":true}')`,
		`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('event','ws','task.attempt_interrupted','task','task','{"task_id":"task","attempt_id":"attempt"}',1)`,
		`INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,task_revision_at_capture,revision,created_at) VALUES('case','ws','task','attempt','event','deleted-original-job',7,3,123)`,
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
	var source, job, phase, state, status, metadata string
	var revision, created, taskRevision int
	if e = db.SQL().QueryRow(`SELECT c.source_kind,c.source_job_id,c.phase,c.revision,c.created_at,t.state,t.revision,a.status,a.metadata_json
 FROM task_recovery_cases c JOIN tasks t ON t.id=c.task_id JOIN task_attempts a ON a.id=c.attempt_id WHERE c.id='case'`).Scan(&source, &job, &phase, &revision, &created, &state, &taskRevision, &status, &metadata); e != nil || source != "outbox" || job != "deleted-original-job" || phase != "awaiting_inventory" || revision != 3 || created != 123 || state != "blocked" || taskRevision != 7 || status != "interrupted" || metadata != `{"unknown_effect":true}` {
		t.Fatalf("migration lost identity/history: %s %s %s %d %d %s %d %s %s %v", source, job, phase, revision, created, state, taskRevision, status, metadata, e)
	}
	for _, q := range []string{`UPDATE task_recovery_cases SET source_kind='interruption_ledger',source_job_id=NULL`, `UPDATE task_recovery_cases SET source_job_id='invented'`, `DELETE FROM task_recovery_cases`} {
		if _, e = db.SQL().Exec(q); e == nil {
			t.Fatalf("immutable evidence changed: %s", q)
		}
	}
	if _, e = db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('historical','task',2,'interrupted','{}')`); e != nil {
		t.Fatal(e)
	}
	for _, provenance := range []string{"NULL,'outbox'", "'','outbox'", "'fake-job','interruption_ledger'"} {
		if _, e = db.SQL().Exec(`INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,source_kind,task_revision_at_capture,created_at) VALUES('bad','ws','task','historical','event',` + provenance + `,7,1)`); e == nil {
			t.Fatalf("invalid provenance accepted: %s", provenance)
		}
	}
	if _, e = db.SQL().Exec(`INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_kind,task_revision_at_capture,created_at) VALUES('history','ws','task','historical','event','interruption_ledger',7,1)`); e != nil {
		t.Fatal(e)
	}
	rows, e := db.SQL().Query(`PRAGMA foreign_key_check`)
	if e != nil {
		t.Fatal(e)
	}
	if rows.Next() {
		t.Fatal("foreign key failure")
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	if e = rows.Close(); e != nil {
		t.Fatal(e)
	}
	rows, e = db.SQL().Query(`EXPLAIN QUERY PLAN SELECT id FROM task_attempts WHERE status='interrupted' AND id>'cursor' ORDER BY id LIMIT 16`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if e = rows.Scan(&id, &parent, &unused, &detail); e != nil {
			t.Fatal(e)
		}
		plan += detail
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(plan, "idx_task_recovery_interrupted_attempts") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("unbounded discovery query plan: %s", plan)
	}
}
