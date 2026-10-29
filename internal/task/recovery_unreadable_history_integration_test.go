//go:build integration

package task_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// One readable matching interruption cannot establish unique identity when
// another event in the same bounded history cannot be interpreted safely.
func TestRecoveryCaptureAndDiscoveryRejectUnreadableAdditionalHistory(t *testing.T) {
	for _, mode := range []string{"delivery", "discovery"} {
		for _, scenario := range []string{"oversize", "utf8_bytes", "missing_identity", "wrong_shape", "foreign_workspace", "foreign_task"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				db, service, row, attempt := interruptedAdmissionFixture(t)
				workspace := "ws"
				payload := fmt.Sprintf(`{"task_id":%q,"attempt_id":%q}`, row.ID, attempt.ID)
				switch scenario {
				case "oversize":
					payload = fmt.Sprintf(`{"task_id":%q,"attempt_id":%q,"padding":%q}`, row.ID, attempt.ID, strings.Repeat("x", 65537))
				case "utf8_bytes":
					payload = fmt.Sprintf(`{"task_id":%q,"attempt_id":%q,"padding":%q}`, row.ID, attempt.ID, strings.Repeat("界", 23000))
				case "missing_identity":
					payload = `{}`
				case "wrong_shape":
					payload = `[]`
				case "foreign_workspace":
					workspace = "foreign"
				case "foreign_task":
					payload = fmt.Sprintf(`{"task_id":"foreign","attempt_id":%q}`, attempt.ID)
				}
				if _, e := db.SQL().Exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('unreadable',?,'task.attempt_interrupted','task',?,?,1)`, workspace, row.ID, payload); e != nil {
					t.Fatal(e)
				}
				if mode == "delivery" {
					r, e := service.CaptureRecoveryCases(context.Background(), 1)
					if e != nil || r.Captured != 0 || r.Acknowledged != 0 || r.Deferred != 1 {
						t.Fatalf("unreadable complete history accepted: %+v %v", r, e)
					}
				} else {
					r, e := service.DiscoverRecoveryCases(context.Background(), 1)
					if e != nil || r.Captured != 0 || r.Deferred != 1 {
						t.Fatalf("unreadable complete history accepted: %+v %v", r, e)
					}
				}
				for _, query := range []string{`SELECT COUNT(*) FROM task_recovery_cases`, `SELECT COUNT(*) FROM events WHERE event_type='task.recovery_case_captured'`, `SELECT COUNT(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND status='succeeded'`} {
					var n int
					if e := db.SQL().QueryRow(query).Scan(&n); e != nil || n != 0 {
						t.Fatalf("unproven intake changed durable state: %d %v", n, e)
					}
				}
			})
		}
	}
}
