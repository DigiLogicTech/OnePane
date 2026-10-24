//go:build integration

package task_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

var cancellationTemplate struct {
	sync.Once
	data []byte
	err  error
}

func TestUnstartedCancellationAfterReadyPauseRequiresExactHistory(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_pause_%t", corrupt), func(t *testing.T) {
			db, s := cancellationFixture(t)
			ctx := context.Background()
			row, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Never dispatched"})
			if err != nil {
				t.Fatal(err)
			}
			row, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			if err != nil {
				t.Fatal(err)
			}
			row, err = s.Pause(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			if err != nil {
				t.Fatal(err)
			}
			row, err = s.RequestCancel(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if _, err = db.SQL().Exec(`DROP TRIGGER events_no_delete`); err != nil {
					t.Fatal(err)
				}
				if _, err = db.SQL().Exec(`DELETE FROM events WHERE event_type='task.paused'`); err != nil {
					t.Fatal(err)
				}
			}
			n, err := s.SettleUnstartedCancellations(ctx, 16)
			if err != nil {
				t.Fatal(err)
			}
			want, state := 1, task.StateCancelled
			if corrupt {
				want, state = 0, task.StateCancelRequested
			}
			if n != want {
				t.Fatalf("settled %d want %d", n, want)
			}
			got, err := s.Get(ctx, row.ID)
			if err != nil || got.State != state {
				t.Fatalf("state %+v %v", got, err)
			}
		})
	}
}

func cancellationFixture(t *testing.T) (*sqlitestore.DB, *task.Service) {
	t.Helper()
	path := t.TempDir() + "/cancel.db"
	// Run the actual migrations once, then clone their fully checkpointed,
	// closed output. Every case still owns a separate durable database; race
	// coverage spends its budget on lifecycle behavior rather than repeatedly
	// interpreting the same hundred migrations under the race detector.
	cancellationTemplate.Do(func() {
		template, err := sqlitestore.Open(path + ".template")
		if err != nil {
			cancellationTemplate.err = err
			return
		}
		err = template.Migrate(context.Background())
		if err == nil {
			_, err = template.SQL().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
		closeErr := template.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			cancellationTemplate.data, err = os.ReadFile(path + ".template")
		}
		cancellationTemplate.err = err
	})
	if cancellationTemplate.err != nil {
		t.Fatal(cancellationTemplate.err)
	}
	if err := os.WriteFile(path, cancellationTemplate.data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Prove readers are closed before transactional work on tiny hosts.
	db.SQL().SetMaxOpenConns(1)
	for _, ws := range []string{"ws", "foreign"} {
		if _, err = db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,1,1)`, ws, ws); err != nil {
			t.Fatal(err)
		}
	}
	return db, task.NewService(db.SQL(), db, clock.Real{})
}

func cancellingTask(t *testing.T, db *sqlitestore.DB, svc *task.Service, id, parent, ws string) {
	t.Helper()
	var parentID any
	if parent != "" {
		parentID = parent
	}
	if _, err := db.SQL().Exec(`INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,revision,created_at,updated_at)
	 VALUES(?,?,?,?,'created','{}',1,1,1)`, id, ws, parentID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestCancel(context.Background(), task.TransitionCommand{TaskID: id, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
}

func requireCancellationState(t *testing.T, svc *task.Service, id string, state task.State, revision int64) {
	t.Helper()
	got, err := svc.Get(context.Background(), id)
	if err != nil || got.State != state || got.Revision != revision {
		t.Fatalf("%s: state=%s revision=%d err=%v", id, got.State, got.Revision, err)
	}
}

func TestUnstartedCancellationGraphAtomicAndRestartIdempotent(t *testing.T) {
	db, svc := cancellationFixture(t)
	ctx := context.Background()
	cancellingTask(t, db, svc, "a-root", "", "ws")
	cancellingTask(t, db, svc, "z-child", "a-root", "ws")
	if _, err := db.SQL().Exec(`CREATE TRIGGER reject_cancel_audit BEFORE INSERT ON events
	 WHEN NEW.event_type='task.cancelled' AND NEW.aggregate_id='z-child'
	 BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.SettleUnstartedCancellations(ctx, 1); err == nil || n != 0 {
		t.Fatalf("audit failure count=%d err=%v", n, err)
	}
	for _, id := range []string{"a-root", "z-child"} {
		requireCancellationState(t, svc, id, task.StateCancelRequested, 2)
	}
	var audits int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='task.cancelled'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("partial audits=%d err=%v", audits, err)
	}
	if _, err := db.SQL().Exec(`DROP TRIGGER reject_cancel_audit`); err != nil {
		t.Fatal(err)
	}
	// Reconstructed service has no cursor or in-memory cancellation state.
	svc = task.NewService(db.SQL(), db, clock.Real{})
	if n, err := svc.SettleUnstartedCancellations(ctx, 1); err != nil || n != 2 {
		t.Fatalf("settlement=%d err=%v", n, err)
	}
	for _, id := range []string{"a-root", "z-child"} {
		requireCancellationState(t, svc, id, task.StateCancelled, 3)
	}
	svc = task.NewService(db.SQL(), db, clock.Real{})
	if n, err := svc.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
		t.Fatalf("restart replay=%d err=%v", n, err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='task.cancelled'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}

func TestUnstartedCancellationHistoricalEvidenceAndFairness(t *testing.T) {
	for _, status := range []string{"created", "queued", "running", "waiting", "succeeded", "failed", "cancelled", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			db, svc := cancellationFixture(t)
			cancellingTask(t, db, svc, "a-history", "", "ws")
			cancellingTask(t, db, svc, "z-clean", "", "ws")
			if _, err := db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('old','a-history',1,?,'{}')`, status); err != nil {
				t.Fatal(err)
			}
			if n, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil || n != 0 {
				t.Fatalf("historical attempt settled: %d %v", n, err)
			}
			if n, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil || n != 1 {
				t.Fatalf("clean task starved: %d %v", n, err)
			}
			requireCancellationState(t, svc, "a-history", task.StateCancelRequested, 2)
			requireCancellationState(t, svc, "z-clean", task.StateCancelled, 3)
		})
	}
}

func TestUnstartedCancellationDefersUnsafeGraphs(t *testing.T) {
	for _, kind := range []string{"foreign", "uncancelled", "historical_child", "completed_child", "oversized", "result", "started_event", "missing_intent", "running_origin", "power_hold", "wake_attempt", "global_lease", "artifact_session"} {
		t.Run(kind, func(t *testing.T) {
			db, svc := cancellationFixture(t)
			cancellingTask(t, db, svc, "a-root", "", "ws")
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "foreign":
				cancellingTask(t, db, svc, "z-child", "a-root", "foreign")
			case "artifact_session":
				cancellingTask(t, db, svc, "z-child", "a-root", "ws")
				exec(`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('human','human','Owner','active',1,1)`)
				exec(`INSERT INTO artifact_sessions(id,workspace_id,title,format,status,created_by,created_at,updated_at) VALUES('a','ws','Other result','text','active','human',1,1)`)
				exec(`UPDATE tasks SET artifact_session_id='a' WHERE id='z-child'`)
			case "uncancelled", "completed_child", "historical_child":
				cancellingTask(t, db, svc, "z-child", "a-root", "ws")
				if kind == "uncancelled" {
					exec(`UPDATE tasks SET state='ready' WHERE id='z-child'`)
				}
				if kind == "completed_child" {
					exec(`UPDATE tasks SET state='complete',result_json='{"delivered":true}' WHERE id='z-child'`)
				}
				if kind == "historical_child" {
					exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('unknown','z-child',1,'interrupted','{}')`)
				}
			case "oversized":
				for i := 0; i < 256; i++ {
					cancellingTask(t, db, svc, fmt.Sprintf("z-%03d", i), "a-root", "ws")
				}
			case "result":
				exec(`UPDATE tasks SET result_json='{"delivered":true}' WHERE id='a-root'`)
			case "started_event":
				exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('started','ws','task.started','task','a-root','{}',1)`)
			case "missing_intent":
				exec(`UPDATE tasks SET revision=3 WHERE id='a-root'`)
			case "running_origin":
				exec(`UPDATE tasks SET state='running',revision=3 WHERE id='a-root'`)
				if _, err := svc.RequestCancel(context.Background(), task.TransitionCommand{TaskID: "a-root", ExpectedRevision: 3}); err != nil {
					t.Fatal(err)
				}
			case "power_hold":
				exec(`INSERT INTO node_power_holds(lease_id,origin_node_id,task_id,expires_at,created_at,updated_at) VALUES('hold','node','a-root','later','now','now')`)
			case "wake_attempt":
				exec(`INSERT INTO node_wake_attempts(id,node_id,task_id,provider,state,requested_at,deadline_at,updated_at) VALUES('wake','node','a-root','wol','failed','now','later','now')`)
			case "global_lease":
				exec(`INSERT INTO global_resource_identities(resource_uid,kind,canonical_identity,location_json,updated_at) VALUES('resource','file','test-file','{}',1)`)
				exec(`INSERT INTO global_resource_leases(id,resource_uid,lease_mode,status,owner_json,requested_at) VALUES('lease','resource','write','released','{"task_id":"a-root"}',1)`)
			}
			if n, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil || n != 0 {
				t.Fatalf("unsafe graph settled=%d err=%v", n, err)
			}
			got, err := svc.Get(context.Background(), "a-root")
			if err != nil || got.State != task.StateCancelRequested {
				t.Fatalf("root=%+v err=%v", got, err)
			}
			if kind == "completed_child" {
				requireCancellationState(t, svc, "z-child", task.StateComplete, 2)
			}
		})
	}
}

func TestUnstartedCancellationBoundedScanAndCursorWrap(t *testing.T) {
	db, svc := cancellationFixture(t)
	cancellingTask(t, db, svc, "a-history", "", "ws")
	if _, err := db.SQL().Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('old','a-history',1,'failed','{}')`); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 17} {
		if _, err := svc.SettleUnstartedCancellations(context.Background(), limit); err == nil {
			t.Fatal("accepted invalid bound")
		}
	}
	if _, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	// New lower-key requests become visible after a bounded empty wrap tick.
	cancellingTask(t, db, svc, "0-new", "", "ws")
	if n, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil || n != 0 {
		t.Fatalf("wrap=%d %v", n, err)
	}
	if n, err := svc.SettleUnstartedCancellations(context.Background(), 1); err != nil || n != 1 {
		t.Fatalf("lower key starved=%d %v", n, err)
	}
}

func TestUnstartedCancellationWholePassGraphBudget(t *testing.T) {
	db, svc := cancellationFixture(t)
	for _, root := range []string{"a-root", "b-root"} {
		cancellingTask(t, db, svc, root, "", "ws")
		for i := 0; i < 159; i++ {
			cancellingTask(t, db, svc, fmt.Sprintf("z-%s-%03d", root, i), root, "ws")
		}
	}
	if n, err := svc.SettleUnstartedCancellations(context.Background(), 16); err != nil || n != 160 {
		t.Fatalf("whole-pass budget escaped: %d %v", n, err)
	}
	requireCancellationState(t, svc, "a-root", task.StateCancelled, 3)
	requireCancellationState(t, svc, "b-root", task.StateCancelRequested, 2)
	// Cursor progress eventually revisits the deferred graph without settling
	// a truncated prefix as if it were the complete descendant graph.
	for i := 0; i < 30; i++ {
		if _, err := svc.SettleUnstartedCancellations(context.Background(), 16); err != nil {
			t.Fatal(err)
		}
	}
	requireCancellationState(t, svc, "b-root", task.StateCancelled, 3)
}

func TestUnstartedCancellationConcurrentMaintenanceIsIdempotent(t *testing.T) {
	db, svc := cancellationFixture(t)
	cancellingTask(t, db, svc, "a-root", "", "ws")
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.SettleUnstartedCancellations(context.Background(), 1)
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	requireCancellationState(t, svc, "a-root", task.StateCancelled, 3)
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='task.cancelled'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate audit=%d %v", count, err)
	}
}

func TestUnstartedCancellationIndependentServicesAndLedgerIndex(t *testing.T) {
	db, svc := cancellationFixture(t)
	cancellingTask(t, db, svc, "a-root", "", "ws")
	var path string
	var seq int
	var name string
	if err := db.SQL().QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	other, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	second := task.NewService(other.SQL(), other, clock.Real{})
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, service := range []*task.Service{svc, second} {
		go func(service *task.Service) {
			<-start
			_, err := service.SettleUnstartedCancellations(context.Background(), 1)
			results <- err
		}(service)
	}
	close(start)
	for i := 0; i < 2; i++ {
		// SQLite may reject an obsolete read snapshot when another writer
		// commits. That transaction must roll back and be safely retryable.
		if err := <-results; err != nil && !sqlitestore.IsBusy(err) {
			t.Fatal(err)
		}
	}
	for _, service := range []*task.Service{svc, second} {
		if _, err := service.SettleUnstartedCancellations(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
	}
	requireCancellationState(t, svc, "a-root", task.StateCancelled, 3)
	var audits int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='task.cancelled'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("cross-instance audit=%d err=%v", audits, err)
	}
	for _, query := range []string{
		`SELECT 1 FROM events WHERE aggregate_type='task' AND aggregate_id='a-root' AND event_type='task.cancel_requested' AND json_extract(payload_json,'$.revision')=2 AND json_extract(payload_json,'$.from') IN ('created','ready')`,
		`SELECT 1 FROM events WHERE aggregate_type='task' AND aggregate_id='a-root' AND event_type IN ('task.started','task.attempt_interrupted','task.completed')`,
	} {
		rows, err := db.SQL().Query("EXPLAIN QUERY PLAN " + query)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !strings.Contains(plan, "idx_events_task_execution_evidence") {
			t.Fatalf("unbounded ledger lookup: %s", plan)
		}
	}
}
