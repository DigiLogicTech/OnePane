package team

import (
	"context"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestTeamCancellationSettlesInheritedPrerequisites(t *testing.T) {
	ctx := context.Background()
	var prerequisiteID string
	s, db, ss, _, _ := cancellationFixture(t, func(s *Service, db *sqlitestore.DB, ss Session) {
		prerequisite, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, Objective: "Existing prerequisite"})
		if err != nil {
			t.Fatal(err)
		}
		prerequisiteID = prerequisite.ID
		if _, err = db.SQL().Exec(`INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{}')`, ss.TaskID, prerequisiteID); err != nil {
			t.Fatal(err)
		}
	})
	var inherited int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM task_dependencies d JOIN team_execution_assignments a ON a.task_id=d.task_id WHERE a.session_id=? AND d.depends_on_task_id=?`, ss.ID, prerequisiteID).Scan(&inherited); err != nil || inherited != 2 {
		t.Fatalf("inherited prerequisites: %d %v", inherited, err)
	}
	if n, err := s.SettleUnstartedCancellations(ctx, 16); err != nil || n != 1 {
		t.Fatalf("settle inherited graph: %d %v", n, err)
	}
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil || root.State != task.StateCancelled {
		t.Fatalf("root: %+v %v", root, err)
	}
	prerequisite, err := s.tasks.Get(ctx, prerequisiteID)
	if err != nil || prerequisite.State != task.StateCreated || prerequisite.Revision != 1 {
		t.Fatalf("external prerequisite changed: %+v %v", prerequisite, err)
	}
}

func TestTeamCancellationDefersReverseAttribution(t *testing.T) {
	cases := []struct {
		name        string
		queue       bool
		wrongTask   bool
		reverseOnly bool
	}{
		{name: "queue_null_task", queue: true},
		{name: "queue_wrong_task", queue: true, wrongTask: true},
		{name: "reservation_null_task"},
		{name: "reservation_wrong_task", wrongTask: true},
		{name: "turn_reservation_without_request", reverseOnly: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, requestID, turnID := cancellationFixture(t)
			var owner any
			if tc.wrongTask {
				other, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, Objective: "Independent unrelated task"})
				if err != nil {
					t.Fatal(err)
				}
				owner = other.ID
			}
			if tc.queue {
				if _, err := db.SQL().Exec(`INSERT INTO runtime_execution_queue(id,workspace_id,task_id,inference_request_id,deployment_id,node_id,state,queued_at) SELECT 'misattributed-queue',workspace_id,?,id,deployment_id,execution_node_id,'running',1 FROM inference_requests WHERE id=?`, owner, requestID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.SQL().Exec(`INSERT INTO budget_accounts(id,workspace_id,name,unit,limit_amount,period_json,created_at,updated_at) VALUES('attribution-budget',?,'Test budget','tokens',100,'{}',1,1)`, ss.WorkspaceID); err != nil {
					t.Fatal(err)
				}
				var request any = requestID
				if tc.reverseOnly {
					request = nil
				}
				if _, err := db.SQL().Exec(`INSERT INTO budget_reservations(id,budget_account_id,task_id,inference_request_id,amount,status,reserved_at,expires_at) VALUES('misattributed-reservation','attribution-budget',?,?,1,'reserved',1,9999999999999)`, owner, request); err != nil {
					t.Fatal(err)
				}
				if _, err := db.SQL().Exec(`UPDATE team_turn_requests SET budget_reservation_id='misattributed-reservation' WHERE id=?`, turnID); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := s.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
				t.Fatalf("unsafe settlement: %d %v", n, err)
			}
			root, err := s.tasks.Get(ctx, ss.TaskID)
			if err != nil || root.State != task.StateCancelRequested {
				t.Fatalf("root changed: %+v %v", root, err)
			}
			var pending, audit int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='pending'`, ss.ID).Scan(&pending); err != nil || pending != 1 {
				t.Fatalf("pending turn changed: %d %v", pending, err)
			}
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type IN ('team.execution_cancelled','team.pending_turns_cancelled')`, ss.ID).Scan(&audit); err != nil || audit != 0 {
				t.Fatalf("unexpected audit: %d %v", audit, err)
			}
		})
	}
}

func TestTeamCancellationSettlesClosedReservation(t *testing.T) {
	for _, status := range []string{"committed", "released"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, requestID, turnID := cancellationFixture(t)
			if _, err := db.SQL().Exec(`INSERT INTO budget_accounts(id,workspace_id,name,unit,limit_amount,period_json,created_at,updated_at) VALUES('closed-budget',?,'Test budget','tokens',100,'{}',1,1)`, ss.WorkspaceID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL().Exec(`INSERT INTO budget_reservations(id,budget_account_id,task_id,inference_request_id,amount,status,reserved_at,expires_at,closed_at) VALUES('closed-reservation','closed-budget',?,?,0,?,1,9999999999999,2)`, ss.TaskID, requestID, status); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL().Exec(`UPDATE team_turn_requests SET budget_reservation_id='closed-reservation' WHERE id=?`, turnID); err != nil {
				t.Fatal(err)
			}
			if n, err := s.SettleUnstartedCancellations(ctx, 16); err != nil || n != 1 {
				t.Fatalf("settle closed reservation: %d %v", n, err)
			}
			var retained string
			if err := db.SQL().QueryRow(`SELECT status FROM budget_reservations WHERE id='closed-reservation'`).Scan(&retained); err != nil || retained != status {
				t.Fatalf("reservation changed: %s %v", retained, err)
			}
		})
	}
}

func TestTeamCancellationDefersReverseInference(t *testing.T) {
	for _, reference := range []string{"session", "turn"} {
		for _, wrongTask := range []bool{false, true} {
			name := reference + "_null_task"
			if wrongTask {
				name = reference + "_wrong_task"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				s, db, ss, requestID, _ := cancellationFixture(t)
				var owner any
				if wrongTask {
					other, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, Objective: "Independent unrelated task"})
					if err != nil {
						t.Fatal(err)
					}
					owner = other.ID
				}
				remove := "$.turn_id"
				if reference == "turn" {
					remove = "$.session_id"
				}
				if _, err := db.SQL().Exec(`INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,deployment_id,origin_node_id,execution_node_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) SELECT 'reverse-request',workspace_id,?,principal_id,deployment_id,origin_node_id,execution_node_id,'executing',capability_json,json_remove(context_manifest_json,?),classification_json,request_json,created_at,updated_at FROM inference_requests WHERE id=?`, owner, remove, requestID); err != nil {
					t.Fatal(err)
				}
				if n, err := s.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
					t.Fatalf("unsafe reverse inference settlement: %d %v", n, err)
				}
				root, err := s.tasks.Get(ctx, ss.TaskID)
				if err != nil || root.State != task.StateCancelRequested {
					t.Fatalf("root changed: %+v %v", root, err)
				}
			})
		}
	}
}

func TestTeamCancellationDefersOversizedHistoriesAndRotates(t *testing.T) {
	for _, history := range []string{"turns", "requests"} {
		t.Run(history, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, requestID, turnID := cancellationFixture(t)
			var query, cleanup string
			var source string
			if history == "turns" {
				// Two existing turns plus 127 unclaimed turns exceed the cap.
				query = `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<127) INSERT INTO team_turn_requests(id,workspace_id,session_id,member_id,status,created_at,updated_at,round_number) SELECT printf('oversized-turn-%d',seq.n),tr.workspace_id,tr.session_id,tr.member_id,'pending',1,1,tr.round_number FROM seq CROSS JOIN team_turn_requests tr WHERE tr.id=?`
				cleanup = `DELETE FROM team_turn_requests WHERE id LIKE 'oversized-turn-%'`
				source = turnID
			} else {
				query = `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<128) INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) SELECT printf('oversized-request-%d',seq.n),q.workspace_id,q.task_id,q.principal_id,'unknown',q.capability_json,q.context_manifest_json,q.classification_json,q.request_json,1,1 FROM seq CROSS JOIN inference_requests q WHERE q.id=?`
				cleanup = `DELETE FROM inference_requests WHERE id LIKE 'oversized-request-%'`
				source = requestID
			}
			if _, err := db.SQL().Exec(query, source); err != nil {
				t.Fatal(err)
			}
			if n, err := s.SettleUnstartedCancellations(ctx, 1); err != nil || n != 0 {
				t.Fatalf("oversized history settled: %d %v", n, err)
			}
			root, err := s.tasks.Get(ctx, ss.TaskID)
			if err != nil || root.State != task.StateCancelRequested {
				t.Fatalf("oversized history changed root: %+v %v", root, err)
			}
			if _, err = db.SQL().Exec(cleanup); err != nil {
				t.Fatal(err)
			}
			// Exhausting the cursor resets the scan, then the now-eligible root
			// must be revisited rather than stranded by the earlier deferral.
			if n, err := s.SettleUnstartedCancellations(ctx, 1); err != nil || n != 0 {
				t.Fatalf("cursor reset: %d %v", n, err)
			}
			if n, err := s.SettleUnstartedCancellations(ctx, 1); err != nil || n != 1 {
				t.Fatalf("healthy retry after rotation: %d %v", n, err)
			}
			independent := NewService(db.SQL(), db, clock.Real{}, s.tasks)
			if n, err := independent.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
				t.Fatalf("independent service duplicated settlement: %d %v", n, err)
			}
		})
	}
}
