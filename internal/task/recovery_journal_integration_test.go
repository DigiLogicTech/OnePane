//go:build integration

package task_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestRecoveryCaptureJournalInspectionUsesOriginalOwnerWithoutTransitions(t *testing.T) {
	db, s, row, attempt := interruptedAdmissionFixture(t)
	ctx := context.Background()
	if captured, err := s.CaptureRecoveryCases(ctx, 1); err != nil || captured.Captured != 1 {
		t.Fatalf("intake: %+v %v", captured, err)
	}
	page, err := s.ListRecoveryCases(ctx, "ws", row.ID, "")
	if err != nil || len(page.Cases) != 1 {
		t.Fatal(err)
	}
	caseID := page.Cases[0].ID
	missing, err := s.ObserveRecoveryJournal(ctx, "ws", row.ID, caseID)
	if err != nil || missing.Availability != "not_recorded" || missing.RunID != "" || missing.SnapshotSHA256 != "" || len(missing.Steps) != 0 {
		t.Fatalf("absent journal mislabeled: %+v %v", missing, err)
	}
	for _, statement := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('journal-worker','system','Worker','active',1,1,1)`,
	} {
		if _, err = db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL().Exec(`UPDATE task_attempts SET worker_principal_id='journal-worker' WHERE id=?`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('journal-run','ws',?,?,'journal-worker','interrupted','general','inference.general','L1',24,3,3,'{}','{"PRIVATE_PROMPT":"not public"}',1,1)`, row.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,result_ref,detail_json,started_at) VALUES('journal-step','journal-run',1,'model','unknown','PRIVATE_TARGET_ID','{"reason":"PRIVATE_REASON","outcome":"unknown_or_response_unreadable","action":"operator_reconciliation_required"}',1)`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	view, err := s.ObserveRecoveryJournal(ctx, "ws", row.ID, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Availability != "recorded" || view.TaskID != row.ID || view.AttemptID != attempt.ID || view.Phase != "awaiting_inventory" || view.TaskRevision != row.Revision || view.SnapshotSHA256 == "" || len(view.Steps) != 1 || view.Steps[0].Kind != "model" || view.Steps[0].Status != "unknown" || !view.Steps[0].ReferenceRecognized {
		t.Fatalf("real journal missing: %+v", view)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("raw journal or candidate target identity disclosed")
	}
	var after int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after); err != nil || after != before {
		t.Fatal("diagnostic read emitted an audit or transition")
	}
	current, err := s.Get(ctx, row.ID)
	if err != nil || current.State != task.StateBlocked || current.Revision != row.Revision {
		t.Fatal("diagnostic cleared original recovery fence")
	}
	if _, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision}); err == nil {
		t.Fatal("journal viewing authorized retry")
	}
	for _, invalid := range []struct{ ws, task, caseID string }{{"foreign", row.ID, caseID}, {"ws", "foreign", caseID}, {"ws", row.ID, "missing"}} {
		if got, e := s.ObserveRecoveryJournal(ctx, invalid.ws, invalid.task, invalid.caseID); e == nil || got.SnapshotSHA256 != "" {
			t.Fatalf("foreign observation accepted: %+v", got)
		}
	}
	foreign, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Foreign journal owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`UPDATE agent_worker_runs SET task_id=? WHERE id='journal-run'`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if got, e := s.ObserveRecoveryJournal(ctx, "ws", row.ID, caseID); e == nil || got.Availability == "not_recorded" || got.SnapshotSHA256 != "" {
		t.Fatal("foreign journal was accepted or hidden as absent")
	}
	if _, err = db.SQL().Exec(`UPDATE agent_worker_runs SET task_id=? WHERE id='journal-run'`, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`UPDATE agent_worker_steps SET detail_json=json_object('private',printf('%070000d',1)) WHERE id='journal-step'`); err != nil {
		t.Fatal(err)
	}
	if got, e := s.ObserveRecoveryJournal(ctx, "ws", row.ID, caseID); e == nil || got.SnapshotSHA256 != "" || len(got.Steps) != 0 {
		t.Fatal("oversized journal emitted a partial diagnostic")
	}
}
