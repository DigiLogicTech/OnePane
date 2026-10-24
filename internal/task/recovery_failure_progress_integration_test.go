//go:build integration

package task_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

// A durable write failure belongs to its original case. It must remain visible
// and retryable without pinning the bounded maintenance cursor forever.
func TestRecoveryCaptureAndDiscoveryWriteFailureIndependentProgress(t *testing.T) {
	for _, mode := range []string{"delivery", "discovery"} {
		for _, limit := range []int{1, 16} {
			t.Run(fmt.Sprintf("%s/limit_%d", mode, limit), func(t *testing.T) {
				db, service, original, _ := interruptedAdmissionFixture(t)
				ctx := context.Background()
				other, err := service.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Independent recovery intake"})
				if err != nil {
					t.Fatal(err)
				}
				other, err = service.MarkReady(ctx, task.TransitionCommand{TaskID: other.ID, ExpectedRevision: other.Revision})
				if err != nil {
					t.Fatal(err)
				}
				other, _, err = service.Start(ctx, task.StartCommand{TaskID: other.ID, ExpectedRevision: other.Revision})
				if err != nil {
					t.Fatal(err)
				}
				other, err = service.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: other.ID, ExpectedRevision: other.Revision})
				if err != nil {
					t.Fatal(err)
				}
				selector := `SELECT json_extract(payload_json,'$.task_id') FROM outbox_jobs WHERE job_type='task.recovery.required' ORDER BY id LIMIT 1`
				if mode == "discovery" {
					selector = `SELECT task_id FROM task_attempts WHERE status='interrupted' ORDER BY id LIMIT 1`
				}
				var rejected string
				if err = db.SQL().QueryRow(selector).Scan(&rejected); err != nil {
					t.Fatal(err)
				}
				healthy := original.ID
				if healthy == rejected {
					healthy = other.ID
				}
				// Simulate a persistent case-specific audit constraint failure.
				if _, err = db.SQL().Exec(`CREATE TRIGGER reject_one_recovery_audit BEFORE INSERT ON events WHEN NEW.event_type='task.recovery_case_captured' AND NEW.aggregate_id='` + rejected + `' BEGIN SELECT RAISE(ABORT,'case audit unavailable'); END`); err != nil {
					t.Fatal(err)
				}
				run := func() (int, int, error) {
					if mode == "delivery" {
						r, e := service.CaptureRecoveryCases(ctx, limit)
						return r.Scanned, r.Captured, e
					}
					r, e := service.DiscoverRecoveryCases(ctx, limit)
					return r.Scanned, r.Captured, e
				}
				scanned, captured, err := run()
				if err == nil {
					t.Fatal("audit failure hidden")
				}
				if limit == 16 && (scanned != 2 || captured != 1) {
					t.Fatalf("same-pass independent capture lost: scanned=%d captured=%d err=%v", scanned, captured, err)
				}
				if limit == 1 {
					if scanned != 1 || captured != 0 {
						t.Fatalf("failed record reported success: %d %d", scanned, captured)
					}
					if _, captured, err = run(); err != nil || captured != 1 {
						t.Fatalf("failed prefix pinned next pass: captured=%d err=%v", captured, err)
					}
				}
				for _, check := range []struct {
					query, id string
					count     int
				}{
					{`SELECT COUNT(*) FROM task_recovery_cases WHERE task_id=?`, rejected, 0},
					{`SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured' AND aggregate_id=?`, rejected, 0},
					{`SELECT COUNT(*) FROM task_recovery_cases WHERE task_id=?`, healthy, 1},
					{`SELECT COUNT(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND json_extract(payload_json,'$.task_id')=? AND status='pending'`, rejected, 1},
				} {
					var n int
					if e := db.SQL().QueryRow(check.query, check.id).Scan(&n); e != nil || n != check.count {
						t.Fatalf("rollback/ownership %s: got %d want %d err=%v", check.query, n, check.count, e)
					}
				}
				if _, err = db.SQL().Exec(`DROP TRIGGER reject_one_recovery_audit`); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 4; i++ {
					if _, _, err = run(); err != nil {
						t.Fatal(err)
					}
				}
				for _, check := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM task_attempts WHERE status='interrupted'`} {
					var n int
					if err = db.SQL().QueryRow(check).Scan(&n); err != nil || n != 2 {
						t.Fatalf("retry lost/duplicated evidence: %s %d %v", check, n, err)
					}
				}
				for _, row := range []task.Task{original, other} {
					current, e := service.Get(ctx, row.ID)
					if e != nil || current.State != row.State || current.Revision != row.Revision {
						t.Fatalf("intake changed original execution: %+v %v", current, e)
					}
					if _, e = service.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision}); e == nil {
						t.Fatal("intake bypassed recovery admission")
					}
				}
			})
		}
	}
}
