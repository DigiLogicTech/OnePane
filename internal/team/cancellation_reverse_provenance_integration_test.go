package team

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// These are conservative negative cancellation guards. Persisted reverse
// provenance can contradict a claim of pristine execution even when a caller's
// forward association never committed. This is not a complete effect inventory
// or physical model/external-effect acceptance.
func TestTeamCancellationDefersReverseRootProvenance(t *testing.T) {
	for _, kind := range []string{"runtime", "artifact", "library", "ordinary_model"} {
		for _, foreign := range []bool{false, true} {
			name := kind + "_null_forward_owner"
			if foreign {
				name = kind + "_foreign_forward_owner"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				s, db, ss, requestID, _ := cancellationFixture(t)
				exec := func(q string, args ...any) {
					t.Helper()
					if _, err := db.SQL().Exec(q, args...); err != nil {
						t.Fatal(err)
					}
				}
				var directOwner any
				artifactWorkspace := ss.WorkspaceID
				if foreign {
					exec(`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('reverse-foreign-tenant','Independent foreign tenant','active',1,1)`)
					other, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "reverse-foreign-tenant", Objective: "Independent foreign owner"})
					if err != nil {
						t.Fatal(err)
					}
					directOwner = other.ID
					artifactWorkspace = "reverse-foreign-tenant"
				}
				body, err := json.Marshal(map[string]any{"protocol_version": "v1", "request_id": "orphan-agent-request", "task_id": ss.TaskID, "attempt_id": "historical-unlinked-attempt"})
				if err != nil {
					t.Fatal(err)
				}
				payload := string(body)
				var evidenceQuery string
				switch kind {
				case "runtime":
					exec(`INSERT INTO agent_runtime_connections(id,workspace_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,created_at,updated_at) VALUES('reverse-runtime',?,'test','External','test','1','{}','none','connected','user_trusted','proposal_only','{}','{}','{}',1,1)`, artifactWorkspace)
					// Neither nullable direct Task/Attempt nor Team session/turn
					// metadata identifies the root; only the full request does.
					exec(`INSERT INTO agent_runtime_invocations(id,workspace_id,task_id,principal_id,connection_id,status,request_json,created_at,updated_at,completed_at) VALUES('reverse-root-effect',?,?,'human','reverse-runtime','succeeded',?,1,1,2)`, artifactWorkspace, directOwner, payload)
					evidenceQuery = `SELECT request_json FROM agent_runtime_invocations WHERE id='reverse-root-effect'`
				case "artifact":
					// No response/publication/Worker forward link exists. The
					// artifact's reverse Task provenance committed independently.
					exec(`INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_at) VALUES('reverse-root-effect',?,'sha256:preserved','application/octet-stream',1,'not-read','internal','any','unverified_derived','archived',?,1)`, artifactWorkspace, payload)
					evidenceQuery = `SELECT metadata_json FROM artifacts WHERE id='reverse-root-effect'`
				case "library":
					exec(`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('reverse-root-project',?,'Independent Library','active','{}','{}','human',1,1)`, artifactWorkspace)
					exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,created_at,updated_at) VALUES('reverse-root-asset','reverse-root-project','Unlinked version','file',1,1)`)
					exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,storage_uri,provenance_json,created_at) VALUES('reverse-root-asset',1,'sha256:preserved','not-read',?,1)`, payload)
					evidenceQuery = `SELECT provenance_json FROM project_library_asset_versions WHERE asset_id='reverse-root-asset' AND version=1`
				case "ordinary_model":
					outer, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "system", "content": "Bounded OnePane worker"}, {"role": "user", "content": payload}}})
					if err != nil {
						t.Fatal(err)
					}
					payload = string(outer)
					// Separate from the fixture's genuine attributed local Team
					// reasoning request; neither direct Task nor Team context is owned.
					exec(`INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at,completed_at) VALUES('reverse-root-effect',?,?,'human','succeeded','{}','{}','{}',?,1,1,2)`, artifactWorkspace, directOwner, payload)
					evidenceQuery = `SELECT request_json FROM inference_requests WHERE id='reverse-root-effect'`
				}
				beforeRoot, err := s.tasks.Get(ctx, ss.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				beforeSession, err := s.Session(ctx, ss.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, service := range []*Service{s, NewService(db.SQL(), db, clock.Real{}, s.tasks)} {
					if n, err := service.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
						t.Fatalf("unsafe settlement ignored %s reverse root provenance: settled=%d err=%v", kind, n, err)
					}
					root, err := s.tasks.Get(ctx, ss.TaskID)
					if err != nil || root.State != task.StateCancelRequested || !reflect.DeepEqual(beforeRoot, root) {
						t.Fatalf("deferred root was changed: %+v err=%v", root, err)
					}
					current, err := s.Session(ctx, ss.ID)
					if err != nil || !reflect.DeepEqual(beforeSession, current) {
						t.Fatalf("deferred Team session was changed: %+v err=%v", current, err)
					}
				}
				var preserved string
				if err = db.SQL().QueryRow(evidenceQuery).Scan(&preserved); err != nil || preserved != payload {
					t.Fatalf("reverse provenance changed: %q err=%v", preserved, err)
				}
				if err = db.SQL().QueryRow(`SELECT status FROM inference_requests WHERE id=?`, requestID).Scan(&preserved); err != nil || preserved != "succeeded" {
					t.Fatalf("completed local reasoning changed: %s err=%v", preserved, err)
				}
				var pending, succeeded, audits int
				if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='pending'`, ss.ID).Scan(&pending); err != nil || pending != 1 {
					t.Fatalf("pending turn changed: %d err=%v", pending, err)
				}
				if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='succeeded'`, ss.ID).Scan(&succeeded); err != nil || succeeded != 1 {
					t.Fatalf("completed turn changed: %d err=%v", succeeded, err)
				}
				if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type IN ('team.execution_cancelled','team.pending_turns_cancelled')`, ss.ID).Scan(&audits); err != nil || audits != 0 {
					t.Fatalf("deferred cancellation appended terminal audit: %d err=%v", audits, err)
				}
			})
		}
	}
}
