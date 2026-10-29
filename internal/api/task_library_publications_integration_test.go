//go:build integration

package api

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func publishedReceiptTask(t *testing.T, f *publishedLibraryFixture) string {
	t.Helper()
	var task string
	if err := f.db.SQL().QueryRow(`SELECT task_id FROM workspace_file_publications WHERE library_asset_id=?`, f.assetID).Scan(&task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskLibraryPublicationsProductionReceiptProbeIsBounded(t *testing.T) {
	f := publishedLibraryHTTPFixture(t, nil)
	taskID := publishedReceiptTask(t, f)
	other, err := task.NewService(f.db.SQL(), f.db, clock.Real{}).Create(context.Background(), task.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, ProjectWorkspaceID: &f.workspaceID, Objective: "Unrelated publication history"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.SQL().Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024)
 INSERT INTO workspace_file_publications(task_id,project_id,project_workspace_id,runtime_id,application_id,relative_path,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at)
 SELECT ?,project_id,project_workspace_id,runtime_id,application_id,'history/'||x,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at
 FROM workspace_file_publications,n WHERE task_id=?`, other.ID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "../projectworkspace/task_library_publications.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	ast.Inspect(parsed, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || v.Names[0].Name != "taskLibraryReceiptsSQL" {
			return true
		}
		literal, ok := v.Values[0].(*ast.BasicLit)
		if !ok {
			t.Fatal("production receipt query is not a literal")
		}
		query, err = strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		return false
	})
	if query == "" {
		t.Fatal("production receipt query not found")
	}
	for _, stats := range []bool{false, true} {
		if stats {
			if _, err = f.db.SQL().Exec(`ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		rows, e := f.db.SQL().Query("EXPLAIN QUERY PLAN "+query, f.projectID, f.workspaceID, taskID)
		if e != nil {
			t.Fatal(e)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if e = rows.Scan(&id, &parent, &unused, &detail); e != nil {
				t.Fatal(e)
			}
			fmt.Fprintln(&plan, detail)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		if !strings.Contains(plan.String(), "SEARCH workspace_file_publications USING INDEX") || !strings.Contains(plan.String(), "(task_id=?)") || strings.Contains(plan.String(), "SCAN ") || strings.Contains(plan.String(), "TEMP B-TREE") || strings.Contains(plan.String(), "AUTOMATIC") {
			t.Fatalf("unbounded production receipts (stats=%v): %s", stats, plan.String())
		}
		out, e := f.projects.TaskLibraryPublications(context.Background(), f.projectID, f.workspaceID, taskID)
		if e != nil || len(out) != 1 {
			t.Fatalf("unrelated history displaced exact Task: %d %v", len(out), e)
		}
	}
	if _, err = f.db.SQL().Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.projects.TaskLibraryPublications(context.Background(), f.projectID, f.workspaceID, taskID); err != nil {
		t.Fatal(err)
	}
}

func TestTaskLibraryPublicationsUseActualPublisherReceiptsAndCurrentGrants(t *testing.T) {
	for _, scenario := range []string{"allowed", "historical_stopped_source", "wrong_runtime", "wrong_application", "runtime_scope_changed", "no_receipts", "unknown_receipt", "wrong_receipt_scope", "wrong_hash", "wrong_storage", "wrong_artifact", "archived_asset", "archived_artifact", "quarantined_artifact", "artifact_wrong_size", "artifact_wrong_hash", "revoked_grant", "stale_latest", "wrong_pin", "project_archived", "workspace_archived", "task_archived", "oversized_path", "utf8_bytes", "bad_path", "oversized_mime", "invalid_utf8", "control_path", "control_mime"} {
		t.Run(scenario, func(t *testing.T) {
			f := publishedLibraryHTTPFixture(t, nil)
			task := publishedReceiptTask(t, f)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.db.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "historical_stopped_source":
				exec(`UPDATE project_runtimes SET status='stopped'`)
				exec(`UPDATE project_applications SET status='stopped'`)
			case "wrong_runtime", "wrong_application", "runtime_scope_changed":
				other, err := f.projects.CreateWorkspaceView(context.Background(), projectworkspace.CreateWorkspaceViewCommand{ProjectID: f.projectID, Name: "Other source", ActorPrincipalID: "human"})
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "runtime_scope_changed" {
					exec(`UPDATE project_runtimes SET project_workspace_id=? WHERE id=(SELECT runtime_id FROM workspace_file_publications WHERE task_id=?)`, other.ID, task)
					break
				}
				runtime, err := f.projects.CreateRuntime(context.Background(), projectworkspace.CreateRuntimeCommand{ProjectID: f.projectID, ProjectWorkspaceID: &other.ID, CreatedBy: "human"})
				if err != nil {
					t.Fatal(err)
				}
				app, err := f.projects.DeclareApplication(context.Background(), projectworkspace.DeclareApplicationCommand{RuntimeID: runtime.ID, Name: "Other publisher", SourceKind: projectworkspace.AppOCIImage, SourceRef: "example.invalid/other@sha256:" + strings.Repeat("1", 64), CreatedBy: "human"})
				if err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "wrong_runtime":
					exec(`UPDATE workspace_file_publications SET runtime_id=? WHERE task_id=?`, runtime.ID, task)
				case "wrong_application":
					exec(`UPDATE workspace_file_publications SET application_id=? WHERE task_id=?`, app.ID, task)
				}
			case "no_receipts":
				exec(`DELETE FROM workspace_file_publications WHERE task_id=?`, task)
			case "unknown_receipt":
				exec(`UPDATE workspace_file_publications SET status='in_progress',library_asset_id=NULL,asset_version=NULL WHERE task_id=?`, task)
			case "wrong_receipt_scope":
				other, err := f.projects.CreateWorkspaceView(context.Background(), projectworkspace.CreateWorkspaceViewCommand{ProjectID: f.projectID, Name: "Other", ActorPrincipalID: "human"})
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE workspace_file_publications SET project_workspace_id=? WHERE task_id=?`, other.ID, task)
			case "wrong_hash":
				exec(`UPDATE workspace_file_publications SET content_hash=printf('%064x',1) WHERE task_id=?`, task)
			case "wrong_storage":
				exec(`UPDATE project_library_asset_versions SET storage_uri='https://private.invalid/blob' WHERE asset_id=?`, f.assetID)
			case "wrong_artifact":
				exec(`UPDATE workspace_file_publications SET artifact_id='missing' WHERE task_id=?`, task)
			case "archived_asset":
				exec(`UPDATE project_library_assets SET archived=1 WHERE id=?`, f.assetID)
			case "archived_artifact":
				exec(`UPDATE artifacts SET status='archived' WHERE id=?`, f.artifactID)
			case "quarantined_artifact":
				exec(`UPDATE artifacts SET status='quarantined' WHERE id=?`, f.artifactID)
			case "artifact_wrong_size":
				exec(`UPDATE artifacts SET size_bytes=size_bytes+1 WHERE id=?`, f.artifactID)
			case "artifact_wrong_hash":
				exec(`UPDATE artifacts SET content_hash='sha256:'||printf('%064x',1) WHERE id=?`, f.artifactID)
			case "revoked_grant":
				exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID)
			case "stale_latest":
				exec(`UPDATE project_library_assets SET current_version=2 WHERE id=?`, f.assetID)
			case "wrong_pin":
				exec(`UPDATE workspace_library_grants SET version_policy='pinned',pinned_version=2 WHERE asset_id=?`, f.assetID)
			case "project_archived":
				exec(`UPDATE projects SET status='archived' WHERE id=?`, f.projectID)
			case "workspace_archived":
				exec(`UPDATE project_workspaces SET status='archived' WHERE id=?`, f.workspaceID)
			case "task_archived":
				exec(`UPDATE tasks SET archived_at=1 WHERE id=?`, task)
			case "oversized_path":
				exec(`UPDATE workspace_file_publications SET relative_path=? WHERE task_id=?`, strings.Repeat("a", 4097), task)
			case "utf8_bytes":
				exec(`UPDATE workspace_file_publications SET relative_path=? WHERE task_id=?`, strings.Repeat("界", 1366), task)
			case "bad_path":
				exec(`UPDATE workspace_file_publications SET relative_path='../private' WHERE task_id=?`, task)
			case "oversized_mime":
				exec(`UPDATE project_library_asset_versions SET mime_type=? WHERE asset_id=?`, strings.Repeat("a", 257), f.assetID)
			case "invalid_utf8":
				exec(`UPDATE workspace_file_publications SET relative_path=CAST(x'ff' AS TEXT) WHERE task_id=?`, task)
			case "control_path":
				exec(`UPDATE workspace_file_publications SET relative_path=? WHERE task_id=?`, "outputs/a\tb.txt", task)
			case "control_mime":
				exec(`UPDATE project_library_asset_versions SET mime_type=? WHERE asset_id=?`, "text/plain\x7ffoo", f.assetID)
			}
			var eventsBefore int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsBefore); err != nil {
				t.Fatal(err)
			}
			out, err := f.projects.TaskLibraryPublications(context.Background(), f.projectID, f.workspaceID, task)
			if scenario == "allowed" || scenario == "historical_stopped_source" {
				if err != nil || len(out) != 1 || out[0].TaskID != task || out[0].ContentHash != f.contentHash || out[0].RelativePath != "outputs/result.txt" || out[0].Version != 1 {
					t.Fatalf("actual publication: %+v %v", out, err)
				}
			} else if scenario == "no_receipts" {
				if err != nil || out == nil || len(out) != 0 {
					t.Fatalf("empty receipt inventory: %+v %v", out, err)
				}
			} else if err == nil || len(out) != 0 {
				t.Fatalf("invalid receipt advertised: %+v %v", out, err)
			}
			var eventsAfter int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsAfter); err != nil {
				t.Fatal(err)
			}
			if eventsBefore != eventsAfter {
				t.Fatal("metadata read wrote ledger events")
			}
		})
	}
}

func TestTaskLibraryPublicationsBoundRawInventoryBeforePermissionFiltering(t *testing.T) {
	f := publishedLibraryHTTPFixture(t, nil)
	task := publishedReceiptTask(t, f)
	clone := func(i int) {
		t.Helper()
		_, err := f.db.SQL().Exec(`INSERT INTO workspace_file_publications(task_id,project_id,project_workspace_id,runtime_id,application_id,relative_path,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at)
 SELECT task_id,project_id,project_workspace_id,runtime_id,application_id,?,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at FROM workspace_file_publications WHERE task_id=? AND relative_path='outputs/result.txt'`, fmt.Sprintf("outputs/copy-%02d.txt", i), task)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < 32; i++ {
		clone(i)
	}
	out, err := f.projects.TaskLibraryPublications(context.Background(), f.projectID, f.workspaceID, task)
	if err != nil || len(out) != 32 {
		t.Fatalf("32 exact receipts: %d %v", len(out), err)
	}
	clone(32)
	// Even an unreadable 33rd receipt must overflow the RAW first slice.
	if _, err = f.db.SQL().Exec(`UPDATE workspace_file_publications SET status='in_progress',library_asset_id=NULL,asset_version=NULL WHERE task_id=? AND relative_path='outputs/copy-32.txt'`, task); err != nil {
		t.Fatal(err)
	}
	out, err = f.projects.TaskLibraryPublications(context.Background(), f.projectID, f.workspaceID, task)
	if err != projectworkspace.ErrTaskPublicationLimit || out != nil {
		t.Fatalf("33 raw receipts partially disclosed: %d %v", len(out), err)
	}
}
