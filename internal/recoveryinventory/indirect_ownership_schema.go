package recoveryinventory

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// IndirectOwnershipSchemaVersion identifies a structural prerequisite only.
// It is not a row-attribution protocol, complete inventory or clearance token.
const IndirectOwnershipSchemaVersion = "onepane.recovery.indirect-ownership-schema.v1"

//go:embed indirect_ownership_schema_v1.json
var indirectOwnershipSchemaJSON []byte

var ErrIndirectOwnershipSchemaUnreviewed = errors.New("recovery indirect ownership schema requires review")

type indirectSchemaTable struct {
	schemaTable
	CreateSQL string `json:"create_sql"`
}

type indirectReferenceDescriptor struct {
	Columns []string
	Meaning string
}

// This reviewed register explains the ownership-bearing fields independently
// of the metadata fixture. Every listed association remains unsupported for a
// future constrained observation until separate bounded attribution probes and
// row-vocabulary contracts are implemented. Shared context never recursively
// owns all rows in its tenant, Project, Workspace or artifact session.
var indirectReferenceDescriptors = map[string][]indirectReferenceDescriptor{
	"agent_worker_steps": {
		{[]string{"run_id"}, "exact original Worker journal ownership"},
		{[]string{"step_kind", "status", "result_ref", "request_ref", "detail_json"}, "discriminated typed references: successful model -> agent response artifact; failed/unknown model -> inference request; runtime -> Invocation; tool -> Observation with invocation_id/observation_id; operation -> Operation; delegate -> child Task; verification waiting -> Verification, succeeded -> checkpoint except explicit retry; uncommitted response/accounting outcomes require result_ref_kind. Unsupported discriminator combinations defer"},
	},
	"artifacts":                      {{[]string{"metadata_json"}, "reverse JSON $.task_id,$.attempt_id,$.inference_request_id,$.agent_request_id,$.agent_runtime_invocation_id; orphan or disagreeing forward references remain relevant"}},
	"project_library_asset_versions": {{[]string{"asset_id", "version", "provenance_json", "storage_uri", "content_hash"}, "composite immutable identity; reverse JSON $.task_id,$.attempt_id,$.artifact_id; reused versions preserve original provenance, not exclusive Task ownership"}},
	"dependency_refs":                {{[]string{"owner_type", "owner_id", "target_type", "target_ref", "target_revision"}, "both polymorphic sides unsupported: no closed Task/Attempt/Worker/Operation vocabulary has been established; unknown strings cannot establish absence"}},
	"notification_deliveries": {
		{[]string{"source_event_id", "source_event_seq"}, "both independent Event pointers require union discovery and agreement when both present"},
		{[]string{"operation_id", "verification_id", "status", "message_json", "receipt_json"}, "external-send history and exact Operation/Verification context; terminal/unknown status does not exclude an external send"},
	},
	"events":                         {{[]string{"sequence", "id", "event_type", "aggregate_type", "aggregate_id", "payload_json"}, "exact Event IDs/sequences plus discriminated aggregate and typed payload references; event vocabulary and payload attribution are not implemented"}},
	"outbox_jobs":                    {{[]string{"job_type", "payload_json", "status", "claimed_by", "claim_expires_at"}, "durable lifecycle job/claim history; task.admission.evaluate and task.recovery.required need exact typed payload materialization, not blanket exclusion or clearance"}},
	"agent_runtime_invocations":      {{[]string{"response_artifact_id", "request_json"}, "forward response artifact plus independent request JSON Task/Attempt and Team-session/turn provenance; foreign/null columns do not hide reverse ownership"}},
	"inference_requests":             {{[]string{"response_artifact_id", "request_json", "context_manifest_json"}, "forward artifact plus nested ordinary Task/Attempt request and Team session/turn provenance; standalone classifier is not durable complete attribution"}},
	"approvals":                      {{[]string{"operation_id"}, "Operation approval context, never effect clearance"}},
	"operation_receipts":             {{[]string{"operation_id", "receipt_json"}, "Operation receipt history, never independent postcondition proof"}},
	"artifact_revisions":             {{[]string{"artifact_id", "session_id", "parent_revision_id"}, "exact revision/content and ancestry context; shared session does not own unrelated revisions"}},
	"artifact_sessions":              {{[]string{"source_artifact_id", "current_revision_id"}, "outgoing artifact/revision context; not recursive ownership of all session work"}},
	"assurance_runs":                 {{[]string{"worker_run_id", "operation_id", "verification_id", "result_json"}, "exact Worker/Operation and linked Verification specification; recorded assurance result is not independent recovery postcondition proof"}},
	"incidents":                      {{[]string{"operation_id"}, "Operation incident context"}},
	"operations":                     {{[]string{"compensates_operation_id"}, "incoming and outgoing compensation context; terminal status does not prove rollback"}},
	"project_change_proposals":       {{[]string{"patch_artifact_id"}, "exact patch artifact and proposal context"}},
	"project_workspace_publications": {{[]string{"asset_id", "asset_version", "content_hash", "link_id"}, "composite immutable Library version/hash with directional source/target link context"}},
	"task_recovery_cases":            {{[]string{"interruption_event_id", "source_job_id"}, "immutable intake Event and optional source-job provenance; not effect clearance"}},
	"tool_invocations":               {{[]string{"operation_id"}, "exact Operation tool dispatch context"}},
	"verifications":                  {{[]string{"operation_id", "spec_json", "result_json"}, "Operation verification context and specification evidence.artifact_ids/observation_ids/operation_ids/worker_run_id; not complete recovery decision/hash/postcondition binding"}},
}

var indirectOwnershipTargets = map[string]bool{
	"agent_worker_runs": true, "agent_worker_steps": true, "operations": true,
	"events": true, "artifacts": true, "project_library_asset_versions": true,
	"notification_deliveries": true, "outbox_jobs": true, "dependency_refs": true,
}

func indirectOwnershipRelevant(table schemaTable) bool {
	if _, ok := indirectReferenceDescriptors[table.Table]; ok {
		return true
	}
	if indirectOwnershipTargets[strings.ToLower(table.Table)] {
		return true
	}
	for _, edge := range table.ForeignKeys {
		if indirectOwnershipTargets[strings.ToLower(edge.Target)] {
			return true
		}
	}
	for _, column := range table.Columns {
		name := strings.ToLower(column.Name)
		for _, owner := range []string{"operation_id", "worker_run_id", "artifact_id", "event_id", "event_seq"} {
			if name == owner || strings.HasSuffix(name, "_"+owner) {
				return true
			}
		}
		if name == "run_id" {
			return true
		}
	}
	return false
}

// ValidateIndirectOwnershipSchema checks bounded main-schema metadata and the
// reviewed first indirect boundary. It reads no rows or effect payloads and
// writes nothing. Callers must retain the observation transaction; success
// cannot establish complete semantic attribution or authorize any recovery.
// Table DDL additionally binds CHECK/default/unique definitions; standalone
// indexes and triggers, closed JSON vocabularies and row semantics remain
// separate prerequisites. This function is intentionally not wired to a
// collector, daemon, public API, case phase or execution admission boundary.
func ValidateIndirectOwnershipSchema(ctx context.Context, db SchemaQueryer) error {
	var contract []indirectSchemaTable
	if err := json.Unmarshal(indirectOwnershipSchemaJSON, &contract); err != nil {
		return fmt.Errorf("%w: invalid embedded fixture", ErrIndirectOwnershipSchemaUnreviewed)
	}
	want := make(map[string]indirectSchemaTable, len(contract))
	for _, table := range contract {
		descriptors, ok := indirectReferenceDescriptors[table.Table]
		if !ok || want[table.Table].Table != "" || table.CreateSQL == "" {
			return fmt.Errorf("%w: unclassified fixture table", ErrIndirectOwnershipSchemaUnreviewed)
		}
		columns := make(map[string]bool, len(table.Columns))
		for _, col := range table.Columns {
			columns[col.Name] = true
		}
		for _, descriptor := range descriptors {
			for _, col := range descriptor.Columns {
				if !columns[col] {
					return fmt.Errorf("%w: descriptor column absent", ErrIndirectOwnershipSchemaUnreviewed)
				}
			}
		}
		want[table.Table] = table
	}
	if len(want) != len(indirectReferenceDescriptors) {
		return fmt.Errorf("%w: incomplete fixture", ErrIndirectOwnershipSchemaUnreviewed)
	}
	// Known direct tables provide structural classification for additional
	// unconstrained indirect-name paths (for example publication artifact_id).
	var direct []schemaTable
	if err := json.Unmarshal(taskAttemptSchemaJSON, &direct); err != nil {
		return fmt.Errorf("%w: invalid direct fixture", ErrIndirectOwnershipSchemaUnreviewed)
	}
	directWant := make(map[string]schemaTable, len(direct))
	for _, table := range direct {
		directWant[table.Table] = table
	}
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(name AS BLOB))<=256 THEN name ELSE '' END,length(CAST(name AS BLOB)) FROM main.sqlite_schema WHERE type='table' AND name NOT GLOB 'sqlite_*' LIMIT 513`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		var size int
		if err = rows.Scan(&name, &size); err != nil {
			break
		}
		if len(names) == 512 || size > 256 {
			err = fmt.Errorf("%w: table bounds exceeded", ErrIndirectOwnershipSchemaUnreviewed)
			break
		}
		names = append(names, name)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, name := range names {
		if err = indirectMetadataBounds(ctx, db, name); err != nil {
			return err
		}
		actual, _, e := readTaskAttemptTable(ctx, db, name)
		if e != nil {
			return errors.Join(ErrIndirectOwnershipSchemaUnreviewed, e)
		}
		expected, reviewed := want[name]
		if reviewed {
			if !reflect.DeepEqual(actual, expected.schemaTable) {
				return fmt.Errorf("%w: changed table %s", ErrIndirectOwnershipSchemaUnreviewed, name)
			}
			ddl, e := indirectTableDDL(ctx, db, name)
			if e != nil {
				return e
			}
			if ddl != expected.CreateSQL {
				return fmt.Errorf("%w: changed constraints on %s", ErrIndirectOwnershipSchemaUnreviewed, name)
			}
			delete(want, name)
		} else if indirectOwnershipRelevant(actual) {
			known, ok := directWant[name]
			if !ok || !reflect.DeepEqual(actual, known) {
				return fmt.Errorf("%w: unclassified indirect owner %s", ErrIndirectOwnershipSchemaUnreviewed, name)
			}
		}
	}
	if len(want) != 0 {
		return fmt.Errorf("%w: missing reviewed table", ErrIndirectOwnershipSchemaUnreviewed)
	}
	return nil
}

// Check sizes inside SQLite before the shared metadata reader copies names or
// types into Go. Limit-plus-one prevents arbitrary wide imported schemas from
// escaping the existing per-table column/FK budget.
func indirectMetadataBounds(ctx context.Context, db SchemaQueryer, name string) error {
	for _, query := range []string{
		`SELECT COUNT(*),COALESCE(MAX(length(CAST(name AS BLOB))),0),COALESCE(MAX(length(CAST(type AS BLOB))),0),0 FROM (SELECT name,type FROM pragma_table_xinfo(?,'main') LIMIT 129)`,
		`SELECT COUNT(*),COALESCE(MAX(length(CAST("from" AS BLOB))),0),COALESCE(MAX(length(CAST("table" AS BLOB))),0),COALESCE(MAX(length(CAST("to" AS BLOB))),0) FROM (SELECT "from","table","to" FROM pragma_foreign_key_list(?,'main') LIMIT 129)`,
	} {
		rows, err := db.QueryContext(ctx, query, name)
		if err != nil {
			return err
		}
		var count, a, b, c int
		if !rows.Next() {
			err = errors.Join(rows.Err(), rows.Close())
			if err != nil {
				return err
			}
			return ErrIndirectOwnershipSchemaUnreviewed
		}
		err = rows.Scan(&count, &a, &b, &c)
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		if count > 128 || a > 256 || b > 256 || c > 256 {
			return fmt.Errorf("%w: column or FK bounds exceeded", ErrIndirectOwnershipSchemaUnreviewed)
		}
	}
	return nil
}

func indirectTableDDL(ctx context.Context, db SchemaQueryer, name string) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(sql AS BLOB))<=32768 THEN sql ELSE '' END,length(CAST(sql AS BLOB)) FROM main.sqlite_schema WHERE type='table' AND name=?`, name)
	if err != nil {
		return "", err
	}
	var ddl string
	var size int
	if !rows.Next() {
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return "", err
		}
		return "", ErrIndirectOwnershipSchemaUnreviewed
	}
	err = rows.Scan(&ddl, &size)
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return "", err
	}
	if size > 32768 {
		return "", fmt.Errorf("%w: DDL bounds exceeded", ErrIndirectOwnershipSchemaUnreviewed)
	}
	return strings.TrimSpace(strings.ReplaceAll(ddl, "\r\n", "\n")), nil
}
