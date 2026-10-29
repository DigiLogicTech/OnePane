//go:build integration

package agentworker

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestCancellationBetweenWorkerTicksParksOriginalAttemptOnce(t *testing.T) {
	w, ctx, run, cleanup := prepareUnknownDispatch(t)
	defer cleanup()
	current, err := w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}); err != nil {
		t.Fatal(err)
	}
	var beforeJournal int
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&beforeJournal); err != nil {
		t.Fatal(err)
	}
	results, err := w.Tick(ctx, 1)
	if err != nil || len(results) != 1 || results[0].Status != "cancellation_requested" || results[0].Error != "" {
		t.Fatalf("between-tick cancellation did not park: results=%+v err=%v", results, err)
	}
	parked, err := w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var continuation struct {
		CancellationRequested bool `json:"cancellation_requested"`
	}
	if err = json.Unmarshal(parked.Continuation, &continuation); err != nil {
		t.Fatal(err)
	}
	if parked.Status != RunWaiting || parked.AttemptID != run.AttemptID || parked.StepCount != run.StepCount || !continuation.CancellationRequested {
		t.Fatalf("park consumed reasoning or lost original owner: before=%+v after=%+v", run, parked)
	}
	var taskState, attemptState string
	var attemptCount, afterJournal, inferenceCount, runtimeCount int
	if err = w.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=?`, run.TaskID).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id=? AND task_id=?`, run.AttemptID, run.TaskID).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, run.TaskID).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&afterJournal); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_requests WHERE task_id=?`, run.TaskID).Scan(&inferenceCount); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runtime_invocations WHERE task_id=?`, run.TaskID).Scan(&runtimeCount); err != nil {
		t.Fatal(err)
	}
	if taskState != "cancel_requested" || attemptState != "waiting" || attemptCount != 1 || afterJournal != beforeJournal+1 || inferenceCount != 0 || runtimeCount != 0 || w.dispatchReconciliationFailed {
		t.Fatalf("unsafe cancellation boundary: task=%s attempt=%s attempts=%d journal=%d→%d inference=%d runtime=%d alarm=%v", taskState, attemptState, attemptCount, beforeJournal, afterJournal, inferenceCount, runtimeCount, w.dispatchReconciliationFailed)
	}
	// The fixture deliberately has no model service. Reaching inference would
	// fail instead of concealing a dispatch behind a successful mock response.
	results, err = w.Tick(ctx, 1)
	if err != nil || len(results) != 0 || w.dispatchReconciliationFailed {
		t.Fatalf("pending cancellation repeated or halted scheduling: %+v %v", results, err)
	}
	again, err := w.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var finalJournal int
	if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&finalJournal); err != nil {
		t.Fatal(err)
	}
	if finalJournal != afterJournal || again.Revision != parked.Revision || again.StepCount != parked.StepCount || again.Status != RunWaiting {
		t.Fatalf("park was not idempotent: before=%+v after=%+v journal=%d→%d", parked, again, afterJournal, finalJournal)
	}
}

func TestCancellationBetweenWorkerTicksRejectsForeignOrStaleAttempt(t *testing.T) {
	for _, tc := range []struct{ name, mutation, expectedState, expectedPrincipal string }{
		{"foreign Attempt principal", `UPDATE task_attempts SET worker_principal_id='system:agent-authority' WHERE id=?`, "running", AuthorityPrincipal},
		{"stale original Attempt", `UPDATE task_attempts SET status='failed' WHERE id=?`, "failed", WorkerPrincipal},
		{"foreign Worker principal", `UPDATE agent_worker_runs SET worker_principal_id='system:agent-authority' WHERE attempt_id=?`, "running", WorkerPrincipal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, ctx, run, cleanup := prepareUnknownDispatch(t)
			defer cleanup()
			current, err := w.tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}); err != nil {
				t.Fatal(err)
			}
			if _, err = w.db.ExecContext(ctx, tc.mutation, run.AttemptID); err != nil {
				t.Fatal(err)
			}
			var beforeJournal int
			if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&beforeJournal); err != nil {
				t.Fatal(err)
			}
			results, err := w.Tick(ctx, 1)
			if !errors.Is(err, ErrDispatchRequiresReconciliation) || len(results) != 1 || results[0].Status != "failed" || !w.dispatchReconciliationFailed {
				t.Fatalf("invalid owner did not fail closed: %+v err=%v alarm=%v", results, err, w.dispatchReconciliationFailed)
			}
			var attemptState, attemptPrincipal, taskState string
			var journal, dispatches int
			if err = w.db.QueryRowContext(ctx, `SELECT status,worker_principal_id FROM task_attempts WHERE id=?`, run.AttemptID).Scan(&attemptState, &attemptPrincipal); err != nil {
				t.Fatal(err)
			}
			if err = w.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=?`, run.TaskID).Scan(&taskState); err != nil {
				t.Fatal(err)
			}
			if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&journal); err != nil {
				t.Fatal(err)
			}
			if err = w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM inference_requests WHERE task_id=?)+(SELECT COUNT(*) FROM agent_runtime_invocations WHERE task_id=?)`, run.TaskID, run.TaskID).Scan(&dispatches); err != nil {
				t.Fatal(err)
			}
			unchanged, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if attemptState != tc.expectedState || attemptPrincipal != tc.expectedPrincipal || taskState != "cancel_requested" || unchanged.Status != RunRunning || unchanged.Revision != run.Revision || unchanged.StepCount != run.StepCount || journal != beforeJournal || dispatches != 0 {
				t.Fatalf("foreign/stale owner was mutated or dispatched: attempt=%s principal=%s task=%s run=%+v journal=%d→%d dispatches=%d", attemptState, attemptPrincipal, taskState, unchanged, beforeJournal, journal, dispatches)
			}
		})
	}
}
