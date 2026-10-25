package team

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAssignmentCompletionReadBindsCurrentAcceptedScope(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	for _, query := range []string{
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('project','tenant','Project','active','{}','{}','human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('named','project','Workspace',1,1)`,
		`UPDATE tasks SET project_id='project',project_workspace_id='named' WHERE id='` + ss.TaskID + `'`,
	} {
		if _, err := db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	ids := acceptAssignments(t, s, ss, specs)
	cp := completeAssignment(t, s, db, ids["build"])
	ctx := context.Background()
	want, err := s.AssignmentCompletion(ctx, ss.ID, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	if want.SessionID != ss.ID || want.WorkspaceID != "tenant" || want.ParentTaskID != ss.TaskID || want.TaskID != ids["build"] || want.AssignmentKey != "build" || want.MemberID != specs[0].MemberID || want.ProjectID != "project" || want.ProjectWorkspaceID != "named" || want.ArtifactSessionID != "" || want.TaskRevision < 1 || want.CheckpointID != cp || want.VerificationID == "" || want.VerificationLevel != "V1" || want.PlanID == "" {
		t.Fatalf("unexpected current identity: %+v", want)
	}
	if _, err = s.AssignmentCompletion(ctx, ss.ID, ids["review"]); err != ErrInvalid {
		t.Fatalf("unfinished assignment: %v", err)
	}
	if _, err = s.AssignmentCompletion(ctx, ss.ID, ss.TaskID); err != ErrInvalid {
		t.Fatalf("parent is not assignment: %v", err)
	}
	if _, err = s.AssignmentCompletion(ctx, "foreign", ids["build"]); err != ErrInvalid {
		t.Fatalf("foreign session: %v", err)
	}
	oversized := strings.Repeat("s", 257)
	if _, err := db.SQL().Exec(`INSERT INTO artifact_sessions(id,workspace_id,title,format,status,created_by,created_at,updated_at) VALUES(?,'tenant','Oversized identity','text','active','human',1,1)`, oversized); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, query string }{
		{"accepted plan", `UPDATE team_sessions SET accepted_plan_id=NULL WHERE id='` + ss.ID + `'`},
		{"parent Task", `UPDATE team_sessions SET task_id='` + ids["review"] + `' WHERE id='` + ss.ID + `'`},
		{"accepted root", `DELETE FROM team_assignment_roots WHERE session_id='` + ss.ID + `'`},
		{"completion criteria", `UPDATE tasks SET completion_json='{"required_verification":"V1","changed":true}' WHERE id='` + ids["build"] + `'`},
		{"root completion criteria", `UPDATE tasks SET completion_json='{"required_verification":"V1","changed":true}' WHERE id='` + ss.TaskID + `'`},
		{"oversized root completion criteria", `UPDATE tasks SET completion_json=json_object('required_verification','V1','padding',printf('%65536s','x')) WHERE id='` + ss.TaskID + `'`},
		{"oversized root seal", `UPDATE team_assignment_roots SET completion_sha256=printf('%65536s','x') WHERE session_id='` + ss.ID + `'`},
		{"oversized child seal", `UPDATE team_execution_assignments SET completion_sha256=printf('%65536s','x') WHERE task_id='` + ids["build"] + `'`},
		{"Task revision", `UPDATE tasks SET revision=revision+1 WHERE id='` + ids["build"] + `'`},
		{"checkpoint", `UPDATE checkpoints SET invalidated_at=1 WHERE id='` + cp + `'`},
		{"verification", `UPDATE verifications SET status='stale' WHERE id='` + want.VerificationID + `'`},
		{"latest Attempt", `UPDATE task_attempts SET status='failed' WHERE task_id='` + ids["build"] + `'`},
		{"parent archived", `UPDATE tasks SET archived_at=1 WHERE id='` + ss.TaskID + `'`},
		{"child archived", `UPDATE tasks SET archived_at=1 WHERE id='` + ids["build"] + `'`},
		{"child named scope", `UPDATE tasks SET project_workspace_id=NULL WHERE id='` + ids["build"] + `'`},
		{"child Project scope", `UPDATE tasks SET project_id=NULL WHERE id='` + ids["build"] + `'`},
		{"absent Project scope", `UPDATE tasks SET project_id=NULL,project_workspace_id=NULL WHERE id IN ('` + ss.TaskID + `','` + ids["build"] + `')`},
		{"oversized identity", `UPDATE tasks SET artifact_session_id='` + oversized + `' WHERE id IN ('` + ss.TaskID + `','` + ids["build"] + `')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.SQL().Exec(`SAVEPOINT changed_binding`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.SQL().Exec(`ROLLBACK TO changed_binding`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.SQL().Exec(`RELEASE changed_binding`); err != nil {
					t.Fatal(err)
				}
			}()
			if tc.name == "accepted root" {
				if _, err := db.SQL().Exec(`DROP TRIGGER team_assignment_root_retained`); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "oversized root seal" {
				if _, err := db.SQL().Exec(`DROP TRIGGER team_assignment_root_immutable`); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "oversized child seal" {
				if _, err := db.SQL().Exec(`DROP TRIGGER team_assignment_immutable`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.SQL().Exec(tc.query); err != nil {
				t.Fatal(err)
			}
			got, err := s.AssignmentCompletion(ctx, ss.ID, ids["build"])
			if err != ErrInvalid || got != (AssignmentCompletion{}) {
				t.Fatalf("changed identity disclosed: %+v, %v", got, err)
			}
		})
	}
	got, err := s.AssignmentCompletion(ctx, ss.ID, ids["build"])
	if err != nil || got != want {
		t.Fatalf("original identity not preserved: %+v, %v", got, err)
	}
	// The complete shared protocol remains indexed when unrelated and older
	// completion history is present; this tests the actual shipped statement.
	for i := 0; i < 256; i++ {
		if _, err := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'tenant','task.completed','task',?,'{}',1)`, fmt.Sprintf("other-completion-%d", i), fmt.Sprintf("other-task-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.SQL().Query("EXPLAIN QUERY PLAN "+assignmentCompletionReadQuery, ss.ID, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.String(), "SCAN ") || strings.Contains(plan.String(), "TEMP B-TREE") || strings.Contains(plan.String(), "AUTOMATIC") || strings.Count(plan.String(), "idx_events_task_latest_completion") != 2 {
		t.Fatalf("unbounded completion read:\n%s", plan.String())
	}
}
