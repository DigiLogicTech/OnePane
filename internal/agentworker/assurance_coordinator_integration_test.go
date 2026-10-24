//go:build integration

package agentworker

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/assurance"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

// Exercise the actual evaluator and bridge across the durable callback boundary.
// Synthetic JSON evidence establishes V1 only; no model or external tool runs.
func TestAssuranceCoordinatorRedeliversRejectedJournalAfterRestart(t *testing.T) {
	for _, scenario := range []struct {
		name                   string
		passing, afterCallback bool
	}{{"retry_journal", false, false}, {"pass_journal", true, false}, {"retry_finalization", false, true}, {"pass_finalization", true, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			passing := scenario.passing
			w, ctx, r, cleanup := prepareUnknownDispatch(t)
			defer cleanup()
			cur, err := w.tasks.Get(ctx, r.TaskID)
			if err == nil {
				cur, err = w.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: r.TaskID, ExpectedRevision: cur.Revision})
			}
			if err == nil {
				_, err = w.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: r.TaskID, ExpectedRevision: cur.Revision})
			}
			if err != nil {
				t.Fatal(err)
			}
			vs := verification.NewService(w.db, w.tx, clock.Real{})
			spec, err := json.Marshal(map[string]any{
				"assurance_version": 1, "source": "coordinator-integration", "worker_run_id": r.ID,
				"proposed_result": map[string]any{"ok": passing},
				"criteria":        map[string]any{"result_checks": []any{map[string]any{"pointer": "/ok", "operator": "equals", "value": true}}},
				"evidence":        map[string]any{},
			})
			if err != nil {
				t.Fatal(err)
			}
			v, err := vs.Create(ctx, verification.CreateCommand{WorkspaceID: r.WorkspaceID, TaskID: &r.TaskID, SubjectRef: "agent_worker_run:" + r.ID, RequiredLevel: policy.VerificationV1, Spec: spec})
			if err != nil {
				t.Fatal(err)
			}
			out := w.parkWorkerForIndependentAssurance(ctx, r, TickResult{TaskID: r.TaskID, RunID: r.ID}, json.RawMessage(`{}`), v.ID, "V1")
			if out.Status != "blocked_verification" {
				t.Fatalf("park: %+v", out)
			}
			trigger := `CREATE TRIGGER reject_assurance_callback BEFORE INSERT ON events
 WHEN NEW.event_type='agent_worker.step' AND
 (json_extract(NEW.payload_json,'$.detail.action')='retry' OR json_extract(NEW.payload_json,'$.detail.independent')=1)
 BEGIN SELECT RAISE(ABORT,'callback audit rejected'); END`
			if scenario.afterCallback {
				trigger = `CREATE TRIGGER reject_assurance_callback BEFORE UPDATE ON assurance_runs
 WHEN NEW.status IN ('passed','failed','inconclusive') BEGIN SELECT RAISE(ABORT,'finalization rejected'); END`
			}
			if _, err = w.db.ExecContext(ctx, trigger); err != nil {
				t.Fatal(err)
			}
			newCoordinator := func() *assurance.Service {
				s := assurance.New(w.db, w.tx, clock.Real{}, vs, w.tasks, nil, nil, nil)
				s.SetWorkerBridge(w)
				return s
			}
			first := newCoordinator()
			if _, err = first.Tick(ctx, 1); err == nil {
				t.Fatal("rejected callback audit was concealed")
			}
			var workerState, assuranceState, taskState, attemptState string
			if err = w.db.QueryRowContext(ctx, `SELECT r.status,ar.status,t.state,a.status FROM agent_worker_runs r
 JOIN assurance_runs ar ON ar.worker_run_id=r.id JOIN tasks t ON t.id=r.task_id JOIN task_attempts a ON a.id=r.attempt_id WHERE r.id=?`, r.ID).Scan(&workerState, &assuranceState, &taskState, &attemptState); err != nil {
				t.Fatal(err)
			}
			wantTask, wantAttempt, wantPendingWorker := "verifying", "waiting", "blocked"
			if passing {
				wantTask, wantAttempt = "complete", "succeeded"
			}
			if scenario.afterCallback {
				wantPendingWorker = "succeeded"
				if !passing {
					wantPendingWorker, wantTask, wantAttempt = "running", "running", "running"
				}
			}
			if workerState != wantPendingWorker || assuranceState != "running" || taskState != wantTask || attemptState != wantAttempt {
				t.Fatalf("failed callback state: %s/%s/%s/%s", workerState, assuranceState, taskState, attemptState)
			}
			if _, err = w.db.ExecContext(ctx, `DROP TRIGGER reject_assurance_callback`); err != nil {
				t.Fatal(err)
			}
			restarted := newCoordinator()
			if n, err := restarted.RecoverInterrupted(ctx); err != nil || n != 1 {
				t.Fatalf("recover pending callback: n=%d err=%v", n, err)
			}
			results, err := restarted.Tick(ctx, 1)
			wantStatus, wantWorker := assurance.RunFailed, RunRunning
			if passing {
				wantStatus, wantWorker = assurance.RunPassed, RunSucceeded
			}
			if err != nil || len(results) != 1 || results[0].Status != wantStatus {
				t.Fatalf("redelivery: %+v %v", results, err)
			}
			after, err := w.getRun(ctx, r.ID)
			if err != nil || after.Status != wantWorker || after.AttemptID != r.AttemptID {
				t.Fatalf("redelivery changed original ownership: %+v %v", after, err)
			}
			if results, err := restarted.Tick(ctx, 1); err != nil || len(results) != 0 {
				t.Fatalf("terminal assurance redelivered: %+v %v", results, err)
			}
			var attempts, journals, audits int
			if err := w.db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM task_attempts WHERE task_id=?),
 (SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' AND status='succeeded'),
 (SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='agent_worker.step' AND
 (json_extract(payload_json,'$.detail.action')='retry' OR json_extract(payload_json,'$.detail.independent')=1))`, r.TaskID, r.ID, r.ID).Scan(&attempts, &journals, &audits); err != nil {
				t.Fatal(err)
			}
			if attempts != 1 || journals != 1 || audits != 1 {
				t.Fatalf("duplicate or missing records: attempts=%d journals=%d audits=%d", attempts, journals, audits)
			}
		})
	}
}
