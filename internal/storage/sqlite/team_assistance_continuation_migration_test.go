package sqlite

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

// Upgrade evidence uses actual previous-schema rows, not a fresh final-schema
// fixture. This establishes retention, not execution or credential authority.
func TestTeamAssistanceContinuationMigrationPreservesWaitingHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/continuation-upgrade.db")
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
		if m.version == 113 {
			expected = m
		}
		if m.version < 113 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('tenant','Preserve','active',1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('human','human','Owner','active',1,1),('system:agent-worker','system','Worker','active',1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) VALUES('root','tenant','Preserve','running','{}',1,1)`,
		`INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,created_at,updated_at) VALUES('child','tenant','root','Preserve','waiting_approval','{}',1,1)`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,worker_principal_id,metadata_json) VALUES('attempt','child',1,'waiting','system:agent-worker','{"agent_worker":"v1"}')`,
		`INSERT INTO teams(id,workspace_id,name,status,created_by,created_at,updated_at) VALUES('team','tenant','Preserve','active','human',1,1)`,
		`INSERT INTO team_members(id,team_id,workspace_id,member_kind,display_name,role_name,route_policy_json,status,config_json,created_at,updated_at) VALUES('member','team','tenant','agent','Builder','Build','{}','active','{}',1,1)`,
		`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,accepted_plan_id,config_json,created_by,created_at,updated_at) VALUES('session','tenant','team','root','executing','plan','{}','human',1,1)`,
		`INSERT INTO team_plans(id,workspace_id,session_id,version,status,plan_json,created_at) VALUES('plan','tenant','session',1,'accepted','{}',1)`,
		`INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) VALUES('session','plan','build','tenant','member','child','root','[]','preserved','agent.md',0,1)`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('run','tenant','child','attempt','system:agent-worker','waiting','Build','inference.general','L1',8,2,2,'{}','{"human_input_required":"Preserved private question"}',1,1)`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES('step','run',1,'human','waiting','human','{}',1,1)`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) VALUES('notice','tenant','session','member','system:agent-worker','system','{}',0,1)`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) VALUES('request','tenant','session','plan','build','member','child','attempt','run','step','notice','system:agent-worker','Preserved private question',1)`,
		`INSERT INTO team_assistance_request_scopes(request_id) VALUES('request')`,
		`INSERT INTO team_assistance_answers(request_id,id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at) VALUES('request','answer','human','key','Preserved human answer',3,4,2)`,
		`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO events(id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'history-'||x,'agent_worker.step','agent_worker_run','run','{}',x FROM n`,
	} {
		if _, err = db.SQL().Exec(query); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	tables := []string{"tasks", "task_attempts", "teams", "team_members", "team_sessions", "team_plans", "team_execution_assignments", "agent_worker_runs", "agent_worker_steps", "team_messages", "team_assistance_requests", "team_assistance_request_scopes", "team_assistance_answers", "events"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("continuation upgrade changed waiting and answer history")
	}
	var count int
	if expected.filename != "0113_team_assistance_continuations.sql" {
		t.Fatal("missing reviewed continuation migration")
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=113 AND name=? AND checksum=?`, expected.name, expected.checksum).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration113=%d err=%v", count, err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assistance_continuations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("upgrade invented continuation: %d %v", count, err)
	}
	assertAssistanceContinuationConstraints(t, db)
	assertAssistanceHumanWaitLookupPlan(t, db)
}

func assertAssistanceContinuationConstraints(t *testing.T, db *DB) {
	t.Helper()
	for _, query := range []string{
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES('step2','run',2,'human','waiting','human','{}',2,2),('continued','run',3,'human','succeeded','human','{}',3,3)`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT 'notice2',workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,2 FROM team_messages WHERE id='notice'`,
		`INSERT INTO team_assistance_requests SELECT 'request2',workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,'step2','notice2',author_principal_id,question,2 FROM team_assistance_requests WHERE id='request'`,
		`INSERT INTO team_assistance_answers(request_id,id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at) VALUES('request2','answer2','human','key2','Other answer',3,4,2)`,
	} {
		if _, err := db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	insert := `INSERT INTO team_assistance_continuations(request_id,id,answer_id,actor_principal_id,continue_step_id,task_revision,worker_revision,created_at) VALUES(?,?,?,?,?,?,?,?)`
	for _, args := range [][]any{
		{"missing", "receipt", "answer", "human", "continued", 3, 4, 3},
		{"request", "receipt", "missing", "human", "continued", 3, 4, 3},
		{"request", "receipt", "answer2", "human", "continued", 3, 4, 3},
		{"request", "receipt", "answer", "missing", "continued", 3, 4, 3},
		{"request", "receipt", "answer", "human", "missing", 3, 4, 3},
		{"request", "receipt", "answer", "human", "continued", 0, 4, 3},
		{"request", "receipt", "answer", "human", "continued", 3, 0, 3},
		{"request", "receipt", "answer", "human", "continued", 3, 4, 0},
	} {
		if _, err := db.SQL().Exec(insert, args...); err == nil {
			t.Fatalf("invalid continuation receipt accepted: %v", args)
		}
	}
	// A receipt must roll back with the execution transition's caller-owned TX.
	tx, err := db.SQL().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(insert, "request", "receipt", "answer", "human", "continued", 3, 4, 3); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assistance_continuations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback retained receipt: %d %v", count, err)
	}
	if _, err = db.SQL().Exec(insert, "request", "receipt", "answer", "human", "continued", 3, 4, 3); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE team_assistance_continuations SET task_revision=99 WHERE request_id='request'`,
		`DELETE FROM team_assistance_continuations WHERE request_id='request'`,
		`INSERT INTO team_assistance_continuations SELECT request_id,'receipt2',answer_id,actor_principal_id,continue_step_id,task_revision,worker_revision,created_at FROM team_assistance_continuations`,
		`INSERT INTO team_assistance_continuations SELECT 'request2','receipt2','answer2',actor_principal_id,continue_step_id,task_revision,worker_revision,created_at FROM team_assistance_continuations`,
	} {
		if _, err = db.SQL().Exec(query); err == nil {
			t.Fatalf("immutable/unique receipt accepted %s", query)
		}
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("continuation upgrade left foreign key violation")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertAssistanceHumanWaitLookupPlan(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.SQL().Exec(`WITH RECURSIVE n(x) AS (VALUES(4) UNION ALL SELECT x+1 FROM n WHERE x<1027)
 INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,detail_json,started_at,completed_at)
 SELECT 'model-history-'||x,'run',x,'model','succeeded','{}',x,x FROM n`); err != nil {
		t.Fatal(err)
	}
	query := `SELECT id FROM agent_worker_steps WHERE run_id=? AND step_kind='human' AND status='waiting' ORDER BY step_number DESC LIMIT 1`
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			if _, err := db.SQL().Exec(`ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		for _, owner := range []string{"run", "missing", "foreign"} {
			plan := recoveryDirectLookupPlan(t, db.SQL(), query, owner)
			if !strings.Contains(plan, "SEARCH agent_worker_steps USING INDEX idx_agent_worker_human_wait") || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("unbounded latest human wait plan afterAnalyze=%v: %s", analyzed, plan)
			}
			var id string
			err := db.SQL().QueryRow(query, owner).Scan(&id)
			if owner == "run" {
				if err != nil || id != "step2" {
					t.Fatalf("wrong latest wait %q %v", id, err)
				}
			} else if err != sql.ErrNoRows {
				t.Fatalf("foreign/missing wait resolved %q %v", id, err)
			}
		}
	}
}
