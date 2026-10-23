package team

import (
	"context"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func TestTeamCancellationIsAtomicScopedAndPreservesCompletedContributions(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	cp := completeAssignment(t, s, db, ids["build"])
	parentID := ids["review"]
	descendant, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, ParentTaskID: &parentID, Objective: "Delegated review"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, Objective: "Other work"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.Session(ctx, ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human", Reason: "Operator cancellation"}
	if _, err = s.RequestExecutionCancellation(ctx, command); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root.ID, parentID, descendant.ID} {
		got, err := s.tasks.Get(ctx, id)
		if err != nil || got.State != task.StateCancelRequested || got.CancelRequestedAt == nil {
			t.Fatalf("missing scoped cancellation: %+v %v", got, err)
		}
		if _, err = teamgate.Require(ctx, db.SQL(), ss.WorkspaceID, id); id == parentID && err == nil {
			t.Fatal("cancelled Team still authorized contribution")
		}
	}
	finished, _ := s.tasks.Get(ctx, ids["build"])
	if finished.State != task.StateComplete {
		t.Fatalf("completed result changed: %+v", finished)
	}
	var retained int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM checkpoints WHERE id=? AND task_id=?`, cp, finished.ID).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("completed checkpoint lost: %d %v", retained, err)
	}
	other, _ := s.tasks.Get(ctx, unrelated.ID)
	if other.State != task.StateCreated || other.Revision != unrelated.Revision {
		t.Fatal("unrelated Task changed")
	}
	if _, err = s.RequestExecutionCancellation(ctx, command); err != task.ErrRevisionConflict {
		t.Fatalf("stale root revision accepted: %v", err)
	}
	current, _ := s.tasks.Get(ctx, root.ID)
	command.ExpectedTaskRevision = current.Revision
	if _, err = s.RequestExecutionCancellation(ctx, command); err != nil {
		t.Fatalf("fresh duplicate intent was not idempotent: %v", err)
	}
	again, _ := s.tasks.Get(ctx, parentID)
	if again.Revision != 2 {
		t.Fatalf("duplicate cancellation mutated contribution: %+v", again)
	}
}

func TestTeamCancellationRollsBackAllTasksAndRequiresCurrentHumanAuthority(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	command := CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}
	if _, err := db.SQL().Exec(`CREATE TRIGGER fail_team_cancel BEFORE UPDATE OF state ON tasks WHEN NEW.id='` + ids["review"] + `' AND NEW.state='cancel_requested' BEGIN SELECT RAISE(ABORT,'fixture rollback'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestExecutionCancellation(ctx, command); err == nil {
		t.Fatal("failed graph transaction succeeded")
	}
	for _, id := range []string{root.ID, ids["build"], ids["review"]} {
		got, _ := s.tasks.Get(ctx, id)
		expectedState, expectedRevision := task.StateCreated, int64(1)
		if id == root.ID {
			expectedState, expectedRevision = root.State, root.Revision
		}
		if got.State != expectedState || got.Revision != expectedRevision {
			t.Fatalf("partially committed cancellation: %+v", got)
		}
	}
	if _, err := db.SQL().Exec(`DROP TRIGGER fail_team_cancel`); err != nil {
		t.Fatal(err)
	}
	wrong := command
	wrong.PlanID = "other-plan"
	if _, err := s.RequestExecutionCancellation(ctx, wrong); err == nil {
		t.Fatal("wrong accepted plan cancelled work")
	}
	if _, err := db.SQL().Exec(`UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestExecutionCancellation(ctx, command); err != ErrHumanRequired {
		t.Fatalf("revoked human cancelled work: %v", err)
	}
}

func TestTeamCancellationRejectsForeignAndOversizedDescendantScopes(t *testing.T) {
	for _, mode := range []string{"foreign", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, specs := assignmentFixture(t)
			acceptAssignments(t, s, ss, specs)
			root, _ := s.tasks.Get(ctx, ss.TaskID)
			accepted, _ := s.Session(ctx, ss.ID)
			if mode == "foreign" {
				if _, err := db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`); err != nil {
					t.Fatal(err)
				}
				if _, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "foreign", ParentTaskID: &ss.TaskID, Objective: "Foreign descendant"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.SQL().Exec(`WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<255)
			INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,created_at,updated_at)
			SELECT 'overflow-'||n,'tenant',?,'Bounded scope fixture','created','{}',1,1 FROM numbers`, ss.TaskID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.RequestExecutionCancellation(ctx, CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != ErrInvalid {
				t.Fatalf("unsafe %s scope accepted: %v", mode, err)
			}
			current, _ := s.tasks.Get(ctx, root.ID)
			if current.Revision != root.Revision || current.State != root.State {
				t.Fatal("scope rejection changed root")
			}
		})
	}
}

func TestTeamCancellationPropagatesAnEarlierRootOnlyRequest(t *testing.T) {
	ctx := context.Background()
	s, _, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	root, err := s.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RequestExecutionCancellation(ctx, CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		child, err := s.tasks.Get(ctx, id)
		if err != nil || child.State != task.StateCancelRequested {
			t.Fatalf("root-only request was not propagated: %+v %v", child, err)
		}
	}
}
