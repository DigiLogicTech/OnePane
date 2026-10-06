//go:build integration

package routineworker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestSystemPrincipalsAndLostRoutineAttemptsAreDurable(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := int64(1_700_000_000_000)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	clk := clock.Real{}
	tasks := task.NewService(db.SQL(), db, clk)
	svc := New(db.SQL(), db, clk, tasks, nil, nil, nil, nil, nil)
	if err := svc.EnsureSystemPrincipals(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureWorkspaceAccess(ctx, "ws"); err != nil {
		t.Fatal(err)
	}
	var members int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='ws' AND principal_id IN (?,?,?) AND status='active'`, AuthorityPrincipal, WorkerPrincipal, VerifierPrincipal).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 3 {
		t.Fatalf("system members=%d", members)
	}

	taskRow, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "routine job", SchedulingClass: task.ClassBackgroundRoutine})
	if err != nil {
		t.Fatal(err)
	}
	taskRow, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: taskRow.ID, ExpectedRevision: taskRow.Revision})
	if err != nil {
		t.Fatal(err)
	}
	worker := WorkerPrincipal
	taskRow, attempt, err := tasks.Start(ctx, task.StartCommand{TaskID: taskRow.ID, ExpectedRevision: taskRow.Revision, WorkerPrincipalID: &worker})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.State != task.AttemptRunning || taskRow.State != task.StateRunning {
		t.Fatalf("pre-recovery task=%s attempt=%s", taskRow.State, attempt.State)
	}
	count, err := svc.RecoverLostAttempts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered=%d", count)
	}
	taskRow, err = tasks.Get(ctx, taskRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if taskRow.State != task.StateBlocked {
		t.Fatalf("task state=%s", taskRow.State)
	}
	var attemptState string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id=?`, attempt.ID).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if attemptState != string(task.AttemptInterrupted) {
		t.Fatalf("attempt state=%s", attemptState)
	}
}
