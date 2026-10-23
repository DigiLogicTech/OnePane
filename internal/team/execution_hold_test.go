package team

import (
	"context"
	"fmt"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func TestTeamPauseResumesOriginalAttemptsAndRetainsPlanAndCreatedContributions(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	build, err := s.tasks.Get(ctx, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	build, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: build.ID, ExpectedRevision: build.Revision})
	if err != nil {
		t.Fatal(err)
	}
	human := "human"
	_, attempt, err := s.tasks.Start(ctx, task.StartCommand{TaskID: build.ID, ExpectedRevision: build.Revision, WorkerPrincipalID: &human})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	c := ExecutionControlCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}
	if _, err = s.PauseExecution(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err = teamgate.Require(ctx, db.SQL(), ss.WorkspaceID, build.ID); err != teamgate.ErrPaused {
		t.Fatalf("hold did not fence Worker: %v", err)
	}
	review, _ := s.tasks.Get(ctx, ids["review"])
	if _, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: review.ID, ExpectedRevision: review.Revision}); err != teamgate.ErrPaused {
		t.Fatalf("created contribution admitted under hold: %v", err)
	}
	room, err := s.LiveRoom(ctx, ss.ID)
	if err != nil || !room.ExecutionPaused || room.TaskState != "paused" {
		t.Fatalf("hold projection: %+v %v", room, err)
	}
	root, _ = s.tasks.Get(ctx, ss.TaskID)
	c.ExpectedTaskRevision = root.Revision
	if _, err = s.ResumeExecution(ctx, c); err != nil {
		t.Fatal(err)
	}
	var id, state string
	var count int
	if err = db.SQL().QueryRow(`SELECT id,status FROM task_attempts WHERE task_id=?`, build.ID).Scan(&id, &state); err != nil || id != attempt.ID || state != "running" {
		t.Fatalf("Attempt replaced: %s %s %v", id, state, err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, build.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("pause created duplicate Attempts")
	}
	current, _ := s.Session(ctx, ss.ID)
	if current.AcceptedPlanID == nil || *current.AcceptedPlanID != c.PlanID {
		t.Fatal("pause rewrote accepted plan")
	}
	review, _ = s.tasks.Get(ctx, ids["review"])
	if review.State != task.StateCreated {
		t.Fatal("resume implicitly ran unstarted contribution")
	}
}

func TestTeamPauseFencesDeepDelegationAndResumeRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	parent := ids["build"]
	for i := 0; i < 40; i++ {
		child, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, ParentTaskID: &parent, Objective: fmt.Sprintf("Delegated %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		parent = child.ID
	}
	child, err := s.tasks.Get(ctx, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision}); err != nil {
		t.Fatal(err)
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	c := ExecutionControlCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}
	if _, err = s.PauseExecution(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err = teamgate.Require(ctx, db.SQL(), ss.WorkspaceID, parent); err != teamgate.ErrPaused {
		t.Fatalf("deep descendant bypassed hold: %v", err)
	}
	root, _ = s.tasks.Get(ctx, ss.TaskID)
	c.ExpectedTaskRevision = root.Revision
	if _, err = db.SQL().Exec(`CREATE TRIGGER fail_team_resume BEFORE UPDATE OF state ON tasks WHEN NEW.id='` + child.ID + `' AND NEW.state='ready' BEGIN SELECT RAISE(ABORT,'resume rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResumeExecution(ctx, c); err == nil {
		t.Fatal("failed resume committed")
	}
	still, _ := s.tasks.Get(ctx, root.ID)
	if still.State != task.StatePaused || still.Revision != root.Revision {
		t.Fatal("failed resume changed root")
	}
	if paused, err := teamgate.Paused(ctx, db.SQL(), ss.ID); err != nil || !paused {
		t.Fatalf("failed resume released hold: %v %v", paused, err)
	}
	if _, err = db.SQL().Exec(`DROP TRIGGER fail_team_resume`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResumeExecution(ctx, c); err != nil {
		t.Fatal(err)
	}
	if paused, err := teamgate.TaskPaused(ctx, db.SQL(), ss.WorkspaceID, parent); err != nil || paused {
		t.Fatalf("released hold still fences descendant: %v %v", paused, err)
	}
}

func TestTeamResumeRejectsRevokedHumanAndPauseRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	child, _ := s.tasks.Get(ctx, ids["build"])
	if _, err := s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision}); err != nil {
		t.Fatal(err)
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	c := ExecutionControlCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}
	if _, err := db.SQL().Exec(`CREATE TRIGGER fail_team_pause BEFORE UPDATE OF state ON tasks WHEN NEW.id='` + child.ID + `' AND NEW.state='paused' BEGIN SELECT RAISE(ABORT,'pause rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PauseExecution(ctx, c); err == nil {
		t.Fatal("failed atomic pause succeeded")
	}
	paused, err := teamgate.Paused(ctx, db.SQL(), ss.ID)
	if err != nil || paused {
		t.Fatalf("failed pause retained hold: %v %v", paused, err)
	}
	if _, err = db.SQL().Exec(`DROP TRIGGER fail_team_pause`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PauseExecution(ctx, c); err != nil {
		t.Fatal(err)
	}
	root, _ = s.tasks.Get(ctx, ss.TaskID)
	c.ExpectedTaskRevision = root.Revision
	if _, err = db.SQL().Exec(`UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResumeExecution(ctx, c); err != ErrHumanRequired {
		t.Fatalf("revoked human resumed Team: %v", err)
	}
	paused, err = teamgate.Paused(ctx, db.SQL(), ss.ID)
	if err != nil || !paused {
		t.Fatal("failed resume released hold")
	}
}

func TestTeamPauseRejectsOversizedScopeAndForeignAncestry(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	if _, err := db.SQL().Exec(`WITH RECURSIVE chain(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM chain WHERE n<255)
	 INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,completion_json,created_at,updated_at)
	 SELECT 'deep-'||n,?,CASE WHEN n=1 THEN ? ELSE 'deep-'||(n-1) END,'Delegated work','created','{}',1,1 FROM chain`, ss.WorkspaceID, ids["build"]); err != nil {
		t.Fatal(err)
	}
	if _, err := teamgate.TaskPaused(ctx, db.SQL(), ss.WorkspaceID, "deep-255"); err != teamgate.ErrExecution {
		t.Fatalf("truncated ancestry authorized: %v", err)
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	accepted, _ := s.Session(ctx, ss.ID)
	c := ExecutionControlCommand{SessionID: ss.ID, PlanID: *accepted.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}
	if _, err := s.PauseExecution(ctx, c); err != ErrInvalid {
		t.Fatalf("oversized graph paused: %v", err)
	}
	if paused, err := teamgate.Paused(ctx, db.SQL(), ss.ID); err != nil || paused {
		t.Fatalf("oversized graph retained hold: %v %v", paused, err)
	}
	if _, err := db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`UPDATE tasks SET workspace_id='foreign' WHERE id='deep-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := teamgate.TaskPaused(ctx, db.SQL(), ss.WorkspaceID, "deep-2"); err != teamgate.ErrExecution {
		t.Fatalf("foreign ancestor authorized: %v", err)
	}
}
