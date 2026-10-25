//go:build integration

package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func approvalContinuationFixture(t *testing.T) (*sqlitestore.DB, *task.Service, task.Task, task.Attempt) {
	t.Helper()
	db, s := cancellationFixture(t)
	ctx := context.Background()
	if _, err := db.SQL().Exec(`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('approval-human','human','Owner','active',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	row, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Continue only the exact approved waiting owner"})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, original, err := s.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.WaitApproval(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	return db, s, row, original
}

func approvalContinuationCommand(row task.Task) task.TransitionCommand {
	actor := "approval-human"
	return task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision, ActorPrincipalID: &actor, Reason: "explicit approved continuation"}
}

func requireApprovalOwnerUnchanged(t *testing.T, db *sqlitestore.DB, s *task.Service, row task.Task, original task.Attempt) {
	t.Helper()
	got, err := s.Get(context.Background(), row.ID)
	if err != nil || got.State != row.State || got.Revision != row.Revision {
		t.Fatalf("Task changed: %+v %v", got, err)
	}
	var status string
	if err = db.SQL().QueryRow(`SELECT status FROM task_attempts WHERE id=?`, original.ID).Scan(&status); err != nil || status != "waiting" {
		t.Fatalf("original Attempt changed: %s %v", status, err)
	}
	var attempts, audits int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, row.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("Attempt count=%d err=%v", attempts, err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.resumed'`, row.ID).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("unexpected resume audit=%d err=%v", audits, err)
	}
}

func TestApprovalContinuationResumesExactAttemptAndAudit(t *testing.T) {
	db, _, row, original := approvalContinuationFixture(t)
	ctx := context.Background()
	// Reconstructing the trusted service cannot create another Attempt.
	s := task.NewService(db.SQL(), db, clock.Real{})
	err := db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		return s.ResumeApprovalAttemptInTransaction(ctx, tx, approvalContinuationCommand(row), original.ID)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, row.ID)
	if err != nil || got.State != task.StateRunning || got.Revision != row.Revision+1 {
		t.Fatalf("Task not resumed: %+v %v", got, err)
	}
	var status string
	var started, ended *int64
	if err = db.SQL().QueryRow(`SELECT status,started_at,ended_at FROM task_attempts WHERE id=?`, original.ID).Scan(&status, &started, &ended); err != nil || status != "running" || started == nil || *started != *original.StartedAt || ended != nil {
		t.Fatalf("original Attempt not preserved: %s %v %v %v", status, started, ended, err)
	}
	var attempts, audits int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, row.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("Attempt count=%d err=%v", attempts, err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.resumed' AND actor_principal_id='approval-human' AND json_extract(payload_json,'$.attempt_id')=? AND json_extract(payload_json,'$.from')='waiting_approval' AND json_extract(payload_json,'$.revision')=?`, row.ID, original.ID, row.Revision+1).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("missing exact approval audit=%d err=%v", audits, err)
	}
	if err = db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		return s.ResumeApprovalAttemptInTransaction(ctx, tx, approvalContinuationCommand(row), original.ID)
	}); !errors.Is(err, task.ErrRevisionConflict) {
		t.Fatalf("duplicate stale approval accepted: %v", err)
	}
}

func TestApprovalContinuationRejectsInvalidOwnersAndTaskStates(t *testing.T) {
	for _, scenario := range []string{"stale_revision", "foreign_attempt", "missing_attempt", "running_attempt", "ended_attempt", "unstarted_attempt", "manual_pause", "dependency_wait", "cancel_requested", "archived", "missing_actor", "blank_actor"} {
		t.Run(scenario, func(t *testing.T) {
			db, s, row, original := approvalContinuationFixture(t)
			cmd := approvalContinuationCommand(row)
			attemptID := original.ID
			query := ""
			want := task.ErrInvalidAttempt
			switch scenario {
			case "stale_revision":
				cmd.ExpectedRevision--
				want = task.ErrRevisionConflict
			case "foreign_attempt":
				attemptID = "foreign-owner"
			case "missing_attempt":
				query = `UPDATE task_attempts SET status='failed',ended_at=1 WHERE id=?`
			case "running_attempt":
				query = `UPDATE task_attempts SET status='running' WHERE id=?`
			case "ended_attempt":
				query = `UPDATE task_attempts SET ended_at=1 WHERE id=?`
			case "unstarted_attempt":
				query = `UPDATE task_attempts SET started_at=NULL WHERE id=?`
			case "manual_pause", "dependency_wait", "cancel_requested":
				state := map[string]task.State{"manual_pause": task.StatePaused, "dependency_wait": task.StateWaitingDependency, "cancel_requested": task.StateCancelRequested}[scenario]
				if _, err := db.SQL().Exec(`UPDATE tasks SET state=? WHERE id=?`, state, row.ID); err != nil {
					t.Fatal(err)
				}
				row.State = state
				want = task.ErrInvalidTransition
			case "archived":
				if _, err := db.SQL().Exec(`UPDATE tasks SET archived_at=1 WHERE id=?`, row.ID); err != nil {
					t.Fatal(err)
				}
				want = task.ErrInvalidTransition
			case "missing_actor":
				cmd.ActorPrincipalID = nil
				want = task.ErrInvalidCommand
			case "blank_actor":
				blank := "  "
				cmd.ActorPrincipalID = &blank
				want = task.ErrInvalidCommand
			}
			if query != "" {
				if _, err := db.SQL().Exec(query, original.ID); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			err := db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
				return s.ResumeApprovalAttemptInTransaction(ctx, tx, cmd, attemptID)
			})
			if !errors.Is(err, want) {
				t.Fatalf("rejection=%v want=%v", err, want)
			}
			// Attempt-corruption cases preserve their existing rows rather than
			// silently repairing them; all other cases retain the waiting owner.
			if query == "" {
				requireApprovalOwnerUnchanged(t, db, s, row, original)
			} else {
				got, err := s.Get(ctx, row.ID)
				if err != nil || got.State != row.State || got.Revision != row.Revision {
					t.Fatalf("invalid owner changed Task: %+v %v", got, err)
				}
			}
		})
	}
}

func TestApprovalContinuationHonorsAdmissionAndAtomicRollback(t *testing.T) {
	for _, scenario := range []string{"hard_dependency", "recovery", "root_hold", "attempt_write", "task_write", "audit_write", "caller_receipt_failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			db, s, row, original := approvalContinuationFixture(t)
			ctx := context.Background()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "hard_dependency":
				predecessor, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Unfinished prerequisite"})
				if err != nil {
					t.Fatal(err)
				}
				exec(`INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{}')`, row.ID, predecessor.ID)
			case "recovery":
				exec(`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('uncertain','ws','task.attempt_interrupted','task',?,'{}',1)`, row.ID)
			case "root_hold":
				exec(`INSERT INTO teams(id,workspace_id,name,status,created_by,created_at,updated_at) VALUES('held-team','ws','Held','active','approval-human',1,1)`)
				exec(`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,config_json,created_by,created_at,updated_at) VALUES('held-session','ws','held-team',?,'assembling','{}','approval-human',1,1)`, row.ID)
				exec(`INSERT INTO team_plans(id,workspace_id,session_id,version,status,plan_json,created_at) VALUES('held-plan','ws','held-session',1,'candidate','{}',1)`)
				exec(`INSERT INTO team_execution_holds(id,session_id,plan_id,root_task_id,created_by,created_at) VALUES('active-hold','held-session','held-plan',?,'approval-human',1)`, row.ID)
			case "attempt_write":
				exec(`CREATE TRIGGER reject_approval_attempt BEFORE UPDATE ON task_attempts BEGIN SELECT RAISE(ABORT,'attempt denied'); END`)
			case "task_write":
				exec(`CREATE TRIGGER reject_approval_task BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'Task denied'); END`)
			case "audit_write":
				exec(`CREATE TRIGGER reject_approval_audit BEFORE INSERT ON events WHEN NEW.event_type='task.resumed' BEGIN SELECT RAISE(ABORT,'audit denied'); END`)
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			receiptFailure := errors.New("applied answer receipt unavailable")
			err := db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
				if err := s.ResumeApprovalAttemptInTransaction(ctx, tx, approvalContinuationCommand(row), original.ID); err != nil {
					return err
				}
				if scenario == "caller_receipt_failure" {
					return receiptFailure
				}
				return nil
			})
			if err == nil {
				t.Fatal("approval bypassed admission or committed rejected writes")
			}
			want := map[string]error{"hard_dependency": task.ErrHardDependencyUnsatisfied, "recovery": task.ErrRecoveryRequired, "root_hold": teamgate.ErrPaused, "caller_receipt_failure": receiptFailure, "cancelled": context.Canceled}[scenario]
			if want != nil && !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			requireApprovalOwnerUnchanged(t, db, s, row, original)
			if scenario == "root_hold" {
				var held int
				if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_execution_holds WHERE id='active-hold' AND released_at IS NULL`).Scan(&held); err != nil || held != 1 {
					t.Fatalf("approval released hold=%d err=%v", held, err)
				}
			}
		})
	}
}
