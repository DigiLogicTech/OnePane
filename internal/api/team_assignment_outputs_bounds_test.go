//go:build integration

package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTeamAssignmentOutputsReceiptBoundariesAndResponseBudget(t *testing.T) {
	f := newTeamAssignmentOutputsFixture(t)
	get := func(want int, count int) {
		t.Helper()
		before := assignmentOutputReadState(t, f)
		w := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/team-sessions/"+f.sessionID+"/assignments/"+f.taskID+"/outputs", nil))
		if w.Code != want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
		}
		if want == 200 {
			var result assignmentPublicationsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Outputs) != count || result.Outputs == nil {
				t.Fatalf("receipt count=%d want=%d", len(result.Outputs), count)
			}
		} else if strings.Contains(w.Body.String(), f.assetID) || strings.Contains(w.Body.String(), f.contentHash) || strings.Contains(w.Body.String(), "outputs/result.txt") {
			t.Fatal("bounded refusal disclosed partial private inventory")
		}
		if after := assignmentOutputReadState(t, f); after != before {
			t.Fatal("metadata request mutated execution state")
		}
	}
	for i := 1; i < 32; i++ {
		if _, err := f.db.SQL().Exec(`INSERT INTO workspace_file_publications(task_id,project_id,project_workspace_id,runtime_id,application_id,relative_path,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at)
 SELECT task_id,project_id,project_workspace_id,runtime_id,application_id,?,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at FROM workspace_file_publications WHERE task_id=? AND relative_path='outputs/result.txt'`, fmt.Sprintf("outputs/copy-%02d.txt", i), f.taskID); err != nil {
			t.Fatal(err)
		}
	}
	get(200, 32)
	if _, err := f.db.SQL().Exec(`INSERT INTO workspace_file_publications(task_id,project_id,project_workspace_id,runtime_id,application_id,relative_path,content_hash,status,artifact_id,library_asset_id,asset_version,created_at,updated_at)
 SELECT task_id,project_id,project_workspace_id,runtime_id,application_id,'outputs/overflow.txt',content_hash,'in_progress',NULL,NULL,NULL,created_at,updated_at FROM workspace_file_publications WHERE task_id=? LIMIT 1`, f.taskID); err != nil {
		t.Fatal(err)
	}
	get(409, 0)
	if _, err := f.db.SQL().Exec(`DELETE FROM workspace_file_publications WHERE task_id=? AND relative_path='outputs/overflow.txt'`, f.taskID); err != nil {
		t.Fatal(err)
	}
	// Each raw UTF-8 path is within4096bytes. JSON escaping makes the complete
	// encoded response exceed256KiB, which must refuse the entire projection.
	if _, err := f.db.SQL().Exec(`UPDATE workspace_file_publications SET relative_path=?||relative_path WHERE task_id=?`, strings.Repeat("\"", 4060), f.taskID); err != nil {
		t.Fatal(err)
	}
	get(409, 0)
	if _, err := f.db.SQL().Exec(`DELETE FROM workspace_file_publications WHERE task_id=?`, f.taskID); err != nil {
		t.Fatal(err)
	}
	get(200, 0)
}
