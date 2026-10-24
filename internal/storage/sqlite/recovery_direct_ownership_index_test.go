package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every direct Task/Attempt edge in the reviewed v1 structural contract. The
// TCP alert attempt_id is explicitly excluded: it names a TCP probe Attempt.
var recoveryDirectOwnershipEdges = map[string][]string{
	"agent_runtime_invocations":             {"task_id", "attempt_id"},
	"agent_worker_runs":                     {"task_id", "attempt_id"},
	"assistant_project_handoffs":            {"task_id"},
	"assistant_turns":                       {"task_id"},
	"assurance_runs":                        {"task_id"},
	"budget_reservations":                   {"task_id"},
	"capability_leases":                     {"task_id"},
	"checkpoints":                           {"task_id"},
	"continuation_capsules":                 {"task_id"},
	"deliberations":                         {"task_id"},
	"edit_proposals":                        {"task_id"},
	"incidents":                             {"task_id"},
	"inference_requests":                    {"task_id"},
	"inference_sessions":                    {"task_id"},
	"node_power_holds":                      {"task_id"},
	"node_wake_attempts":                    {"task_id"},
	"operations":                            {"task_id", "attempt_id"},
	"plan_proposals":                        {"task_id"},
	"project_change_proposals":              {"task_id"},
	"project_orchestrator_task_graph_nodes": {"task_id"},
	"project_orchestrator_turns":            {"task_id"},
	"recovery_snapshots":                    {"task_id", "attempt_id"},
	"resource_leases":                       {"task_id"},
	"routine_occurrences":                   {"task_id"},
	"runtime_execution_queue":               {"task_id"},
	"task_attempts":                         {"task_id"},
	"task_dependencies":                     {"task_id", "depends_on_task_id"},
	"task_execution_profiles":               {"task_id"},
	"task_recovery_cases":                   {"task_id", "attempt_id"},
	"tasks":                                 {"parent_task_id"},
	"team_assignment_notices":               {"task_id"},
	"team_assignment_roots":                 {"task_id"},
	"team_execution_assignments":            {"task_id", "parent_task_id"},
	"team_execution_held_tasks":             {"task_id"},
	"team_execution_holds":                  {"root_task_id"},
	"team_sessions":                         {"task_id"},
	"tool_invocations":                      {"task_id", "attempt_id"},
	"verifications":                         {"task_id"},
	"workspace_file_publications":           {"task_id"},
}

func TestRecoveryDirectOwnershipPlans(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/direct.db")
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
		if m.version < 106 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	seedRecoveryProvenanceRows(t, db.SQL())
	seedRecoveryDirectOwnershipRows(t, db.SQL())
	var tables []string
	for table := range recoveryDirectOwnershipEdges {
		tables = append(tables, table)
	}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("direct ownership index upgrade changed historical rows")
	}
	var applied int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=106`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration106 applied=%d err=%v", applied, err)
	}
	assertRecoveryDirectEdgeCoverage(t, db.SQL())
	for table, columns := range recoveryDirectOwnershipEdges {
		for _, column := range columns {
			t.Run(table+"."+column, func(t *testing.T) {
				query := "SELECT * FROM " + table + " WHERE " + column + "=? LIMIT 2"
				value := "owned-task"
				if column == "attempt_id" {
					value = "owned-attempt"
				}
				for _, value := range []string{value, "missing"} {
					plan := recoveryDirectLookupPlan(t, db.SQL(), query, value)
					if !strings.Contains(plan, "SEARCH "+table+" USING ") || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") {
						t.Errorf("unbounded direct ownership probe: %s", plan)
					}
					if value == "missing" {
						var row any
						if err = db.SQL().QueryRow("SELECT 1 FROM "+table+" WHERE "+column+"=? LIMIT 1", value).Scan(&row); err != sql.ErrNoRows {
							t.Fatalf("missing ownership unexpectedly found a row: %v", err)
						}
					}
				}
			})
		}
	}
	// Each newly indexed path has its own positive record. Attempt probes
	// deliberately have null/foreign Task ownership and require no Task match.
	newProbes := map[string]string{
		"agent_runtime_invocations.attempt_id":      "target",
		"operations.attempt_id":                     "direct-target",
		"tool_invocations.attempt_id":               "direct-target",
		"recovery_snapshots.attempt_id":             "direct-target",
		"assistant_turns.task_id":                   "target",
		"assistant_project_handoffs.task_id":        "direct-target",
		"project_orchestrator_turns.task_id":        "target",
		"deliberations.task_id":                     "direct-target",
		"incidents.task_id":                         "direct-target",
		"plan_proposals.task_id":                    "direct-target",
		"routine_occurrences.task_id":               "direct-target",
		"task_dependencies.depends_on_task_id":      "direct-target",
		"team_execution_assignments.parent_task_id": "direct-target",
		"team_execution_holds.root_task_id":         "direct-target",
		"team_execution_held_tasks.task_id":         "direct-target",
	}
	for edge, expected := range newProbes {
		parts := strings.Split(edge, ".")
		key := "id"
		switch parts[0] {
		case "task_dependencies", "team_execution_assignments":
			key = "task_id"
		case "team_execution_held_tasks":
			key = "hold_id"
		}
		value := "owned-task"
		if parts[1] == "attempt_id" {
			value = "owned-attempt"
		}
		var actual string
		if err = db.SQL().QueryRow("SELECT "+key+" FROM "+parts[0]+" WHERE "+parts[1]+"=?", value).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("reverse independent %s lost: actual=%q expected=%q err=%v", edge, actual, expected, err)
		}
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after direct ownership upgrade")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertRecoveryDirectEdgeCoverage(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	actual := map[string][]string{}
	tcpException := false
	for _, table := range tables {
		rows, err := db.Query(`SELECT "from","table","to" FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var from, target, to string
			if err = rows.Scan(&from, &target, &to); err != nil {
				t.Fatal(err)
			}
			if target == "tasks" || target == "task_attempts" {
				actual[table] = append(actual[table], from)
			}
			if table == "project_goal_tcp_reachability_alerts" && from == "attempt_id" && target == "project_goal_tcp_probe_attempts" && to == "id" {
				tcpException = true
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	if !tcpException {
		t.Fatal("TCP Attempt exception changed and needs ownership review")
	}
	// Reviewed unconstrained fields cannot be discovered from foreign keys.
	for _, table := range []string{"node_power_holds", "node_wake_attempts", "runtime_execution_queue"} {
		actual[table] = []string{"task_id"}
	}
	expected := map[string][]string{}
	for table, columns := range recoveryDirectOwnershipEdges {
		expected[table] = append([]string(nil), columns...)
		sort.Strings(expected[table])
	}
	for table := range actual {
		sort.Strings(actual[table])
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("direct ownership coverage requires review: actual=%v expected=%v", actual, expected)
	}
}

func recoveryDirectLookupPlan(t *testing.T, db *sql.DB, query string, value any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, value)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
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
	return strings.Join(details, " | ")
}

func seedRecoveryDirectOwnershipRows(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatalf("direct seed: %v: %s", err, q)
		}
	}
	exec(`UPDATE agent_runtime_invocations SET attempt_id='owned-attempt' WHERE id='target'`)
	exec(`UPDATE assistant_turns SET task_id='owned-task' WHERE id='target'`)
	exec(`UPDATE project_orchestrator_turns SET task_id='owned-task' WHERE id='target'`)
	exec(`INSERT INTO routines(id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at) VALUES('routine','foreign','Preserved',1,'paused','{}','{}','UTC','principal',1,1)`)
	exec(`INSERT INTO teams(id,workspace_id,name,status,created_by,created_at,updated_at) VALUES('direct-team','foreign','Preserved','active','principal',1,1)`)
	exec(`INSERT INTO team_members(id,team_id,workspace_id,principal_id,member_kind,display_name,role_name,route_policy_json,status,config_json,created_at,updated_at) VALUES('direct-member','direct-team','foreign','principal','human','Owner','reviewer','{}','active','{}',1,1)`)
	exec(`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,config_json,created_by,created_at,updated_at) VALUES('direct-session','foreign','direct-team','foreign-task','completed','{}','principal',1,1)`)
	exec(`INSERT INTO team_plans(id,workspace_id,session_id,version,status,plan_json,created_at) VALUES('direct-plan','foreign','direct-session',1,'accepted','{}',1)`)
	for i := 0; i <= 128; i++ {
		id := fmt.Sprintf("direct-noise-%03d", i)
		task := id
		attempt := "foreign-attempt"
		if i == 128 {
			id = "direct-target"
			task = "owned-task"
			attempt = "owned-attempt"
		}
		// A separate child Task supplies each unique assignment/dependency key.
		exec(`INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES(?,'foreign',?,'Preserved','blocked','normal_task',0,'{}',1,1)`, id, "foreign-task")
		exec(`INSERT INTO operations(id,workspace_id,task_id,attempt_id,principal_id,idempotency_key,state,capability_id,resource_ref,tool_id,tool_version,adapter_id,adapter_version,desired_state_json,precondition_json,reconciliation_json,policy_revision,input_hash,created_at,updated_at) VALUES(?,'foreign','foreign-task',?,'principal',?,'committed','test','test','test','1','test','1','{}','{}','{}',1,'hash',1,1)`, id, attempt, id)
		exec(`INSERT INTO tool_invocations(id,workspace_id,task_id,attempt_id,principal_id,tool_id,tool_version,adapter_id,adapter_version,mode,input_hash,status,created_at,updated_at) VALUES(?,'foreign',NULL,?,'principal','test','1','test','1','read','hash','succeeded',1,1)`, id, attempt)
		exec(`INSERT INTO recovery_snapshots(id,workspace_id,task_id,attempt_id,snapshot_json,created_at) VALUES(?,'foreign','foreign-task',?,'{}',1)`, id, attempt)
		exec(`INSERT INTO assistant_project_handoffs(id,assistant_thread_id,project_id,orchestrator_id,task_id,status,objective,created_at,updated_at) VALUES(?,'thread','project','orchestrator',?,'completed','Preserved',1,1)`, id, task)
		exec(`INSERT INTO deliberations(id,workspace_id,task_id,state,config_json,budget_json,created_at,updated_at) VALUES(?,'foreign',?,'completed','{}','{}',1,1)`, id, task)
		exec(`INSERT INTO incidents(id,workspace_id,task_id,severity,state,incident_type,summary,details_json,opened_at,updated_at) VALUES(?,'foreign',?,'info','resolved','test','Preserved','{}',1,1)`, id, task)
		exec(`INSERT INTO plan_proposals(id,task_id,principal_id,proposal_json,status,created_at,updated_at) VALUES(?,?,'principal','{}','superseded',1,1)`, id, task)
		exec(`INSERT INTO routine_occurrences(id,routine_id,definition_version,occurrence_key,task_id,state,trigger_time_utc,created_at,updated_at) VALUES(?,'routine',1,?,?,'succeeded',1,1,1)`, id, id, task)
		// Inverse dependency ownership and parent-assignment ownership remain
		// independently visible even when their direct Task is a foreign child.
		exec(`INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{}')`, id, task)
		exec(`INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) VALUES('direct-session','direct-plan',?,'foreign','direct-member',?,?,'[]','hash','test',1,1)`, id, id, task)
		exec(`INSERT INTO team_execution_holds(id,session_id,plan_id,root_task_id,created_by,created_at,released_by,released_at) VALUES(?,'direct-session','direct-plan',?,'principal',1,'principal',2)`, id, task)
		exec(`INSERT INTO team_execution_held_tasks(hold_id,task_id,previous_state) VALUES(?,?,'running')`, id, task)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
