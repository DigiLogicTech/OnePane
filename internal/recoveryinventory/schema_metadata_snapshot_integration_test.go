//go:build integration

package recoveryinventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type countingSchemaQueryer struct {
	SchemaQueryer
	flagReads int
}

func TestRecoverySchemaSnapshotPreservesFlagsAndFreshness(t *testing.T) {
	ctx := context.Background()
	db := OpenMigratedRecoveryFixture(t, ctx)
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE snapshot_strict(id TEXT PRIMARY KEY) STRICT;
	 CREATE TABLE snapshot_without(id TEXT PRIMARY KEY) WITHOUT ROWID;
	 CREATE VIEW snapshot_view AS SELECT id FROM snapshot_strict;
	 CREATE TEMP TABLE snapshot_strict(wrong_shape TEXT);
	 CREATE TEMP TABLE pragma_table_list(schema TEXT,name TEXT,type TEXT,wr INTEGER,strict INTEGER)`); err != nil {
		t.Fatal(err)
	}
	tables, err := readMainSchemaTables(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	flags := make(map[string]schemaTable)
	for _, table := range tables {
		flags[table.Table] = table
	}
	if flags["snapshot_strict"].Strict != 1 || flags["snapshot_strict"].WithoutRowID != 0 || flags["snapshot_without"].WithoutRowID != 1 || flags["snapshot_without"].Strict != 0 {
		t.Fatal("main STRICT/WITHOUT ROWID metadata was lost or shadowed")
	}
	if _, present := flags["snapshot_view"]; present {
		t.Fatal("view acquired table ownership")
	}
	for _, validate := range []struct {
		name  string
		check func(context.Context, SchemaQueryer) error
	}{{"direct", ValidateTaskAttemptSchema}, {"indirect", ValidateIndirectOwnershipSchema}} {
		if err = validate.check(ctx, tx); err != nil {
			t.Fatalf("unrelated flags and temp shadows denied %s: %v", validate.name, err)
		}
	}
	// Validation has no cached clearance. New ownership introduced after a
	// successful observation must be found despite the same connection/tx.
	if _, err = tx.ExecContext(ctx, `ALTER TABLE main.snapshot_strict ADD COLUMN task_id TEXT; ALTER TABLE main.snapshot_strict ADD COLUMN operation_id TEXT`); err != nil {
		t.Fatal(err)
	}
	if err = ValidateTaskAttemptSchema(ctx, tx); !errors.Is(err, ErrTaskAttemptSchemaUnreviewed) {
		t.Fatalf("new direct ownership escaped fresh snapshot: %v", err)
	}
	if err = ValidateIndirectOwnershipSchema(ctx, tx); !errors.Is(err, ErrIndirectOwnershipSchemaUnreviewed) {
		t.Fatalf("new indirect ownership escaped fresh snapshot: %v", err)
	}
}

func (q *countingSchemaQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(strings.ToLower(query), "pragma_table_list") {
		q.flagReads++
	}
	return q.SchemaQueryer.QueryContext(ctx, query, args...)
}

// table_list builds metadata for the schema. Calling it for every table
// repeats the entire schema walk under the SQLite race-instrumented VM.
// Test the production readers against actual main/temp schema, including new
// unrelated tables, not a fabricated metadata result or an extended deadline.
func TestRecoverySchemaFlagsReadOncePerObservation(t *testing.T) {
	ctx := context.Background()
	db := OpenMigratedRecoveryFixture(t, ctx)
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 64; i++ {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE unrelated_metadata_%d(id TEXT PRIMARY KEY, value TEXT)`, i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE tasks(wrong_shape TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, validate := range []struct {
		name  string
		check func(context.Context, SchemaQueryer) error
	}{{"direct", ValidateTaskAttemptSchema}, {"indirect", ValidateIndirectOwnershipSchema}} {
		t.Run(validate.name, func(t *testing.T) {
			queryer := &countingSchemaQueryer{SchemaQueryer: tx}
			if err := validate.check(ctx, queryer); err != nil {
				t.Fatal(err)
			}
			if queryer.flagReads != 1 {
				t.Fatalf("schema observation repeated whole-schema flags %d times; want one snapshot", queryer.flagReads)
			}
		})
	}
}
