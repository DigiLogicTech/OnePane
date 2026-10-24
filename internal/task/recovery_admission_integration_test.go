//go:build integration

package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

func TestInterruptedRecoveryCannotReadmitOriginalTask(t *testing.T) {
	db, s := cancellationFixture(t)
	ctx := context.Background()
	row, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Never replay an uncertain dispatched effect"})
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
	row, err = s.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision, Reason: "unknown external result"})
	if err != nil {
		t.Fatal(err)
	}
	ready, readyErr := s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if readyErr == nil {
		_, duplicate, startErr := s.Start(ctx, task.StartCommand{TaskID: ready.ID, ExpectedRevision: ready.Revision})
		t.Errorf("unreconciled Task admitted ready; new Attempt=%s original=%s start_error=%v", duplicate.ID, original.ID, startErr)
	}
	var attempts int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, row.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("original Attempt not preserved exclusively: count=%d error=%v", attempts, err)
	}
	current, err := s.Get(ctx, row.ID)
	if err != nil || current.State != task.StateBlocked || current.Revision != row.Revision {
		t.Fatalf("recovery Task changed: %+v %v", current, err)
	}
}

func interruptedAdmissionFixture(t *testing.T) (*sqlitestore.DB, *task.Service, task.Task, task.Attempt) {
	t.Helper()
	db, s := cancellationFixture(t)
	ctx := context.Background()
	row, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Require durable recovery disposition"})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, attempt, err := s.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision, Reason: "unknown external result"})
	if err != nil {
		t.Fatal(err)
	}
	return db, s, row, attempt
}

func TestRecoveryAdmissionStaleStateAndJobDisposition(t *testing.T) {
	for _, scenario := range []string{"ready_start", "paused_resume", "job_consumed", "job_deleted", "ledger_without_attempt"} {
		t.Run(scenario, func(t *testing.T) {
			db, s, row, attempt := interruptedAdmissionFixture(t)
			ctx := context.Background()
			switch scenario {
			case "ready_start", "paused_resume":
				state := "ready"
				if scenario == "paused_resume" {
					state = "paused"
				}
				// Legacy state changes cannot erase the original durable interruption.
				if _, err := db.SQL().Exec(`UPDATE tasks SET state=? WHERE id=?`, state, row.ID); err != nil {
					t.Fatal(err)
				}
			case "job_consumed":
				if _, err := db.SQL().Exec(`UPDATE outbox_jobs SET status='succeeded' WHERE job_type='task.recovery.required'`); err != nil {
					t.Fatal(err)
				}
			case "job_deleted":
				if _, err := db.SQL().Exec(`DELETE FROM outbox_jobs WHERE job_type='task.recovery.required'`); err != nil {
					t.Fatal(err)
				}
			case "ledger_without_attempt":
				if _, err := db.SQL().Exec(`DELETE FROM task_attempts WHERE id=?`, attempt.ID); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			switch scenario {
			case "ready_start":
				_, _, err = s.Start(ctx, task.StartCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			case "paused_resume":
				_, err = s.Resume(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			default:
				_, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision})
			}
			if !errors.Is(err, task.ErrRecoveryRequired) {
				t.Fatalf("recovery fence: %v", err)
			}
			current, err := s.Get(ctx, row.ID)
			if err != nil || current.Revision != row.Revision {
				t.Fatalf("denial mutated Task: %+v %v", current, err)
			}
			var attempts int
			want := 1
			if scenario == "ledger_without_attempt" {
				want = 0
			}
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, row.ID).Scan(&attempts); err != nil || attempts != want {
				t.Fatalf("unexpected Attempts: %d %v", attempts, err)
			}
		})
	}
}

func TestRecoveryAdmissionDescendantWakeAndVerificationRetry(t *testing.T) {
	for _, scenario := range []string{"waiting_resume", "verification_retry"} {
		t.Run(scenario, func(t *testing.T) {
			db, s := cancellationFixture(t)
			ctx := context.Background()
			parent, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Delegating parent"})
			if err != nil {
				t.Fatal(err)
			}
			parent, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: parent.ID, ExpectedRevision: parent.Revision})
			if err != nil {
				t.Fatal(err)
			}
			parent, _, err = s.Start(ctx, task.StartCommand{TaskID: parent.ID, ExpectedRevision: parent.Revision})
			if err != nil {
				t.Fatal(err)
			}
			child, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", ParentTaskID: &parent.ID, Objective: "Already active child"})
			if err != nil {
				t.Fatal(err)
			}
			child, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
			if err != nil {
				t.Fatal(err)
			}
			child, original, err := s.Start(ctx, task.StartCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "waiting_resume" {
				child, err = s.WaitDependency(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
			} else {
				child, err = s.RequestCompletion(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
				if err != nil {
					t.Fatal(err)
				}
				child, err = s.BeginVerification(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: parent.ID, ExpectedRevision: parent.Revision}); err != nil {
				t.Fatal(err)
			}
			if scenario == "waiting_resume" {
				err = db.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
					return s.ResumeWaitingAttemptInTransaction(ctx, tx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision}, original.ID)
				})
			} else {
				_, err = s.ContinueAfterVerification(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
			}
			if !errors.Is(err, task.ErrRecoveryRequired) {
				t.Fatalf("ancestor recovery bypass: %v", err)
			}
			current, err := s.Get(ctx, child.ID)
			if err != nil || current.State != child.State || current.Revision != child.Revision {
				t.Fatalf("child changed: %+v %v", current, err)
			}
			var state string
			if err = db.SQL().QueryRow(`SELECT status FROM task_attempts WHERE id=?`, original.ID).Scan(&state); err != nil || state != "waiting" {
				t.Fatalf("original child Attempt changed: %s %v", state, err)
			}
		})
	}
}

func TestRecoveryAdmissionOrdinaryBlockedAndIndependentProgress(t *testing.T) {
	_, s, interrupted, _ := interruptedAdmissionFixture(t)
	ctx := context.Background()
	healthy, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Independent healthy work"})
	if err != nil {
		t.Fatal(err)
	}
	healthy, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: healthy.ID, ExpectedRevision: healthy.Revision})
	if err != nil {
		t.Fatal(err)
	}
	healthy, err = s.MarkBlocked(ctx, task.TransitionCommand{TaskID: healthy.ID, ExpectedRevision: healthy.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: interrupted.ID, ExpectedRevision: interrupted.Revision}); !errors.Is(err, task.ErrRecoveryRequired) {
		t.Fatalf("interrupted admission: %v", err)
	}
	healthy, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: healthy.ID, ExpectedRevision: healthy.Revision})
	if err != nil {
		t.Fatalf("ordinary blocked Task denied: %v", err)
	}
	if _, _, err = s.Start(ctx, task.StartCommand{TaskID: healthy.ID, ExpectedRevision: healthy.Revision}); err != nil {
		t.Fatalf("independent work stranded: %v", err)
	}
}

func TestRecoveryAdmissionRejectsInvalidAncestry(t *testing.T) {
	for _, scenario := range []string{"too_deep", "cycle", "foreign"} {
		t.Run(scenario, func(t *testing.T) {
			db, s := cancellationFixture(t)
			ctx := context.Background()
			root, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Ancestry root"})
			if err != nil {
				t.Fatal(err)
			}
			leaf := root
			if scenario == "too_deep" {
				for i := 0; i < 256; i++ {
					id := fmt.Sprintf("ancestry-%03d", i)
					if _, err = db.SQL().Exec(`INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,revision,created_at,updated_at) VALUES(?,'ws',?,?,'created','{}',1,1,1)`, id, leaf.ID, id); err != nil {
						t.Fatal(err)
					}
					leaf, err = s.Get(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					if i == 254 {
						leaf, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: leaf.ID, ExpectedRevision: leaf.Revision})
						if err != nil {
							t.Fatalf("valid 256-task ancestry rejected: %v", err)
						}
					}
				}
			} else {
				workspace := "ws"
				if scenario == "foreign" {
					workspace = "foreign"
				}
				parent, err := s.Create(ctx, task.CreateCommand{WorkspaceID: workspace, Objective: "Ancestry parent"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.SQL().Exec(`UPDATE tasks SET parent_task_id=? WHERE id=?`, parent.ID, root.ID); err != nil {
					t.Fatal(err)
				}
				if scenario == "cycle" {
					if _, err = db.SQL().Exec(`UPDATE tasks SET parent_task_id=? WHERE id=?`, root.ID, parent.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err = s.MarkReady(ctx, task.TransitionCommand{TaskID: leaf.ID, ExpectedRevision: leaf.Revision}); err == nil {
				t.Fatalf("unsafe ancestry admitted: %v", err)
			}
		})
	}
}

func TestTeamHoldCannotReadmitThroughUnguardedTaskService(t *testing.T) {
	db, s := cancellationFixture(t)
	ctx := context.Background()
	for _, query := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('recovery-human','human','Owner','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','recovery-human','active',1,1)`,
	} {
		if _, err := db.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	row, err := s.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Retain the persisted Team hold"})
	if err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(db.SQL(), db, clock.Real{}, s)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "ws", Name: "Recovery team", CreatedBy: "recovery-human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Build", CreatedBy: "recovery-human"})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: row.ID, TeamID: group.ID, CreatedBy: "recovery-human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: member.ID, Objective: "Build carefully"}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "recovery-human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: ss.ID, PlanID: plan.ID, AcceptedByPrincipalID: "recovery-human"}); err != nil {
		t.Fatal(err)
	}
	row, err = s.Get(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: ss.ID, PlanID: plan.ID, ExpectedTaskRevision: row.Revision, ActorPrincipalID: "recovery-human"}); err != nil {
		t.Fatal(err)
	}
	row, err = s.Get(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A second caller cannot bypass persisted holds merely because it lacks
	// the optional high-level Team admission guard.
	independent := task.NewService(db.SQL(), db, clock.Real{})
	if _, err = independent.MarkReady(ctx, task.TransitionCommand{TaskID: row.ID, ExpectedRevision: row.Revision}); err == nil {
		t.Error("MarkReady bypassed active Team hold")
	}
	current, err := s.Get(ctx, row.ID)
	if err != nil || current.State != task.StatePaused || current.Revision != row.Revision {
		t.Fatalf("held Task changed: %+v %v", current, err)
	}
}
