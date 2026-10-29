package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestTeamRunningTurnIndexPreservesPopulatedUpgrade(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/turn-history.db")
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
		if m.version < 107 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('tenant','Tenant','active',1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('human','human','Human','active',1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES('task','tenant','Preserved','created','normal_task',0,'{}',1,1)`,
		`INSERT INTO teams(id,workspace_id,name,status,created_by,created_at,updated_at) VALUES('team','tenant','Team','active','human',1,1)`,
		`INSERT INTO team_members(id,team_id,workspace_id,member_kind,display_name,role_name,route_policy_json,status,config_json,created_at,updated_at) VALUES('member','team','tenant','agent','Seat','Reviewer','{}','active','{}',1,1)`,
		`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,config_json,created_by,created_at,updated_at) VALUES('session','tenant','team','task','deliberating','{}','human',1,1)`,
		`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<512) INSERT INTO team_turn_requests(id,workspace_id,session_id,member_id,status,error_text,round_number,created_at,updated_at) SELECT 'turn-'||i,'tenant','session','member',CASE WHEN i=1 THEN 'running' ELSE 'succeeded' END,CASE WHEN i=1 THEN 'operator reconciliation required' ELSE NULL END,i,1,1 FROM n`,
	} {
		if _, err = db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"workspaces", "principals", "tasks", "teams", "team_members", "team_sessions", "team_turn_requests"}
	before := recoveryProvenanceRows(t, db.SQL(), tables)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := recoveryProvenanceRows(t, db.SQL(), tables); !reflect.DeepEqual(before, after) {
		t.Fatal("index upgrade changed original Task/turn history")
	}
	plan := recoveryDirectLookupPlan(t, db.SQL(), `SELECT EXISTS(SELECT 1 FROM team_turn_requests WHERE session_id=? AND status='running')`, "session")
	if !strings.Contains(plan, "idx_team_turn_requests_session_status (session_id=? AND status=?)") {
		t.Fatalf("unindexed running-history probe: %s", plan)
	}
	var count int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=107`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration applied count=%d error=%v", count, err)
	}
	rows, err := db.SQL().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("index upgrade broke foreign keys")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
