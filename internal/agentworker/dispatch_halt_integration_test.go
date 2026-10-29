//go:build integration

package agentworker

import (
	"context"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestKillAndRestartParksInFlightTaskWithoutReplay(t *testing.T) {
	worker, ctx, run, cleanup := prepareUnknownDispatch(t)
	defer cleanup()
	var seq int
	var name, file string
	if err := worker.db.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	if err := worker.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := New(reopened.SQL(), reopened, worker.clock, worker.localNodeID, task.NewService(reopened.SQL(), reopened, worker.clock), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	recovered, err := restarted.RecoverLostRuns(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("restart recovery count=%d err=%v", recovered, err)
	}
	var state, attempt, runState string
	var attempts int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=t.id) FROM tasks t JOIN task_attempts a ON a.id=? JOIN agent_worker_runs r ON r.id=? WHERE t.id=?`, run.AttemptID, run.ID, run.TaskID).Scan(&state, &attempt, &runState, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "blocked" || attempt != "interrupted" || runState != "interrupted" || attempts != 1 {
		t.Fatalf("restart replayed or dropped the in-flight task: %s/%s/%s attempts=%d", state, attempt, runState, attempts)
	}
	again, err := restarted.RecoverLostRuns(ctx)
	if err != nil || again != 0 {
		t.Fatalf("second restart recovery changed the parked task: count=%d err=%v", again, err)
	}
}

func TestUncommittedWriteRecoversWithoutRestartOrReplay(t *testing.T) {
	worker, ctx, run, cleanup := prepareUnknownDispatch(t)
	defer cleanup()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	result := worker.interruptDispatchForReconciliation(cancelled, run, TickResult{TaskID: run.TaskID, RunID: run.ID}, "model", "model_deployment", "candidate-1", "inference-1", errors.New("transport uncertain"))
	if result.Status != "failed" || !worker.dispatchReconciliationFailed {
		t.Fatalf("failed park did not halt: %+v", result)
	}
	var beforeState, beforeRun string
	if err := worker.db.QueryRowContext(ctx, `SELECT t.state,r.status FROM tasks t JOIN agent_worker_runs r ON r.task_id=t.id WHERE r.id=?`, run.ID).Scan(&beforeState, &beforeRun); err != nil {
		t.Fatal(err)
	}
	if beforeState != "running" || beforeRun != "running" {
		t.Fatalf("failed commit mutated records: %s/%s", beforeState, beforeRun)
	}
	if _, err := worker.Tick(ctx, 2); err != nil {
		t.Fatalf("halt did not recover in-process: %v", err)
	}
	if worker.dispatchReconciliationFailed {
		t.Fatal("recovered halt stayed latched")
	}
	var state, attempt, runState string
	var attempts int
	if err := worker.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=t.id) FROM tasks t JOIN task_attempts a ON a.id=? JOIN agent_worker_runs r ON r.id=? WHERE t.id=?`, run.AttemptID, run.ID, run.TaskID).Scan(&state, &attempt, &runState, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "blocked" || attempt != "interrupted" || runState != "interrupted" || attempts != 1 {
		t.Fatalf("in-process recovery replayed the uncertain step: %s/%s/%s attempts=%d", state, attempt, runState, attempts)
	}
	status, reason := worker.ObservedHealth()
	if status != "ok" || reason != "" {
		t.Fatalf("health stayed halted after the park: %s %s", status, reason)
	}
	created, err := worker.tasks.Create(ctx, task.CreateCommand{WorkspaceID: run.WorkspaceID, Objective: "Independent work continues"})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := worker.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: created.ID, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	other, res := worker.startRun(ctx, ready)
	if res.Error != "" || other.ID == "" || other.ID == run.ID {
		t.Fatalf("independent task did not start after the halted write: %+v %+v", other, res)
	}
}
