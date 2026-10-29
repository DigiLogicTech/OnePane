// Package recoveryinventory holds private prerequisites for recovery observation.
// It grants no authority to settle a case, retry execution or verify an effect.
package recoveryinventory

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// This is an explicit reviewed structural contract, not a migration-number or
// schema-digest check. Each position records column name/type/nullability/PK
// ordinal/generated status, and every FK records grouping, source, target and
// update/delete semantics. STRICT/WITHOUT ROWID flags are bound too. It covers
// the direct Task/Attempt boundary ONLY. Reverse JSON, Worker steps, Operation,
// Team, polymorphic and delivery closure still need their own contracts.
// Defaults, CHECKs, triggers and unique/index definitions are not this slice's
// contract; a future collector still needs its complete semantic prerequisites.
//
//go:embed task_attempt_schema_v1.json
var taskAttemptSchemaJSON []byte

type schemaTable struct {
	Table        string             `json:"table"`
	Columns      []schemaColumn     `json:"columns"`
	ForeignKeys  []schemaForeignKey `json:"foreign_keys"`
	WithoutRowID int                `json:"without_rowid"`
	Strict       int                `json:"strict"`
}

type schemaColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    int    `json:"not_null"`
	PrimaryKey int    `json:"primary_key"`
	Hidden     int    `json:"hidden"`
}

type schemaForeignKey struct {
	From     string `json:"from"`
	Target   string `json:"target"`
	To       string `json:"to"`
	OnUpdate string `json:"on_update"`
	OnDelete string `json:"on_delete"`
	ID       int    `json:"id"`
	Sequence int    `json:"sequence"`
}

// Reviewed semantics are deliberately independent of the structural fixture.
// Shared context is never followed recursively as ownership. These families
// are all unsupported effects/context for the first constrained observation;
// describing an edge does not implement its probe or make a row eligible.
var taskAttemptFamilies = map[string]string{
	"tasks":                                 "original_and_descendant_history",
	"task_attempts":                         "all_historical_attempts_and_outgoing_snapshot",
	"task_dependencies":                     "incoming_and_outgoing_prerequisites_not_owned_tasks",
	"task_recovery_cases":                   "immutable_intake_not_effect_clearance",
	"agent_runtime_invocations":             "dispatch_by_task_and_attempt_plus_unimplemented_json_closure",
	"agent_worker_runs":                     "worker_by_task_and_attempt_plus_unimplemented_step_closure",
	"tool_invocations":                      "tool_dispatch_by_task_and_attempt",
	"operations":                            "effects_by_task_and_attempt_plus_compensation_and_lease_context",
	"recovery_snapshots":                    "snapshots_by_task_and_attempt_and_outgoing_attempt_reference",
	"assistant_project_handoffs":            "conversation_context_not_entire_project",
	"assistant_turns":                       "conversation_context_plus_reverse_provenance",
	"project_orchestrator_turns":            "orchestrator_context_plus_reverse_provenance",
	"assurance_runs":                        "verification_context_by_task_operation_and_worker",
	"budget_reservations":                   "accounting_by_task_and_reverse_dispatch",
	"capability_leases":                     "task_grants_plus_operation_references",
	"resource_leases":                       "task_resources_plus_operation_references",
	"checkpoints":                           "checkpoint_and_verification_context",
	"continuation_capsules":                 "task_session_and_checkpoint_context",
	"deliberations":                         "deliberation_context",
	"edit_proposals":                        "artifact_revision_and_proposal_context",
	"incidents":                             "task_and_operation_context",
	"inference_requests":                    "model_dispatch_plus_session_team_and_nested_request",
	"inference_sessions":                    "session_and_parent_session_context",
	"node_power_holds":                      "unconstrained_task_reference_keyed_by_lease_id",
	"node_wake_attempts":                    "unconstrained_task_reference_not_task_attempt",
	"plan_proposals":                        "task_and_deliberation_context",
	"project_change_proposals":              "project_change_and_patch_context",
	"project_goal_tcp_reachability_alerts":  "unrelated_tcp_probe_attempt_explicit_exception",
	"project_orchestrator_task_graph_nodes": "task_graph_context_composite_graph_node_key",
	"routine_occurrences":                   "routine_context",
	"runtime_execution_queue":               "unconstrained_task_and_request_reference",
	"task_execution_profiles":               "task_and_team_execution_context",
	"team_assignment_notices":               "task_checkpoint_message_context",
	"team_assistance_requests":              "immutable_original_task_attempt_worker_human_wait_context_not_resume_authority",
	"team_assignment_roots":                 "team_assignment_and_plan_context",
	"team_execution_assignments":            "task_and_parent_task_assignment_context_composite_key",
	"team_execution_held_tasks":             "held_task_context_composite_hold_task_key",
	"team_execution_holds":                  "root_task_hold_and_plan_context",
	"team_sessions":                         "team_session_context_not_entire_team",
	"verifications":                         "task_and_operation_verification_not_actual_effect_proof",
	"workspace_file_publications":           "publication_context_composite_task_path_hash_key",
}

// SchemaQueryer permits validation inside the caller's observation transaction.
// The caller must keep that transaction through collection; a validation result
// is not a reusable clearance token. No current collector calls this boundary.
type SchemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

var ErrTaskAttemptSchemaUnreviewed = errors.New("recovery Task/Attempt schema requires review")

// ValidateTaskAttemptSchema checks the reviewed direct-relationship slice with
// metadata only: it reads no task, prompt, secret or effect payload and writes
// nothing. Success says nothing about the still-unimplemented full inventory.
func ValidateTaskAttemptSchema(ctx context.Context, db SchemaQueryer) error {
	var expected []schemaTable
	if err := json.Unmarshal(taskAttemptSchemaJSON, &expected); err != nil {
		return fmt.Errorf("%w: invalid embedded contract", ErrTaskAttemptSchemaUnreviewed)
	}
	want := make(map[string]schemaTable, len(expected))
	for _, table := range expected {
		if taskAttemptFamilies[table.Table] == "" || want[table.Table].Table != "" {
			return fmt.Errorf("%w: unclassified contract table", ErrTaskAttemptSchemaUnreviewed)
		}
		want[table.Table] = table
	}
	if len(want) != len(taskAttemptFamilies) {
		return fmt.Errorf("%w: incomplete contract", ErrTaskAttemptSchemaUnreviewed)
	}
	tables, err := readMainSchemaTables(ctx, db)
	if err != nil {
		return err
	}
	for _, flags := range tables {
		name := flags.Table
		actual, relevant, err := readTaskAttemptTable(ctx, db, flags)
		if err != nil {
			return err
		}
		contract, reviewed := want[name]
		if !reviewed {
			if relevant {
				return fmt.Errorf("%w: unclassified table %s", ErrTaskAttemptSchemaUnreviewed, name)
			}
			continue
		}
		if !reflect.DeepEqual(actual, contract) {
			return fmt.Errorf("%w: changed table %s", ErrTaskAttemptSchemaUnreviewed, name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		return fmt.Errorf("%w: missing reviewed table", ErrTaskAttemptSchemaUnreviewed)
	}
	return nil
}

// Read main-table identity and flags together once per observation. table_list
// is a schema-wide PRAGMA; repeating it once for each table caused quadratic
// metadata work. No result survives this validation call or its transaction.
func readMainSchemaTables(ctx context.Context, db SchemaQueryer) ([]schemaTable, error) {
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(name AS BLOB))<=256 THEN name ELSE '' END,length(CAST(name AS BLOB)),wr,strict FROM main.pragma_table_list() WHERE schema='main' AND type IN ('table','virtual','shadow') AND name NOT GLOB 'sqlite_*' ORDER BY name LIMIT 513`)
	if err != nil {
		return nil, err
	}
	var tables []schemaTable
	for rows.Next() {
		var table schemaTable
		var size int
		if err = rows.Scan(&table.Table, &size, &table.WithoutRowID, &table.Strict); err != nil {
			break
		}
		if len(tables) == 512 || size > 256 {
			err = fmt.Errorf("%w: schema bounds exceeded", ErrTaskAttemptSchemaUnreviewed)
			break
		}
		tables = append(tables, table)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	return tables, err
}

func readTaskAttemptTable(ctx context.Context, db SchemaQueryer, flags schemaTable) (schemaTable, bool, error) {
	name := flags.Table
	table := schemaTable{Table: name, WithoutRowID: flags.WithoutRowID, Strict: flags.Strict, Columns: make([]schemaColumn, 0), ForeignKeys: make([]schemaForeignKey, 0)}
	relevant := name == "tasks" || name == "task_attempts"
	// Table identifiers are bound arguments to SQLite's table-valued PRAGMAs,
	// never interpolated SQL from an imported schema or external caller.
	rows, err := db.QueryContext(ctx, `SELECT name,type,"notnull",pk,hidden FROM pragma_table_xinfo(?, 'main') ORDER BY cid LIMIT 129`, name)
	if err != nil {
		return table, false, err
	}
	for rows.Next() {
		var column schemaColumn
		if err = rows.Scan(&column.Name, &column.Type, &column.NotNull, &column.PrimaryKey, &column.Hidden); err != nil {
			break
		}
		if len(table.Columns) == 128 || len(column.Name) > 256 || len(column.Type) > 256 {
			err = fmt.Errorf("%w: column bounds exceeded", ErrTaskAttemptSchemaUnreviewed)
			break
		}
		table.Columns = append(table.Columns, column)
		canonicalColumn := strings.ToLower(column.Name)
		if canonicalColumn == "task_id" || strings.HasSuffix(canonicalColumn, "_task_id") || canonicalColumn == "attempt_id" || strings.HasSuffix(canonicalColumn, "_attempt_id") {
			relevant = true
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return table, false, err
	}
	rows, err = db.QueryContext(ctx, `SELECT "from","table","to",on_update,on_delete,id,seq FROM pragma_foreign_key_list(?, 'main') ORDER BY id,seq LIMIT 129`, name)
	if err != nil {
		return table, false, err
	}
	for rows.Next() {
		var edge schemaForeignKey
		var target sql.NullString
		if err = rows.Scan(&edge.From, &edge.Target, &target, &edge.OnUpdate, &edge.OnDelete, &edge.ID, &edge.Sequence); err != nil {
			break
		}
		edge.To = target.String
		if len(table.ForeignKeys) == 128 || len(edge.From) > 256 || len(edge.Target) > 256 || len(edge.To) > 256 {
			err = fmt.Errorf("%w: foreign key bounds exceeded", ErrTaskAttemptSchemaUnreviewed)
			break
		}
		table.ForeignKeys = append(table.ForeignKeys, edge)
		if strings.EqualFold(edge.Target, "tasks") || strings.EqualFold(edge.Target, "task_attempts") {
			relevant = true
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	return table, relevant, err
}
