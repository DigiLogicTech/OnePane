//go:build integration

package recoveryinventory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/recoveryinventory"
)

func TestRecoveryTaskAttemptSchemaRejectsUnreviewedOwnership(t *testing.T) {
	ctx := context.Background()
	db := recoveryinventory.OpenMigratedRecoveryFixture(t, ctx)
	var err error
	if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, db.SQL()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sql string }{
		{"new_task_fk_arbitrary_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, owner TEXT REFERENCES tasks(id))`},
		{"new_attempt_fk_arbitrary_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, owner TEXT REFERENCES task_attempts(id))`},
		{"case_insensitive_task_fk", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, owner TEXT REFERENCES TASKS(id))`},
		{"case_insensitive_attempt_fk", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, owner TEXT REFERENCES Task_Attempts(id))`},
		{"case_insensitive_owner_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, TASK_ID TEXT)`},
		{"sqlite_like_wildcard_name", `CREATE TABLE sqliteXeffects(task_id TEXT)`},
		{"unconstrained_task_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, task_id TEXT)`},
		{"unconstrained_attempt_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, attempt_id TEXT)`},
		{"unconstrained_prefixed_task_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, source_task_id TEXT)`},
		{"unconstrained_prefixed_attempt_column", `CREATE TABLE new_effect(id TEXT PRIMARY KEY, original_attempt_id TEXT)`},
		{"unreviewed_payload_on_existing_owner", `ALTER TABLE tool_invocations ADD COLUMN new_effect_json TEXT`},
		{"changed_assistance_attempt", `ALTER TABLE team_assistance_requests RENAME COLUMN attempt_id TO inferred_attempt`},
		{"new_assistance_payload", `ALTER TABLE team_assistance_requests ADD COLUMN human_decision_json TEXT`},
		{"unreviewed_generated_payload", `ALTER TABLE tool_invocations ADD COLUMN generated_effect TEXT GENERATED ALWAYS AS (task_id) VIRTUAL`},
		{"ordinary_column_replaced_with_generated", `ALTER TABLE agent_runtime_invocations DROP COLUMN completed_at; ALTER TABLE agent_runtime_invocations ADD COLUMN completed_at INTEGER GENERATED ALWAYS AS (1) VIRTUAL`},
		{"renamed_owner", `ALTER TABLE agent_worker_runs RENAME COLUMN attempt_id TO original_incarnation`},
		{"renamed_composite_key", `ALTER TABLE workspace_file_publications RENAME COLUMN relative_path TO published_path`},
		{"missing_reviewed_table", `DROP TABLE team_assignment_notices`},
		{"changed_tcp_exception", `ALTER TABLE project_goal_tcp_reachability_alerts ADD COLUMN task_id TEXT REFERENCES tasks(id)`},
		{"changed_previously_unrelated_table", `ALTER TABLE notifications ADD COLUMN task_id TEXT`},
		{"quoted_untrusted_identifier", `CREATE TABLE "bad'; DROP TABLE tasks; --"(task_id TEXT)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.SQL().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, tx); !errors.Is(err, recoveryinventory.ErrTaskAttemptSchemaUnreviewed) {
				t.Fatalf("unreviewed ownership accepted: %v", err)
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, db.SQL()); err != nil {
				t.Fatalf("rollback changed contract: %v", err)
			}
		})
	}
	// This is structural review of one slice: unrelated schema does not acquire
	// Task ownership through a shared Project/tenant/configuration FK.
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE unrelated_config(id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), workspace_id TEXT REFERENCES workspaces(id))`); err != nil {
		t.Fatal(err)
	}
	if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, tx); err != nil {
		t.Fatalf("shared context treated as Task ownership: %v", err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE tasks(wrong_shape TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, tx); err != nil {
		t.Fatalf("temporary table shadowed main contract: %v", err)
	}
}

func TestRecoveryTaskAttemptSchemaIsBoundedAndReadOnly(t *testing.T) {
	ctx := context.Background()
	db := recoveryinventory.OpenMigratedRecoveryFixture(t, ctx)
	var err error
	conn, err := db.SQL().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, conn); err != nil {
		t.Fatalf("read-only schema contract: %v", err)
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sql string }{
		{"oversized_identifier", fmt.Sprintf(`CREATE TABLE "%s"(task_id TEXT)`, strings.Repeat("x", 257))},
		{"oversized_column_set", `CREATE TABLE too_wide(` + wideColumns(129) + `)`},
		{"oversized_fk_set", `CREATE TABLE too_many_fks(id TEXT, ` + manyFKs(129) + `)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, tx); !errors.Is(err, recoveryinventory.ErrTaskAttemptSchemaUnreviewed) {
				t.Fatalf("oversized schema accepted: %v", err)
			}
		})
	}
	// The metadata pass has one overall table bound, not an unbounded walk.
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 513; i++ {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE extra_%d(id TEXT)`, i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = recoveryinventory.ValidateTaskAttemptSchema(ctx, tx); !errors.Is(err, recoveryinventory.ErrTaskAttemptSchemaUnreviewed) {
		t.Fatalf("oversized table set accepted: %v", err)
	}
}

func wideColumns(count int) string {
	var columns []string
	for i := 0; i < count; i++ {
		columns = append(columns, fmt.Sprintf("col_%d TEXT", i))
	}
	return strings.Join(columns, ",")
}

func manyFKs(count int) string {
	var edges []string
	for i := 0; i < count; i++ {
		edges = append(edges, "FOREIGN KEY(id) REFERENCES projects(id)")
	}
	return strings.Join(edges, ",")
}
