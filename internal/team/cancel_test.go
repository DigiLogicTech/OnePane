package team

import (
	"context"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func TestTeamCancellationPristineSettlementRetainsRootForGraphOwner(t *testing.T) {
	ctx := context.Background()
	s, _, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	// A generic root-only request must remain propagatable through the
	// reviewed Team endpoint. Session reasoning is not tracked by Attempts.
	root, err = s.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if n, err := s.tasks.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
			t.Fatalf("root settled before propagation: %d %v", n, err)
		}
	}
	accepted, err := s.Session(ctx, ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RequestExecutionCancellation(ctx, CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	// Never-attempted members may finish cancellation; the Team session root
	// stays with its owner even after all child requests have settled.
	for i := 0; i < 4; i++ {
		if _, err := s.tasks.SettleUnstartedCancellations(ctx, 16); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.tasks.Get(ctx, root.ID)
	if err != nil || got.State != task.StateCancelRequested {
		t.Fatalf("Team root escaped owner: %+v %v", got, err)
	}
	for _, id := range ids {
		child, err := s.tasks.Get(ctx, id)
		if err != nil || child.State != task.StateCancelled {
			t.Fatalf("unstarted contribution not cancelled: %+v %v", child, err)
		}
	}
}

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
	var cancellationRevision int64
	if err = db.SQL().QueryRow(`SELECT json_extract(payload_json,'$.task_revision') FROM events WHERE event_type='team.execution_cancellation_requested' AND aggregate_id=?`, ss.ID).Scan(&cancellationRevision); err != nil || cancellationRevision != root.Revision+1 {
		t.Fatalf("cancellation audit revision=%d err=%v", cancellationRevision, err)
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

func TestTeamCancellationRejectsCrossProjectWorkspaceAndArtifactScope(t *testing.T) {
	for _, mode := range []string{"project", "named_workspace", "artifact_session"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, specs := assignmentFixture(t)
			ids := acceptAssignments(t, s, ss, specs)
			root, _ := s.tasks.Get(ctx, ss.TaskID)
			accepted, _ := s.Session(ctx, ss.ID)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Foreign project','active','{}','{}','human',1,1)`)
			switch mode {
			case "project":
				exec(`UPDATE tasks SET project_id='p' WHERE id=?`, ids["review"])
			case "named_workspace":
				// Same Project, different named Workspace is still outside the graph.
				exec(`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('w','p','Other',1,1)`)
				exec(`UPDATE tasks SET project_id='p' WHERE id=? OR parent_task_id=?`, ss.TaskID, ss.TaskID)
				exec(`UPDATE tasks SET project_workspace_id='w' WHERE id=?`, ids["review"])
			case "artifact_session":
				exec(`INSERT INTO artifact_sessions(id,workspace_id,title,format,status,created_by,created_at,updated_at) VALUES('a','tenant','Other result','text','active','human',1,1)`)
				exec(`UPDATE tasks SET artifact_session_id='a' WHERE id=?`, ids["review"])
			}
			if _, err := s.RequestExecutionCancellation(ctx, CancelExecutionCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != ErrInvalid {
				t.Fatalf("cross-scope cancellation accepted: %v", err)
			}
			for _, id := range []string{root.ID, ids["build"], ids["review"]} {
				got, err := s.tasks.Get(ctx, id)
				if err != nil || got.CancelRequestedAt != nil || got.State == task.StateCancelRequested {
					t.Fatalf("rejected graph changed Task: %+v %v", got, err)
				}
			}
			var audits int
			if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type IN ('task.cancel_requested','team.execution_cancellation_requested')`).Scan(&audits); err != nil || audits != 0 {
				t.Fatalf("rejected graph emitted cancellation: %d %v", audits, err)
			}
		})
	}
}
