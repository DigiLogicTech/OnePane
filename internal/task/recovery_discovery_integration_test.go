//go:build integration

package task_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestRecoveryDiscoveryDeletedAndConsumedJobsPreserveOriginal(t *testing.T) {
	for _, disposition := range []string{"deleted", "consumed", "pending", "claimed", "future"} {
		t.Run(disposition, func(t *testing.T) {
			db, s, row, attempt := interruptedAdmissionFixture(t)
			query := `DELETE FROM outbox_jobs WHERE job_type='task.recovery.required'`
			switch disposition {
			case "consumed":
				query = `UPDATE outbox_jobs SET status='succeeded' WHERE job_type='task.recovery.required'`
			case "pending":
				query = `UPDATE outbox_jobs SET status='pending' WHERE job_type='task.recovery.required'`
			case "claimed":
				query = `UPDATE outbox_jobs SET status='claimed',claimed_by='other',claim_expires_at=9223372036854775807 WHERE job_type='task.recovery.required'`
			case "future":
				query = `UPDATE outbox_jobs SET available_at=9223372036854775807 WHERE job_type='task.recovery.required'`
			}
			if _, e := db.SQL().Exec(query); e != nil {
				t.Fatal(e)
			}
			var jobsBefore string
			if e := db.SQL().QueryRow(`SELECT coalesce(json_group_array(json_object('id',id,'status',status,'available',available_at,'owner',claimed_by,'expires',claim_expires_at)),'[]') FROM outbox_jobs`).Scan(&jobsBefore); e != nil {
				t.Fatal(e)
			}
			r, e := s.DiscoverRecoveryCases(context.Background(), 1)
			if e != nil || r.Captured != 1 {
				t.Fatalf("discovery: %+v %v", r, e)
			}
			var source, original, phase string
			var job sql.NullString
			if e = db.SQL().QueryRow(`SELECT source_kind,source_job_id,attempt_id,phase FROM task_recovery_cases`).Scan(&source, &job, &original, &phase); e != nil || source != "interruption_ledger" || job.Valid || original != attempt.ID || phase != "awaiting_inventory" {
				t.Fatalf("false provenance: %s %+v %s %s %v", source, job, original, phase, e)
			}
			var jobsAfter string
			if e = db.SQL().QueryRow(`SELECT coalesce(json_group_array(json_object('id',id,'status',status,'available',available_at,'owner',claimed_by,'expires',claim_expires_at)),'[]') FROM outbox_jobs`).Scan(&jobsAfter); e != nil || jobsAfter != jobsBefore {
				t.Fatalf("discovery changed job ownership: %s %s %v", jobsBefore, jobsAfter, e)
			}
			s = task.NewService(db.SQL(), db, clock.Real{})
			if r, e = s.DiscoverRecoveryCases(context.Background(), 16); e != nil || r.Captured != 0 {
				t.Fatalf("restart duplicates: %+v %v", r, e)
			}
			current, e := s.Get(context.Background(), row.ID)
			if e != nil || current.State != row.State || current.Revision != row.Revision {
				t.Fatalf("Task changed: %+v %v", current, e)
			}
			if _, e = s.MarkReady(context.Background(), task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision}); !errors.Is(e, task.ErrRecoveryRequired) {
				t.Fatalf("discovery readmitted effects: %v", e)
			}
			if disposition == "pending" {
				if r, e := s.CaptureRecoveryCases(context.Background(), 1); e != nil || r.Captured != 0 || r.Acknowledged != 1 {
					t.Fatalf("delivery after discovery: %+v %v", r, e)
				}
			}
			for _, q := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM task_attempts WHERE status='interrupted'`} {
				var n int
				if e = db.SQL().QueryRow(q).Scan(&n); e != nil || n != 1 {
					t.Fatalf("lost/duplicate history %s: %d %v", q, n, e)
				}
			}
		})
	}
}

func TestRecoveryDiscoveryRejectsUnprovenLedger(t *testing.T) {
	for _, scenario := range []string{"missing", "foreign_workspace", "foreign_task", "duplicate", "oversized", "129_history", "noninterrupted"} {
		t.Run(scenario, func(t *testing.T) {
			db, s, row, attempt := interruptedAdmissionFixture(t)
			var e error
			switch scenario {
			case "missing":
				_, e = db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('missing',?,2,'interrupted','{}')`, row.ID)
				if e == nil {
					_, e = db.SQL().Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, attempt.ID)
				}
			case "noninterrupted":
				_, e = db.SQL().Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, attempt.ID)
			default:
				workspace, payload := "ws", fmt.Sprintf(`{"task_id":%q,"attempt_id":%q}`, row.ID, attempt.ID)
				if scenario == "foreign_workspace" {
					workspace = "foreign"
				}
				if scenario == "foreign_task" {
					payload = fmt.Sprintf(`{"task_id":"foreign","attempt_id":%q}`, attempt.ID)
				}
				if scenario == "oversized" {
					payload = fmt.Sprintf(`{"task_id":%q,"attempt_id":%q,"padding":%q}`, row.ID, attempt.ID, strings.Repeat("x", 65537))
				}
				// Append-only ledger: use a second incarnation with only the bad event
				// rather than altering immutable historical evidence.
				if scenario == "foreign_workspace" || scenario == "foreign_task" || scenario == "oversized" {
					if e == nil {
						_, e = db.SQL().Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, attempt.ID)
					}
					if e == nil {
						_, e = db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('bad',?,2,'interrupted','{}')`, row.ID)
					}
					switch scenario {
					case "foreign_workspace":
						payload = fmt.Sprintf(`{"task_id":%q,"attempt_id":"bad"}`, row.ID)
					case "foreign_task":
						payload = `{"task_id":"foreign","attempt_id":"bad"}`
					case "oversized":
						payload = fmt.Sprintf(`{"task_id":%q,"attempt_id":"bad","padding":%q}`, row.ID, strings.Repeat("x", 65537))
					}
				}
				count := 1
				if scenario == "129_history" {
					count = 128
				}
				for i := 0; e == nil && i < count; i++ {
					_, e = db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,?,'task.attempt_interrupted','task',?,?,1)`, fmt.Sprintf("bad-%03d", i), workspace, row.ID, payload)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			r, e := s.DiscoverRecoveryCases(context.Background(), 16)
			if e != nil || r.Captured != 0 {
				t.Fatalf("unproven history accepted: %+v %v", r, e)
			}
			var n int
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_recovery_cases`).Scan(&n); e != nil || n != 0 {
				t.Fatalf("case invented: %d %v", n, e)
			}
		})
	}
}

func TestRecoveryDiscoveryFairnessRollbackAndCursorWrap(t *testing.T) {
	db, s, row, _ := interruptedAdmissionFixture(t)
	for i := 0; i < 17; i++ {
		if _, e := db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES(?, ?, ?, 'interrupted','{}')`, fmt.Sprintf("000-invalid-%02d", i), row.ID, i+2); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.DiscoverRecoveryCases(context.Background(), 100)
	if e != nil || r.Scanned != 16 || r.Deferred != 16 {
		t.Fatalf("unbounded scan: %+v %v", r, e)
	}
	if _, e = db.SQL().Exec(`CREATE TRIGGER reject_discovery BEFORE INSERT ON events WHEN NEW.event_type='task.recovery_case_captured' BEGIN SELECT RAISE(ABORT,'audit denied'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DiscoverRecoveryCases(context.Background(), 16); e == nil {
		t.Fatal("audit failure hidden")
	}
	var n int
	if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_recovery_cases`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("partial commit: %d %v", n, e)
	}
	if _, e = db.SQL().Exec(`DROP TRIGGER reject_discovery`); e != nil {
		t.Fatal(e)
	}
	// The rejected original Attempt rotates past the failed transaction. Wrap
	// and bounded traversal of the old invalid prefix must still retry it.
	for pass := 0; pass < 3; pass++ {
		r, e = s.DiscoverRecoveryCases(context.Background(), 16)
		if e != nil || r.Scanned > 16 {
			t.Fatalf("failed cursor retry: %+v %v", r, e)
		}
		if r.Captured == 1 {
			break
		}
	}
	if r.Captured != 1 {
		t.Fatalf("failed cursor lost case after rotation: %+v", r)
	}
	if _, e = db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('000-earlier',?,30,'interrupted','{}')`, row.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('earlier-event','ws','task.attempt_interrupted','task',?,json_object('task_id',?,'attempt_id','000-earlier'),1)`, row.ID, row.ID); e != nil {
		t.Fatal(e)
	}
	if r, e = s.DiscoverRecoveryCases(context.Background(), 16); e != nil || r.Scanned != 0 {
		t.Fatalf("wrap reset: %+v %v", r, e)
	}
	if r, e = s.DiscoverRecoveryCases(context.Background(), 16); e != nil || r.Captured != 1 {
		t.Fatalf("earlier case starved: %+v %v", r, e)
	}
}

func TestRecoveryDiscoveryConcurrentDeliveryAndIndependentConsumers(t *testing.T) {
	db, _, row, _ := interruptedAdmissionFixture(t)
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
	if e = rows.Err(); e != nil {
		t.Fatal(e)
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
	for _, s := range services {
		wg.Add(1)
		go func(s *task.Service) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				if _, e := s.DiscoverRecoveryCases(context.Background(), 16); e != nil && !sqlitestore.IsBusy(e) {
					t.Errorf("discovery: %v", e)
				}
				if _, e := s.CaptureRecoveryCases(context.Background(), 16); e != nil && !sqlitestore.IsBusy(e) {
					t.Errorf("capture: %v", e)
				}
			}
		}(s)
	}
	wg.Wait()
	for _, q := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND status='succeeded'`} {
		var n int
		if e = db.SQL().QueryRow(q).Scan(&n); e != nil || n != 1 {
			t.Fatalf("duplicate/incomplete intake: %s %d %v", q, n, e)
		}
	}
	current, e := services[0].Get(context.Background(), row.ID)
	if e != nil || current.Revision != row.Revision {
		t.Fatalf("changed Task: %+v %v", current, e)
	}
}

func TestRecoveryDiscoveryCompleteHistoryBoundaryAndUTF8Cap(t *testing.T) {
	for _, count := range []int{128, 129} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db, s, row, _ := interruptedAdmissionFixture(t)
			for i := 1; i < count; i++ {
				if _, e := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES(?,'ws','task.attempt_interrupted','task',?,json_object('task_id',?,'attempt_id',?),1)`, fmt.Sprintf("history-%03d", i), row.ID, row.ID, fmt.Sprintf("old-%03d", i)); e != nil {
					t.Fatal(e)
				}
			}
			r, e := s.DiscoverRecoveryCases(context.Background(), 1)
			if e != nil || count == 128 && r.Captured != 1 || count == 129 && r.Captured != 0 {
				t.Fatalf("partial history accepted / full history denied: %+v %v", r, e)
			}
		})
	}
	t.Run("utf8_bytes", func(t *testing.T) {
		db, s, row, _ := interruptedAdmissionFixture(t)
		if _, e := db.SQL().Exec(`UPDATE outbox_jobs SET payload_json=json_set(payload_json,'$.padding',?) WHERE job_type='task.recovery.required'`, strings.Repeat("界", 23000)); e != nil {
			t.Fatal(e)
		}
		if r, e := s.CaptureRecoveryCases(context.Background(), 1); e != nil || r.Captured != 0 {
			t.Fatalf("oversized UTF8 job: %+v %v", r, e)
		}
		if _, e := db.SQL().Exec(`UPDATE task_attempts SET status='failed' WHERE task_id=?`, row.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('utf8',?,2,'interrupted','{}')`, row.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('utf8','ws','task.attempt_interrupted','task',?,json_object('task_id',?,'attempt_id','utf8','padding',?),1)`, row.ID, row.ID, strings.Repeat("界", 23000)); e != nil {
			t.Fatal(e)
		}
		if r, e := s.DiscoverRecoveryCases(context.Background(), 1); e != nil || r.Captured != 0 {
			t.Fatalf("oversized UTF8 ledger: %+v %v", r, e)
		}
	})
}
