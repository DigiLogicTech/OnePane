package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLiveTeamAssignmentCompletionUsesCurrentLedgerBoundEvidence(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	cp := completeAssignment(t, s, db, ids["build"])
	// A later valid checkpoint is not the contribution accepted by Task completion.
	if _, err := db.SQL().Exec(`INSERT INTO checkpoints(id,workspace_id,task_id,verification_id,state_json,status,created_at) SELECT 'unrelated-later-checkpoint',workspace_id,task_id,verification_id,'{"private":"CHECKPOINT_CANARY"}','valid',created_at+1 FROM checkpoints WHERE id=?`, cp); err != nil {
		t.Fatal(err)
	}
	room, err := s.LiveRoom(context.Background(), ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range room.Assignments {
		if a.TaskID == ids["build"] {
			found = true
			if a.CheckpointID != cp || a.VerificationID == "" || a.VerificationLevel != "V1" {
				t.Fatalf("completion: %+v", a)
			}
		} else if a.CheckpointID != "" || a.VerificationID != "" || a.VerificationLevel != "" {
			t.Fatalf("unfinished evidence: %+v", a)
		}
	}
	if !found {
		t.Fatal("completed assignment missing")
	}
	raw, _ := json.Marshal(room)
	if strings.Contains(string(raw), "CHECKPOINT_CANARY") || strings.Contains(string(raw), "synthetic reviewed contribution") {
		t.Fatal("raw checkpoint state disclosed")
	}
}

func TestLiveTeamAssignmentCompletionProductionPlanIsBounded(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	for i := 0; i < 512; i++ {
		if _, err := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'tenant','task.progress','task',?,'{}',1),(?,'tenant','task.completed','task',?,'{}',1)`, fmt.Sprintf("progress-%d", i), ids["build"], fmt.Sprintf("old-completion-%d", i), ids["build"]); err != nil {
			t.Fatal(err)
		}
	}
	cp := completeAssignment(t, s, db, ids["build"])
	ss, err := s.Session(context.Background(), ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.SQL().Query("EXPLAIN QUERY PLAN "+liveAssignmentCompletionQuery, *ss.AcceptedPlanID, ss.ID, ss.WorkspaceID, ss.TaskID, ids["build"], "build", specs[0].MemberID)
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
	if strings.Contains(plan.String(), "SCAN ") || strings.Contains(plan.String(), "TEMP B-TREE") || strings.Count(plan.String(), "idx_events_task_latest_completion") != 2 {
		t.Fatalf("unbounded actual completion query:\n%s", plan.String())
	}
	room, err := s.LiveRoom(context.Background(), ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range room.Assignments {
		if a.TaskID == ids["build"] && a.CheckpointID == cp {
			return
		}
	}
	t.Fatal("current ledger-bound completion not returned with populated history")
}

func TestLiveTeamAssignmentCompletionRejectsChangedBindings(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"invalidated checkpoint", `UPDATE checkpoints SET invalidated_at=1 WHERE id=?`},
		{"stale checkpoint", `UPDATE checkpoints SET status='stale' WHERE id=?`},
		{"foreign checkpoint Task", `UPDATE checkpoints SET task_id=(SELECT parent_task_id FROM tasks WHERE id=checkpoints.task_id) WHERE id=?`},
		{"foreign checkpoint tenant", `UPDATE checkpoints SET workspace_id='other-tenant' WHERE id=?`},
		{"stale verification", `UPDATE verifications SET status='stale' WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`},
		{"foreign verification Task", `UPDATE verifications SET task_id=(SELECT parent_task_id FROM tasks WHERE id=verifications.task_id) WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`},
		{"foreign verification tenant", `UPDATE verifications SET workspace_id='other-tenant' WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`},
		{"weak verification", `UPDATE verifications SET achieved_level='V0' WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`},
		{"missing verification completion", `UPDATE verifications SET completed_at=NULL WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`},
		{"changed Task revision", `UPDATE tasks SET revision=revision+1 WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"changed completion criteria", `UPDATE tasks SET completion_json='{"required_verification":"V1","changed":true}' WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"archived contribution", `UPDATE tasks SET archived_at=1 WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"failed latest Attempt", `UPDATE task_attempts SET status='failed' WHERE task_id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"newer succeeded Attempt", `INSERT INTO task_attempts(id,task_id,attempt_number,status,started_at,ended_at,metadata_json) SELECT 'foreign-latest-attempt',task_id,99,'succeeded',1,1,'{}' FROM checkpoints WHERE id=?`},
		{"unaccepted graph", `UPDATE team_sessions SET accepted_plan_id=NULL WHERE id=(SELECT session_id FROM team_execution_assignments WHERE task_id=(SELECT task_id FROM checkpoints WHERE id=?))`},
		{"foreign artifact scope", `UPDATE tasks SET artifact_session_id='foreign-artifact-session' WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"foreign Project scope", `UPDATE tasks SET project_id='foreign-project' WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"foreign named Workspace scope", `UPDATE tasks SET project_workspace_id='foreign-workspace' WHERE id=(SELECT task_id FROM checkpoints WHERE id=?)`},
		{"missing latest completion binding", `INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'late-completion','tenant','task.completed','task',task_id,'{}',created_at+1 FROM checkpoints WHERE id=?`},
		{"oversized latest completion binding", `INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'late-completion','tenant','task.completed','task',task_id,json_object('checkpoint_id',id,'task_id',task_id,'to','complete','revision',(SELECT revision FROM tasks WHERE id=task_id),'padding',printf('%17000s','x')),created_at+1 FROM checkpoints WHERE id=?`},
		{"unbound latest start event", `INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'late-start','tenant','task.started','task',task_id,'{}',created_at+1 FROM checkpoints WHERE id=?`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, db, ss, specs := assignmentFixture(t)
			ids := acceptAssignments(t, s, ss, specs)
			cp := completeAssignment(t, s, db, ids["build"])
			if _, err := db.SQL().Exec(`INSERT INTO artifact_sessions(id,workspace_id,title,format,status,created_by,created_at,updated_at) VALUES('foreign-artifact-session','tenant','Other result','text','active','human',1,1)`); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('other-tenant','Other','active',1,1,1)`,
				`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('foreign-project','tenant','Other project','active','{}','{}','human',1,1)`,
				`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('foreign-workspace','foreign-project','Other Workspace',1,1)`,
			} {
				if _, err := db.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.SQL().Exec(tc.sql, cp); err != nil {
				t.Fatal(err)
			}
			room, err := s.LiveRoom(context.Background(), ss.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range room.Assignments {
				if a.TaskID == ids["build"] && (a.CheckpointID != "" || a.VerificationID != "" || a.VerificationLevel != "") {
					t.Fatalf("changed evidence exposed: %+v", a)
				}
			}
		})
	}
}

func TestLiveTeamAssignmentCompletionPreservesV0Declaration(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	if _, err := db.SQL().Exec(`UPDATE tasks SET completion_json=json_set(completion_json,'$.required_verification','V0') WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	ids := acceptAssignments(t, s, ss, specs)
	cp := completeAssignment(t, s, db, ids["build"])
	if _, err := db.SQL().Exec(`UPDATE verifications SET required_level='V0',achieved_level='V0' WHERE id=(SELECT verification_id FROM checkpoints WHERE id=?)`, cp); err != nil {
		t.Fatal(err)
	}
	room, err := s.LiveRoom(context.Background(), ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range room.Assignments {
		if a.TaskID == ids["build"] {
			if a.CheckpointID != cp || a.VerificationLevel != "V0" {
				t.Fatalf("V0 declaration lost: %+v", a)
			}
			return
		}
	}
	t.Fatal("completed contribution missing")
}
