//go:build integration

package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// Reverse provenance is conservative contradictory history, not proof that a
// fabricated test artifact or invocation represents a real external effect.
// Its presence alone must prevent the automatic "never started" settlement.
func TestUnstartedCancellationRejectsReverseTaskProvenance(t *testing.T) {
	for _, kind := range []string{"runtime_request", "model_agent_request", "artifact_metadata", "library_version"} {
		for _, direct := range []string{"absent", "foreign"} {
			t.Run(kind+"/"+direct, func(t *testing.T) {
				db, svc := cancellationFixture(t)
				ctx := context.Background()
				cancellingTask(t, db, svc, "original", "", "ws")
				if _, err := db.SQL().Exec(`INSERT INTO tasks(id,workspace_id,objective,state,completion_json,revision,created_at,updated_at) VALUES('foreign-task','foreign','Unrelated history','created','{}',1,1,1)`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.SQL().Exec(`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('principal','agent','Worker','active',1,1)`); err != nil {
					t.Fatal(err)
				}
				var directTask any
				provenanceWorkspace := "ws"
				if direct == "foreign" {
					directTask = "foreign-task"
					provenanceWorkspace = "foreign"
				}
				req := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "historical-request", WorkspaceID: "ws", TaskID: "original", AttemptID: "unrecorded-original-attempt", PrincipalID: "principal", Objective: "Preserve contradictory historical provenance", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalWait}}
				var query string
				switch kind {
				case "runtime_request":
					if _, err := db.SQL().Exec(`INSERT INTO agent_runtime_connections(id,workspace_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,created_at,updated_at) VALUES('connection','foreign','test','Test','test','1','{}','none','registered','untrusted','proposal_only','{}','{}','{}',1,1)`); err != nil {
						t.Fatal(err)
					}
					raw, err := json.Marshal(req)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = db.SQL().Exec(`INSERT INTO agent_runtime_invocations(id,workspace_id,task_id,principal_id,connection_id,status,request_json,created_at,updated_at) VALUES('historical','foreign',?,'principal','connection','succeeded',?,1,1)`, directTask, string(raw)); err != nil {
						t.Fatal(err)
					}
					query = `SELECT json_object('workspace',workspace_id,'task',task_id,'status',status,'request',request_json) FROM agent_runtime_invocations WHERE id='historical'`
				case "model_agent_request":
					raw, err := agentworker.BuildModelRequest(req, "L1")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = db.SQL().Exec(`INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) VALUES('historical','foreign',?,'principal','succeeded','{}','{}','{}',?,1,1)`, directTask, string(raw)); err != nil {
						t.Fatal(err)
					}
					query = `SELECT json_object('workspace',workspace_id,'task',task_id,'status',status,'request',request_json) FROM inference_requests WHERE id='historical'`
				case "artifact_metadata":
					// No publication/request caller link exists. The artifact has
					// already committed and its Task identity survives in metadata.
					metadata := `{"task_id":"original","attempt_id":"unrecorded-original-attempt"}`
					if _, err := db.SQL().Exec(`INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_at) VALUES('historical',?,'sha256:test','application/json',1,'not-read','internal','any','unverified_derived','archived',?,1)`, provenanceWorkspace, metadata); err != nil {
						t.Fatal(err)
					}
					query = `SELECT json_object('workspace',workspace_id,'status',status,'metadata',metadata_json,'storage',storage_ref) FROM artifacts WHERE id='historical'`
				case "library_version":
					if _, err := db.SQL().Exec(`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('project',?,'History','active','{}','{}','principal',1,1)`, provenanceWorkspace); err != nil {
						t.Fatal(err)
					}
					if _, err := db.SQL().Exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,created_at,updated_at) VALUES('asset','project','History','file',1,1)`); err != nil {
						t.Fatal(err)
					}
					if _, err := db.SQL().Exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,storage_uri,provenance_json,created_at) VALUES('asset',1,'sha256:test','not-read','{"task_id":"original","attempt_id":"unrecorded-original-attempt"}',1)`); err != nil {
						t.Fatal(err)
					}
					query = `SELECT json_object('asset',asset_id,'version',version,'provenance',provenance_json,'storage',storage_uri) FROM project_library_asset_versions WHERE asset_id='asset' AND version=1`
				}
				var before string
				if err := db.SQL().QueryRow(query).Scan(&before); err != nil {
					t.Fatal(err)
				}
				for pass := 0; pass < 2; pass++ {
					// Reconstruct the service: an in-memory cursor must not be
					// the sole reason contradictory evidence is retained.
					svc = task.NewService(db.SQL(), db, clock.Real{})
					n, err := svc.SettleUnstartedCancellations(ctx, 16)
					if err != nil {
						t.Fatal(err)
					}
					if n != 0 {
						t.Errorf("reverse %s provenance allowed terminal cancellation on pass%d: count=%d", kind, pass, n)
					}
					row, err := svc.Get(ctx, "original")
					if err != nil {
						t.Fatal(err)
					}
					if row.State != task.StateCancelRequested || row.Revision != 2 {
						t.Errorf("original changed: state=%s revision=%d", row.State, row.Revision)
					}
					if _, _, err = svc.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision}); !errors.Is(err, task.ErrInvalidTransition) {
						t.Errorf("cancelled-intent admission fence lost: %v", err)
					}
					var after string
					if err = db.SQL().QueryRow(query).Scan(&after); err != nil || after != before {
						t.Errorf("historical provenance modified: %v", err)
					}
					for _, q := range []string{`SELECT COUNT(*) FROM task_attempts WHERE task_id='original'`, `SELECT COUNT(*) FROM events WHERE aggregate_id='original' AND event_type='task.cancelled'`} {
						var count int
						if err = db.SQL().QueryRow(q).Scan(&count); err != nil || count != 0 {
							t.Errorf("invented Attempt or cancellation audit (%s): %d %v", q, count, err)
						}
					}
				}
				// The unrelated Task remains intact as well.
				row, err := svc.Get(ctx, "foreign-task")
				if err != nil || row.State != task.StateCreated || row.Revision != 1 {
					t.Errorf("foreign Task changed: %+v %v", row, err)
				}
			})
		}
	}
}
