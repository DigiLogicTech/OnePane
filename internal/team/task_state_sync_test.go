package team

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The clock boundary runs after the read snapshot and before its application.
// This makes concurrent durable changes deterministic without sleeps.
type syncBoundaryClock struct{ beforeApply func() }

func (c syncBoundaryClock) Now() time.Time { return time.UnixMilli(c.UnixMilli()) }
func (c syncBoundaryClock) UnixMilli() int64 {
	if c.beforeApply != nil {
		c.beforeApply()
	}
	return 100
}

func TestSyncTaskStatesRejectsStaleSnapshots(t *testing.T) {
	for _, change := range []string{"session_status", "session_revision", "task_state", "task_revision"} {
		t.Run(change, func(t *testing.T) {
			s, db, ss, _ := liveRoomFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := db.SQL().ExecContext(ctx, `UPDATE team_sessions SET status='plan_accepted' WHERE id=?`, ss.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL().ExecContext(ctx, `UPDATE tasks SET state='running' WHERE id=?`, ss.TaskID); err != nil {
				t.Fatal(err)
			}
			s.clock = syncBoundaryClock{beforeApply: func() {
				var query string
				var id string
				switch change {
				case "session_status":
					query, id = `UPDATE team_sessions SET status='cancelled',revision=revision+1 WHERE id=?`, ss.ID
				case "session_revision":
					query, id = `UPDATE team_sessions SET revision=revision+1 WHERE id=?`, ss.ID
				case "task_state":
					query, id = `UPDATE tasks SET state='cancel_requested',revision=revision+1 WHERE id=?`, ss.TaskID
				case "task_revision":
					query, id = `UPDATE tasks SET revision=revision+1 WHERE id=?`, ss.TaskID
				}
				if _, err := db.SQL().ExecContext(ctx, query, id); err != nil {
					t.Fatal(err)
				}
			}}
			if err := s.SyncTaskStates(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := s.Session(ctx, ss.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "plan_accepted"
			if change == "session_status" {
				want = "cancelled"
			}
			if got.Status != want {
				t.Fatalf("stale snapshot changed session to %s; want %s", got.Status, want)
			}
		})
	}
}

func TestSyncTaskStatesPropagatesWriteFailure(t *testing.T) {
	s, db, ss, _ := liveRoomFixture(t)
	if _, err := db.SQL().Exec(`UPDATE tasks SET state='complete' WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`CREATE TRIGGER reject_session_sync BEFORE UPDATE OF status ON team_sessions
	 WHEN NEW.status='completed' BEGIN SELECT RAISE(ABORT,'session sync rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncTaskStates(context.Background()); err == nil || !strings.Contains(err.Error(), "session sync rejected") {
		t.Fatalf("write failure hidden: %v", err)
	}
	got, err := s.Session(context.Background(), ss.ID)
	if err != nil || got.Status != ss.Status || got.Revision != ss.Revision {
		t.Fatalf("rejected write changed session: %+v, %v", got, err)
	}
}

func TestSyncTaskStatesSingleConnectionAndIdempotence(t *testing.T) {
	s, db, ss, _ := liveRoomFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.SQL().ExecContext(ctx, `UPDATE tasks SET state='complete' WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.SyncTaskStates(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Session(ctx, ss.ID)
	if err != nil || got.Status != "completed" || got.Revision != ss.Revision+1 {
		t.Fatalf("projection not idempotent: %+v, %v", got, err)
	}
}
