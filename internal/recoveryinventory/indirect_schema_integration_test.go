//go:build integration

package recoveryinventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A contract fixture is reviewed source. Integration tests must never regenerate
// it: automatic capture would bless schema changes before review, and the old
// retained authoring test omitted the required table DDL entirely.
func TestRecoveryIndirectOwnershipSchemaRejectsChanges(t *testing.T) {
	ctx := context.Background()
	db := OpenMigratedRecoveryFixture(t, ctx)
	var err error
	if err = ValidateIndirectOwnershipSchema(ctx, db.SQL()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sql string }{
		{"new_operation_fk", `CREATE TABLE new_effect(owner TEXT REFERENCES operations(id))`},
		{"case_insensitive_worker_fk", `CREATE TABLE new_effect(owner TEXT REFERENCES AGENT_WORKER_RUNS(id))`},
		{"new_event_fk", `CREATE TABLE new_effect(owner INTEGER REFERENCES events(sequence))`},
		{"new_artifact_fk", `CREATE TABLE new_effect(owner TEXT REFERENCES artifacts(id))`},
		{"new_composite_library_fk", `CREATE TABLE new_effect(owner TEXT,version INTEGER,FOREIGN KEY(owner,version) REFERENCES project_library_asset_versions(asset_id,version))`},
		{"new_operation_name", `CREATE TABLE new_effect(operation_id TEXT)`},
		{"new_prefixed_operation_name", `CREATE TABLE new_effect(compensates_operation_id TEXT)`},
		{"new_worker_name", `CREATE TABLE new_effect(worker_run_id TEXT)`},
		{"new_run_name", `CREATE TABLE new_effect(run_id TEXT)`},
		{"new_event_id", `CREATE TABLE new_effect(source_event_id TEXT)`},
		{"new_event_sequence", `CREATE TABLE new_effect(source_event_seq INTEGER)`},
		{"new_artifact_name", `CREATE TABLE new_effect(RESULT_ARTIFACT_ID TEXT)`},
		{"sqlite_wildcard_name", `CREATE TABLE sqliteXeffects(operation_id TEXT)`},
		{"quoted_name", `CREATE TABLE "bad'; DROP TABLE operations; --"(operation_id TEXT)`},
		{"changed_existing_unrelated_table", `ALTER TABLE notifications ADD COLUMN operation_id TEXT`},
		{"changed_direct_only_owner", `ALTER TABLE agent_worker_runs ADD COLUMN new_effect_json TEXT`},
		{"new_worker_step_payload", `ALTER TABLE agent_worker_steps ADD COLUMN new_effect_json TEXT`},
		{"renamed_composite_key", `ALTER TABLE project_library_asset_versions RENAME COLUMN version TO version_number`},
		{"changed_event_pointer", `ALTER TABLE notification_deliveries RENAME COLUMN source_event_seq TO another_sequence`},
		{"generated_column", `ALTER TABLE operation_receipts ADD COLUMN shadow TEXT GENERATED ALWAYS AS (operation_id) VIRTUAL`},
		{"missing_polymorphic_table", `DROP TABLE dependency_refs`},
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
			if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
				t.Fatalf("unreviewed indirect ownership accepted: %v", err)
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err = ValidateIndirectOwnershipSchema(ctx, db.SQL()); err != nil {
				t.Fatalf("rollback changed contract: %v", err)
			}
		})
	}
	// Table DDL catches semantic constraints that column/FK metadata cannot:
	// defaults, CHECK vocabulary and inline uniqueness. Standalone indexes and
	// triggers remain explicitly outside this prerequisite.
	for _, tc := range []struct{ name, old, next string }{
		{"changed_default", "DEFAULT 5", "DEFAULT 6"},
		{"changed_check", "max_attempts >= 1", "max_attempts >= 0"},
		{"changed_inline_unique", "UNIQUE(workspace_id,idempotency_key)", "CHECK(length(idempotency_key)>0)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.SQL().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(ctx, `PRAGMA writable_schema=ON`); err != nil {
				t.Fatal(err)
			}
			defer tx.ExecContext(ctx, `PRAGMA writable_schema=OFF`)
			res, err := tx.ExecContext(ctx, `UPDATE main.sqlite_schema SET sql=replace(sql,?,?) WHERE type='table' AND name='notification_deliveries' AND instr(sql,?)>0`, tc.old, tc.next, tc.old)
			if err != nil {
				t.Fatal(err)
			}
			count, err := res.RowsAffected()
			if err != nil || count != 1 {
				t.Fatalf("fixture DDL was not changed: %d %v", count, err)
			}
			if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
				t.Fatalf("changed constraints accepted: %v", err)
			}
		})
	}
	t.Run("ddl_line_endings", func(t *testing.T) {
		tx, err := db.SQL().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `PRAGMA writable_schema=ON`); err != nil {
			t.Fatal(err)
		}
		defer tx.ExecContext(ctx, `PRAGMA writable_schema=OFF`)
		if _, err = tx.ExecContext(ctx, `UPDATE main.sqlite_schema SET sql=replace(replace(sql,char(13)||char(10),char(10)),char(10),char(13)||char(10)) WHERE name='dependency_refs'`); err != nil {
			t.Fatal(err)
		}
		if err = ValidateIndirectOwnershipSchema(ctx, tx); err != nil {
			t.Fatalf("equivalent CRLF DDL rejected: %v", err)
		}
	})
	// Shared configuration scopes do not recursively own unrelated Project rows;
	// a temporary shadow cannot change the explicitly main-schema contract.
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE shared_config(project_id TEXT REFERENCES projects(id),workspace_id TEXT REFERENCES workspaces(id)); CREATE TEMP TABLE agent_worker_steps(wrong_shape TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err = ValidateIndirectOwnershipSchema(ctx, tx); err != nil {
		t.Fatalf("shared scope or temporary shadow changed ownership: %v", err)
	}
}

func TestRecoveryIndirectOwnershipSchemaBoundsAndReadOnly(t *testing.T) {
	ctx := context.Background()
	db := OpenMigratedRecoveryFixture(t, ctx)
	var err error
	conn, err := db.SQL().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	if err = ValidateIndirectOwnershipSchema(ctx, conn); err != nil {
		t.Fatalf("read-only validation: %v", err)
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	var columns []string
	for i := 0; i < 129; i++ {
		columns = append(columns, fmt.Sprintf("column_%d TEXT", i))
	}
	var edges []string
	for i := 0; i < 129; i++ {
		edges = append(edges, "FOREIGN KEY(owner) REFERENCES projects(id)")
	}
	for _, tc := range []struct{ name, sql string }{
		{"identifier_bytes", fmt.Sprintf(`CREATE TABLE "%s"(operation_id TEXT)`, strings.Repeat("界", 86))},
		{"column_identifier_bytes", fmt.Sprintf(`CREATE TABLE new_effect("%s" TEXT)`, strings.Repeat("界", 86))},
		{"type_bytes", fmt.Sprintf(`CREATE TABLE new_effect(owner %s)`, strings.Repeat("x", 257))},
		{"column_count", `CREATE TABLE new_effect(` + strings.Join(columns, ",") + `)`},
		{"fk_count", `CREATE TABLE new_effect(owner TEXT,` + strings.Join(edges, ",") + `)`},
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
			if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
				t.Fatalf("oversized metadata accepted: %v", err)
			}
		})
	}
	t.Run("ddl_bytes", func(t *testing.T) {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `PRAGMA writable_schema=ON`); err != nil {
			t.Fatal(err)
		}
		defer tx.ExecContext(ctx, `PRAGMA writable_schema=OFF`)
		if _, err = tx.ExecContext(ctx, `UPDATE main.sqlite_schema SET sql=sql||? WHERE name='dependency_refs'`, strings.Repeat(" ", 32769)); err != nil {
			t.Fatal(err)
		}
		if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
			t.Fatalf("oversized DDL accepted: %v", err)
		}
	})
	t.Run("table_count", func(t *testing.T) {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < 513; i++ {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE unrelated_%d(id TEXT)`, i)); err != nil {
				t.Fatal(err)
			}
		}
		if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
			t.Fatalf("oversized schema accepted: %v", err)
		}
	})
}
