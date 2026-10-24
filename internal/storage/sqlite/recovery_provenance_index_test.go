package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// These tests prove additive database lookup behavior only. They do not collect
// an inventory, validate actual effects, or authorize a recovery disposition.
func TestRecoveryProvenanceIndexesPreservePopulatedUpgradeAndBoundReverseLookups(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/provenance.db")
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
		if m.version < 104 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	seedRecoveryProvenanceRows(t, db.SQL())
	tables := []string{"workspaces", "principals", "projects", "tasks", "task_attempts", "events", "agent_runtime_connections", "agent_runtime_invocations", "artifacts", "project_library_assets", "project_library_asset_versions", "assistant_threads", "assistant_turns", "project_orchestrators", "project_orchestrator_turns", "gateway_connections", "gateway_targets", "notification_deliveries"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatalf("repeated upgrade: %v", err)
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("additive index migration changed existing rows or provenance")
	}
	var count int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=104`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration104 applied count=%d err=%v", count, err)
	}
	probes := []struct {
		name, table, key, predicate, value, index string
	}{
		{"runtime_task", "agent_runtime_invocations", "id", "json_extract(request_json,'$.task_id')", "owned-task", "idx_runtime_recovery_request_task"},
		{"runtime_attempt", "agent_runtime_invocations", "id", "json_extract(request_json,'$.attempt_id')", "owned-attempt", "idx_runtime_recovery_request_attempt"},
		{"artifact_task", "artifacts", "id", "json_extract(metadata_json,'$.task_id')", "owned-task", "idx_artifacts_recovery_task"},
		{"artifact_attempt", "artifacts", "id", "json_extract(metadata_json,'$.attempt_id')", "owned-attempt", "idx_artifacts_recovery_attempt"},
		{"artifact_inference", "artifacts", "id", "json_extract(metadata_json,'$.inference_request_id')", "owned-inference", "idx_artifacts_recovery_inference"},
		{"artifact_runtime", "artifacts", "id", "json_extract(metadata_json,'$.agent_runtime_invocation_id')", "owned-runtime", "idx_artifacts_recovery_runtime"},
		{"artifact_agent_request", "artifacts", "id", "json_extract(metadata_json,'$.agent_request_id')", "owned-agent-request", "idx_artifacts_recovery_agent_request"},
		{"library_task", "project_library_asset_versions", "asset_id || ':' || version", "json_extract(provenance_json,'$.task_id')", "owned-task", "idx_library_versions_recovery_task"},
		{"library_attempt", "project_library_asset_versions", "asset_id || ':' || version", "json_extract(provenance_json,'$.attempt_id')", "owned-attempt", "idx_library_versions_recovery_attempt"},
		{"library_artifact", "project_library_asset_versions", "asset_id || ':' || version", "json_extract(provenance_json,'$.artifact_id')", "owned-artifact", "idx_library_versions_recovery_artifact"},
		{"assistant_task", "assistant_turns", "id", "json_extract(provenance_json,'$.task_id')", "owned-task", "idx_assistant_turns_recovery_task"},
		{"orchestrator_task", "project_orchestrator_turns", "id", "json_extract(provenance_json,'$.task_id')", "owned-task", "idx_orchestrator_turns_recovery_task"},
		{"notification_event_id", "notification_deliveries", "id", "source_event_id", "owned-event", "idx_notifications_recovery_source_id"},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			// No INDEXED BY or tenant/status/direct-owner filters: the planner
			// must independently find inconsistent and historical associations.
			query := "SELECT " + p.key + " FROM " + p.table + " WHERE " + p.predicate + "=? LIMIT 2"
			for _, value := range []string{p.value, "absent-ownership"} {
				rows, err := db.SQL().Query("EXPLAIN QUERY PLAN "+query, value)
				if err != nil {
					t.Fatal(err)
				}
				var details []string
				for rows.Next() {
					var id, parent, unused int
					var detail string
					if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
						t.Fatal(err)
					}
					details = append(details, detail)
				}
				if err = rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
				plan := strings.Join(details, " | ")
				if !strings.Contains(plan, "SEARCH "+p.table+" USING INDEX "+p.index+" ") || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") {
					t.Fatalf("unbounded or unexpected lookup plan: %s", plan)
				}
				var key string
				err = db.SQL().QueryRow(query, value).Scan(&key)
				if value == "absent-ownership" {
					if err != sql.ErrNoRows {
						t.Fatalf("negative lookup found %q: %v", key, err)
					}
					continue
				}
				want := "target"
				if p.table == "project_library_asset_versions" {
					want = "asset:257"
				}
				if err != nil || key != want {
					t.Fatalf("reverse-only association lost: key=%q want=%q err=%v", key, want, err)
				}
			}
		})
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after upgrade")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func recoveryProvenanceRows(t *testing.T, db *sql.DB, tables []string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, table := range tables {
		// All table identifiers are the fixed test-owned allowlist above.
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var records [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			records = append(records, values)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		encoded, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		out[table] = string(encoded)
	}
	return out
}

func seedRecoveryProvenanceRows(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v: %s", err, q)
		}
	}
	for _, ws := range []string{"owned", "foreign"} {
		exec(`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES(?,?,'active',1,1)`, ws, ws)
		exec(`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES(?,?,'preserved','blocked','normal_task',0,'{}',1,1)`, ws+"-task", ws)
		exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES(?,?,1,'interrupted','{"agent_worker":"v1"}')`, ws+"-attempt", ws+"-task")
	}
	exec(`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('principal','human','Owner','active',1,1)`)
	exec(`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('project','foreign','Project','active','{}','{}','principal',1,1)`)
	exec(`INSERT INTO agent_runtime_connections(id,workspace_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,created_at,updated_at) VALUES('connection','foreign','test','Test','test','1','{}','none','registered','untrusted','proposal_only','{}','{}','{}',1,1)`)
	exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,created_at,updated_at) VALUES('asset','project','History','file',1,1)`)
	exec(`INSERT INTO assistant_threads(id,workspace_id,title,created_by,created_at,updated_at) VALUES('thread','foreign','History','principal',1,1)`)
	exec(`INSERT INTO project_orchestrators(id,project_id,workspace_id,created_at,updated_at) VALUES('orchestrator','project','foreign',1,1)`)
	exec(`INSERT INTO gateway_connections(id,workspace_id,preset_id,display_name,delivery_mode,config_json,status,created_by,created_at,updated_at) VALUES('gateway','foreign','test','Test','relay','{}','active','principal',1,1)`)
	exec(`INSERT INTO gateway_targets(id,workspace_id,connection_id,name,address,config_json,status,created_at,updated_at) VALUES('recipient','foreign','gateway','Test','loopback','{}','active',1,1)`)
	for i := 0; i <= 256; i++ {
		id := fmt.Sprintf("unrelated-%03d", i)
		metadata := fmt.Sprintf(`{"task_id":%q,"attempt_id":%q,"inference_request_id":%q,"agent_runtime_invocation_id":%q,"agent_request_id":%q,"artifact_id":%q}`, id, id, id, id, id, id)
		if i == 256 {
			id = "target"
			metadata = `{"task_id":"owned-task","attempt_id":"owned-attempt","inference_request_id":"owned-inference","agent_runtime_invocation_id":"owned-runtime","agent_request_id":"owned-agent-request","artifact_id":"owned-artifact"}`
		}
		exec(`INSERT INTO agent_runtime_invocations(id,workspace_id,task_id,attempt_id,principal_id,connection_id,status,request_json,created_at,updated_at) VALUES(?,'foreign','foreign-task','foreign-attempt','principal','connection','succeeded',?,1,1)`, id, metadata)
		exec(`INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_at) VALUES(?,'foreign','hash','application/json',1,'not-read','internal','any','unverified_derived','archived',?,1)`, id, metadata)
		exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,storage_uri,provenance_json,created_at) VALUES('asset',?,'hash','not-read',?,1)`, i+1, metadata)
		exec(`INSERT INTO assistant_turns(id,thread_id,role,content,task_id,provenance_json,created_at) VALUES(?,'thread','assistant','preserved',NULL,?,1)`, id, metadata)
		exec(`INSERT INTO project_orchestrator_turns(id,orchestrator_id,role,content,task_id,provenance_json,created_at) VALUES(?,'orchestrator','orchestrator','preserved','foreign-task',?,1)`, id, metadata)
		eventID := id + "-event"
		if i == 256 {
			eventID = "owned-event"
		}
		exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'owned','task.attempt_interrupted','task','owned-task','{}',1)`, eventID)
		// Only source_event_id connects this historical delivery; direct
		// Operation/Verification and source_event_seq associations are absent.
		exec(`INSERT INTO notification_deliveries(id,workspace_id,target_id,source_event_id,idempotency_key,message_json,status,next_attempt_at,created_at,updated_at) VALUES(?,'foreign','recipient',?,?,'{}','accepted',1,1,1)`, id, eventID, id)
	}
	// All were accepted by the previous JSON-valid schema. New expression
	// indexes must not turn unusual but valid legacy JSON into upgrade errors.
	for i, metadata := range []string{`null`, `[]`, `"legacy"`, `{"task_id":null}`, `{"task_id":{"nested":"owned-task"}}`, `{"task_id":["owned-task"]}`} {
		exec(`INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_at) VALUES(?,'foreign','hash','application/json',1,'not-read','internal','any','unverified_derived','archived',?,1)`, fmt.Sprintf("legacy-%d", i), metadata)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
