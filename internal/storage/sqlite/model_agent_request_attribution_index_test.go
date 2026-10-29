package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentworker"
)

// These are consumer predicates, deliberately independent of the index DDL.
// A future collector must validate full encoding/identity and materialize capped
// evidence separately. A NULL key is never evidence that an unsupported request
// could not belong to an interrupted Task.
const modelAgentTaskExpression = `CASE WHEN json_extract(request_json,'$.messages[1].role')='user'
 AND json_type(request_json,'$.messages[1].content')='text'
 THEN CASE WHEN json_valid(json_extract(request_json,'$.messages[1].content'))
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'))='object'
 AND json_extract(json_extract(request_json,'$.messages[1].content'),'$.protocol_version')='v1'
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'),'$.task_id')='text'
 THEN json_extract(json_extract(request_json,'$.messages[1].content'),'$.task_id') END END END END`

const modelAgentAttemptExpression = `CASE WHEN json_extract(request_json,'$.messages[1].role')='user'
 AND json_type(request_json,'$.messages[1].content')='text'
 THEN CASE WHEN json_valid(json_extract(request_json,'$.messages[1].content'))
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'))='object'
 AND json_extract(json_extract(request_json,'$.messages[1].content'),'$.protocol_version')='v1'
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'),'$.attempt_id')='text'
 THEN json_extract(json_extract(request_json,'$.messages[1].content'),'$.attempt_id') END END END END`

func TestModelAgentRequestAttributionUpgradePreservesUnattributableHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/model-attribution.db")
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
		if m.version < 105 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Preserve history','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('principal','agent','Worker','active',1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('foreign-task','ws','Other task','blocked','normal_task',0,'{}',1,1,1)`,
	} {
		if _, err = db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	request := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "request", WorkspaceID: "ws", TaskID: "original-task", AttemptID: "original-attempt", PrincipalID: "principal", Objective: "Observe identity", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalWait}}
	canonical, err := agentworker.BuildModelRequest(request, "L1")
	if err != nil {
		t.Fatal(err)
	}
	envelope := func(role string, content any) string {
		b, e := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "system", "content": "OnePane"}, map[string]any{"role": role, "content": content}}})
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	inner := `{"protocol_version":"v1","task_id":"original-task","attempt_id":"original-attempt"}`
	fixtures := map[string]string{
		"canonical-null":    string(canonical),
		"canonical-foreign": string(canonical),
		"ordinary-text":     envelope("user", "please inspect original-task"),
		"malformed-inner":   envelope("user", `{"protocol_version":"v1","task_id":"original-task"`),
		"null-inner":        envelope("user", "null"),
		"scalar-inner":      envelope("user", "false"),
		"array-inner":       envelope("user", "[]"),
		"wrong-role":        envelope("assistant", inner),
		"object-content":    envelope("user", map[string]any{"protocol_version": "v1", "task_id": "original-task"}),
		"unknown-version":   envelope("user", `{"protocol_version":"v2","task_id":"original-task","attempt_id":"original-attempt"}`),
		"wrong-id-types":    envelope("user", `{"protocol_version":"v1","task_id":7,"attempt_id":true}`),
		"partial-task":      envelope("user", `{"protocol_version":"v1","task_id":"original-task"}`),
		"partial-attempt":   envelope("user", `{"protocol_version":"v1","attempt_id":"original-attempt"}`),
		"mixed-task":        envelope("user", `{"protocol_version":"v1","task_id":"original-task","attempt_id":true}`),
		"mixed-attempt":     envelope("user", `{"protocol_version":"v1","task_id":7,"attempt_id":"original-attempt"}`),
		"missing-message":   `{"messages":[]}`,
		"nonobject-outer":   `[]`,
		// Index maintenance must tolerate valid preexisting large user content;
		// the collector's evidence byte cap is a separate mandatory boundary.
		"large-unrelated-text": envelope("user", strings.Repeat("x", 70000)),
	}
	wantTask := map[string]bool{"canonical-null": true, "canonical-foreign": true, "partial-task": true, "mixed-task": true}
	wantAttempt := map[string]bool{"canonical-null": true, "canonical-foreign": true, "partial-attempt": true, "mixed-attempt": true}
	insert := func(id, body string, taskID any) {
		if _, err = db.SQL().Exec(`INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) VALUES(?,'ws',?,'principal','unknown','{}','{}','{}',?,1,1)`, id, taskID, body); err != nil {
			t.Fatal(err)
		}
	}
	for id, body := range fixtures {
		var taskID any
		if id == "canonical-foreign" {
			taskID = "foreign-task"
		}
		insert(id, body, taskID)
	}
	// Populate unrelated history before the upgrade. Both negative and positive
	// lookups must SEARCH the expression index, never parse this entire history.
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 512; i++ {
		body := envelope("user", fmt.Sprintf(`{"protocol_version":"v1","task_id":"other-%d","attempt_id":"other-attempt-%d"}`, i, i))
		if _, err = tx.Exec(`INSERT INTO inference_requests(id,workspace_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) VALUES(?,'ws','principal','succeeded','{}','{}','{}',?,1,1)`, fmt.Sprintf("unrelated-%04d", i), body); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for id, body := range fixtures {
		var got, status string
		var taskKey, attemptKey sql.NullString
		if err = db.SQL().QueryRow(`SELECT request_json,status,`+modelAgentTaskExpression+`,`+modelAgentAttemptExpression+` FROM inference_requests WHERE id=?`, id).Scan(&got, &status, &taskKey, &attemptKey); err != nil {
			t.Fatal(err)
		}
		if got != body || status != "unknown" {
			t.Fatalf("upgrade changed %s history", id)
		}
		if taskKey.Valid != wantTask[id] || (taskKey.Valid && taskKey.String != "original-task") || attemptKey.Valid != wantAttempt[id] || (attemptKey.Valid && attemptKey.String != "original-attempt") {
			t.Fatalf("incorrect independent provenance keys for %s: %v %v", id, taskKey, attemptKey)
		}
	}
	for _, probe := range []struct{ expression, index, key string }{
		{modelAgentTaskExpression, "idx_inference_agent_request_task", "original-task"},
		{modelAgentAttemptExpression, "idx_inference_agent_request_attempt", "original-attempt"},
	} {
		for _, key := range []string{probe.key, "no-such-identity"} {
			query := `SELECT id FROM inference_requests WHERE ` + probe.expression + `=? LIMIT 5`
			rows, e := db.SQL().Query(`EXPLAIN QUERY PLAN `+query, key)
			if e != nil {
				t.Fatal(e)
			}
			matched := false
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if e = rows.Scan(&id, &parent, &unused, &detail); e != nil {
					t.Fatal(e)
				}
				if strings.Contains(detail, "SCAN inference_requests") {
					t.Fatalf("unbounded model attribution: %s", detail)
				}
				if strings.Contains(detail, "SEARCH inference_requests USING INDEX "+probe.index) {
					matched = true
				}
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			if e = rows.Close(); e != nil {
				t.Fatal(e)
			}
			if !matched {
				t.Fatalf("planner did not SEARCH %s", probe.index)
			}
			rows, e = db.SQL().Query(query, key)
			if e != nil {
				t.Fatal(e)
			}
			count := 0
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					t.Fatal(e)
				}
				wanted := wantTask
				if probe.key == "original-attempt" {
					wanted = wantAttempt
				}
				if !wanted[id] {
					t.Fatalf("incorrect ownership candidate %s", id)
				}
				count++
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			if e = rows.Close(); e != nil {
				t.Fatal(e)
			}
			want := 0
			if key == probe.key {
				want = 4
			}
			if count != want {
				t.Fatalf("attribution %s: got %d want %d", probe.index, count, want)
			}
		}
	}
	// Index maintenance must remain safe for newly written unsupported requests.
	insert("later-malformed", envelope("user", "{"), nil)
	var count int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=105`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration105 count %d: %v", count, err)
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
