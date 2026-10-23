//go:build integration

package task_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestRecoveryCaptureRetainsOriginalAndAcknowledgesOnlyDurableIntake(t *testing.T) {
	db, s, job, attempt := interruptedAdmissionFixture(t)
	ctx := context.Background()
	var jobID string
	if e := db.SQL().QueryRow(`SELECT id FROM outbox_jobs WHERE job_type='task.recovery.required'`).Scan(&jobID); e != nil {
		t.Fatal(e)
	}
	result, e := s.CaptureRecoveryCases(ctx, 1)
	if e != nil || result.Captured != 1 || result.Acknowledged != 1 {
		t.Fatalf("capture: %+v %v", result, e)
	}
	var phase, taskID, attemptID, eventID string
	if e = db.SQL().QueryRow(`SELECT phase,task_id,attempt_id,interruption_event_id FROM task_recovery_cases`).Scan(&phase, &taskID, &attemptID, &eventID); e != nil || phase != "awaiting_inventory" || taskID != job.ID || attemptID != attempt.ID || eventID == "" {
		t.Fatalf("case identity: %s %s %s %s %v", phase, taskID, attemptID, eventID, e)
	}
	// Duplicate delivery and reconstruction must not duplicate the case or audit.
	if _, e = db.SQL().Exec(`UPDATE outbox_jobs SET status='pending' WHERE id=?`, jobID); e != nil {
		t.Fatal(e)
	}
	s = task.NewService(db.SQL(), db, clock.Real{})
	result, e = s.CaptureRecoveryCases(ctx, 1)
	if e != nil || result.Captured != 0 || result.Acknowledged != 1 {
		t.Fatalf("redelivery: %+v %v", result, e)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM task_attempts WHERE status='interrupted'`} {
		var count int
		if e = db.SQL().QueryRow(query).Scan(&count); e != nil || count != 1 {
			t.Fatalf("duplicate/lost history: %s %d %v", query, count, e)
		}
	}
	if _, e = s.MarkReady(ctx, task.TransitionCommand{TaskID: job.ID, ExpectedRevision: job.Revision}); !errors.Is(e, task.ErrRecoveryRequired) {
		t.Fatalf("intake cleared admission: %v", e)
	}
	current, e := s.Get(ctx, job.ID)
	if e != nil || current.State != task.StateBlocked || current.Revision != job.Revision {
		t.Fatalf("capture changed execution: %+v %v", current, e)
	}
	if _, e = db.SQL().Exec(`UPDATE task_recovery_cases SET task_id=task_id`); e == nil {
		t.Fatal("case identity mutable")
	}
	page, e := s.ListRecoveryCases(ctx, "ws", job.ID, "")
	if e != nil || len(page.Cases) != 1 || page.Cases[0].AttemptID != attempt.ID {
		t.Fatalf("intake metadata missing: %+v %v", page, e)
	}
	caseID := page.Cases[0].ID
	if _, e = s.GetRecoveryCase(ctx, "foreign", job.ID, caseID); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("foreign Workspace read: %v", e)
	}
	if _, e = s.GetRecoveryCase(ctx, "ws", "foreign-task", caseID); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("foreign Task read: %v", e)
	}
	if c, e := s.GetRecoveryCase(ctx, "ws", job.ID, caseID); e != nil || c.Phase != "awaiting_inventory" {
		t.Fatalf("case detail missing: %+v %v", c, e)
	}
}

func TestRecoveryCaptureRejectsUnprovenIdentity(t *testing.T) {
	for _, corrupt := range []string{"foreign_workspace", "foreign_attempt", "missing_event", "oversize_payload", "duplicate_event", "noninterrupted"} {
		t.Run(corrupt, func(t *testing.T) {
			db, s, job, attempt := interruptedAdmissionFixture(t)
			var e error
			switch corrupt {
			case "foreign_workspace":
				_, e = db.SQL().Exec(`UPDATE outbox_jobs SET workspace_id=NULL WHERE job_type='task.recovery.required'`)
			case "foreign_attempt":
				_, e = db.SQL().Exec(`UPDATE outbox_jobs SET payload_json=json_set(payload_json,'$.attempt_id','foreign') WHERE job_type='task.recovery.required'`)
			case "missing_event":
				_, e = db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('no-event',?,2,'interrupted','{}')`, job.ID)
				if e == nil {
					_, e = db.SQL().Exec(`UPDATE outbox_jobs SET payload_json=json_set(payload_json,'$.attempt_id','no-event') WHERE job_type='task.recovery.required'`)
				}
			case "oversize_payload":
				_, e = db.SQL().Exec(`UPDATE outbox_jobs SET payload_json=json_set(payload_json,'$.reason',?) WHERE job_type='task.recovery.required'`, string(make([]byte, 65537)))
			case "duplicate_event":
				_, e = db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'duplicate',workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at FROM events WHERE event_type='task.attempt_interrupted'`)
			case "noninterrupted":
				_, e = db.SQL().Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, attempt.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			result, e := s.CaptureRecoveryCases(context.Background(), 1)
			if e != nil || result.Deferred != 1 || result.Captured != 0 || result.Acknowledged != 0 {
				t.Fatalf("unproven case accepted: %+v %v", result, e)
			}
			var count int
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_recovery_cases`).Scan(&count); e != nil || count != 0 {
				t.Fatalf("case persisted: %d %v", count, e)
			}
			current, e := s.Get(context.Background(), job.ID)
			if e != nil || current.State != task.StateBlocked {
				t.Fatalf("Task changed: %+v %v", current, e)
			}
		})
	}
}

func TestRecoveryCaptureAuditAndAcknowledgementRollback(t *testing.T) {
	for _, reject := range []string{"audit", "ack"} {
		t.Run(reject, func(t *testing.T) {
			db, s, _, _ := interruptedAdmissionFixture(t)
			query := `CREATE TRIGGER reject_capture BEFORE INSERT ON events WHEN NEW.event_type='task.recovery_case_captured' BEGIN SELECT RAISE(ABORT,'audit denied'); END`
			if reject == "ack" {
				query = `CREATE TRIGGER reject_capture BEFORE UPDATE ON outbox_jobs WHEN NEW.job_type='task.recovery.required' AND NEW.status='succeeded' BEGIN SELECT RAISE(ABORT,'ack denied'); END`
			}
			if _, e := db.SQL().Exec(query); e != nil {
				t.Fatal(e)
			}
			if _, e := s.CaptureRecoveryCases(context.Background(), 1); e == nil {
				t.Fatal("write failure hidden")
			}
			for _, q := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND status='succeeded'`} {
				var n int
				if e := db.SQL().QueryRow(q).Scan(&n); e != nil || n != 0 {
					t.Fatalf("partial commit %s: %d %v", q, n, e)
				}
			}
			if _, e := db.SQL().Exec(`DROP TRIGGER reject_capture`); e != nil {
				t.Fatal(e)
			}
			r, e := s.CaptureRecoveryCases(context.Background(), 1)
			if e != nil || r.Captured != 1 {
				t.Fatalf("retry lost cursor: %+v %v", r, e)
			}
		})
	}
}

func TestRecoveryCaptureBoundedFairnessAndTwoConsumers(t *testing.T) {
	db, s, _, _ := interruptedAdmissionFixture(t)
	// A permanently invalid prefix must not prevent the next bounded pass.
	for i := 0; i < 17; i++ {
		if _, e := db.SQL().Exec(`INSERT INTO outbox_jobs(id,job_type,payload_json,status,available_at,max_attempts,created_at,updated_at) VALUES(?,'task.recovery.required','{}','pending',1,10,1,1)`, fmt.Sprintf("000-invalid-%02d", i)); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.CaptureRecoveryCases(context.Background(), 100)
	if e != nil || r.Scanned != 16 || r.Deferred != 16 {
		t.Fatalf("unbounded scan: %+v %v", r, e)
	}
	r, e = s.CaptureRecoveryCases(context.Background(), 16)
	if e != nil || r.Captured != 1 || r.Scanned != 2 {
		t.Fatalf("valid work starved: %+v %v", r, e)
	}
	if _, e = db.SQL().Exec(`UPDATE outbox_jobs SET status='pending' WHERE workspace_id IS NOT NULL AND job_type='task.recovery.required'`); e != nil {
		t.Fatal(e)
	}
	// Race both creation of a new case and redelivery of the original case.
	ctx := context.Background()
	second, e := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Independent interrupted work"})
	if e != nil {
		t.Fatal(e)
	}
	second, e = s.MarkReady(ctx, task.TransitionCommand{TaskID: second.ID, ExpectedRevision: second.Revision})
	if e != nil {
		t.Fatal(e)
	}
	second, _, e = s.Start(ctx, task.StartCommand{TaskID: second.ID, ExpectedRevision: second.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: second.ID, ExpectedRevision: second.Revision}); e != nil {
		t.Fatal(e)
	}
	var path string
	rows, e := db.SQL().Query(`PRAGMA database_list`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var seq int
		var name, p string
		if e = rows.Scan(&seq, &name, &p); e != nil {
			t.Fatal(e)
		}
		if name == "main" {
			path = p
		}
	}
	if e = rows.Close(); e != nil {
		t.Fatal(e)
	}
	other, e := sqlitestore.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	services := []*task.Service{task.NewService(db.SQL(), db, clock.Real{}), task.NewService(other.SQL(), other, clock.Real{})}
	var wg sync.WaitGroup
	for _, service := range services {
		wg.Add(1)
		go func(s *task.Service) {
			defer wg.Done()
			for i := 0; i < 6; i++ {
				_, e := s.CaptureRecoveryCases(context.Background(), 16)
				if e != nil && !sqlitestore.IsBusy(e) {
					t.Errorf("consumer: %v", e)
					return
				}
			}
		}(service)
	}
	wg.Wait()
	for _, q := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`} {
		var n int
		if e = db.SQL().QueryRow(q).Scan(&n); e != nil || n != 2 {
			t.Fatalf("duplicate recovery: %d %v", n, e)
		}
	}
}

func TestRecoveryCaptureHistoryBoundAndScheduledOwnership(t *testing.T) {
	for _, history := range []int{128, 129} {
		t.Run(fmt.Sprint(history), func(t *testing.T) {
			db, s, job, _ := interruptedAdmissionFixture(t)
			for i := 1; i < history; i++ {
				// Historical entries for different incarnations do not prove this case.
				if _, e := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'ws','task.attempt_interrupted','task',?,json_object('task_id',?,'attempt_id',?),1)`, fmt.Sprintf("old-%03d", i), job.ID, job.ID, fmt.Sprintf("old-attempt-%03d", i)); e != nil {
					t.Fatal(e)
				}
			}
			r, e := s.CaptureRecoveryCases(context.Background(), 1)
			if e != nil || history == 128 && r.Captured != 1 || history == 129 && (r.Captured != 0 || r.Deferred != 1) {
				t.Fatalf("history bound %d: %+v %v", history, r, e)
			}
		})
	}
	for _, ownership := range []string{"future", "claimed"} {
		t.Run(ownership, func(t *testing.T) {
			db, s, _, _ := interruptedAdmissionFixture(t)
			q := `UPDATE outbox_jobs SET available_at=9223372036854775807 WHERE job_type='task.recovery.required'`
			if ownership == "claimed" {
				q = `UPDATE outbox_jobs SET status='claimed',claimed_by='other',claim_expires_at=9223372036854775807 WHERE job_type='task.recovery.required'`
			}
			if _, e := db.SQL().Exec(q); e != nil {
				t.Fatal(e)
			}
			r, e := s.CaptureRecoveryCases(context.Background(), 16)
			if e != nil || r.Scanned != 0 {
				t.Fatalf("unavailable job consumed: %+v %v", r, e)
			}
		})
	}
}

func TestRecoveryCaptureReadPaginationCannotCrossScope(t *testing.T) {
	db, s, job, _ := interruptedAdmissionFixture(t)
	ctx := context.Background()
	if _, e := s.CaptureRecoveryCases(ctx, 1); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("history-%03d", i)
		if _, e := db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES(?,?,?,'interrupted','{}')`, id, job.ID, i+2); e != nil {
			t.Fatal(e)
		}
		if _, e := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'ws','task.attempt_interrupted','task',?,json_object('task_id',?,'attempt_id',?),1)`, id, job.ID, job.ID, id); e != nil {
			t.Fatal(e)
		}
		if _, e := db.SQL().Exec(`INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,task_revision_at_capture,created_at) VALUES(?,'ws',?,?,?,'historical-job',?,1)`, id, job.ID, id, id, job.Revision); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.ListRecoveryCases(ctx, "ws", job.ID, "")
	if e != nil || len(page.Cases) != 32 || page.NextAfter == "" {
		t.Fatalf("unbounded/missing page: %+v %v", page, e)
	}
	next, e := s.ListRecoveryCases(ctx, "ws", job.ID, page.NextAfter)
	if e != nil || len(next.Cases) != 1 || next.NextAfter != "" || next.Cases[0].ID <= page.NextAfter {
		t.Fatalf("cursor duplicated/omitted history: %+v %v", next, e)
	}
	for _, scope := range [][2]string{{"foreign", job.ID}, {"ws", "foreign-task"}} {
		denied, e := s.ListRecoveryCases(ctx, scope[0], scope[1], "")
		if e != nil || len(denied.Cases) != 0 {
			t.Fatalf("foreign list leaked: %+v %v", denied, e)
		}
	}
	if _, e = s.ListRecoveryCases(ctx, "ws", job.ID, string(make([]byte, 161))); !errors.Is(e, task.ErrInvalidCommand) {
		t.Fatalf("oversized cursor accepted: %v", e)
	}
}
