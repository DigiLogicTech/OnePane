//go:build integration

package agentworker

import (
	"reflect"
	"strings"
	"testing"
)

// A routing observation must commit before reserving funds or claiming a model
// dispatch. These use the actual scheduler, journals, budget and migrated SQLite;
// no transport is configured or invoked.
func TestRouteJournalFailurePreventsPreparation(t *testing.T) {
	for _, selected := range []bool{false, true} {
		for _, failure := range []string{"step", "audit"} {
			name := "unavailable/" + failure
			if selected {
				name = "selected/" + failure
			}
			t.Run(name, func(t *testing.T) {
				w, ctx, run, accountID := prepareBudgetJournal(t, 1)
				if !selected {
					if _, err := w.db.ExecContext(ctx, `UPDATE model_deployments SET status='disabled' WHERE id='budget-deployment'`); err != nil {
						t.Fatal(err)
					}
				}
				statement := `CREATE TRIGGER reject_route_journal BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='route' AND json_extract(NEW.detail_json,'$.action') IS NULL BEGIN SELECT RAISE(ABORT,'route record rejected'); END`
				if failure == "audit" {
					statement = `CREATE TRIGGER reject_route_journal BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.kind')='route' AND json_extract(NEW.payload_json,'$.detail.action') IS NULL BEGIN SELECT RAISE(ABORT,'route record rejected'); END`
				}
				if _, err := w.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				prepared := w.prepareStep(ctx, run)
				if prepared.dispatch != nil {
					prepared.abort()
				}
				if prepared.dispatch != nil || prepared.result.Status != "failed" || !strings.Contains(prepared.result.Error, "route record rejected") {
					t.Fatalf("routing persistence failure was concealed: dispatch=%v result=%+v", prepared.dispatch != nil, prepared.result)
				}
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				account, err := w.budgets.Account(ctx, accountID)
				if err != nil {
					t.Fatal(err)
				}
				var steps, audits, reservations, dispatches int
				if err := w.db.QueryRowContext(ctx, `SELECT
				 (SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=?),
				 (SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='agent_worker.step'),
				 (SELECT COUNT(*) FROM budget_reservations WHERE task_id=?),
				 (SELECT COUNT(*) FROM inference_requests WHERE task_id=?)+(SELECT COUNT(*) FROM agent_runtime_invocations WHERE task_id=?)`, run.ID, run.ID, run.TaskID, run.TaskID, run.TaskID).Scan(&steps, &audits, &reservations, &dispatches); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 || reservations != 0 || dispatches != 0 || account.ReservedAmount != 0 || account.CommittedAmount != 0 || w.dispatchReconciliationFailed {
					t.Fatalf("failed pre-dispatch route changed owner/accounting: before=%+v after=%+v account=%+v steps=%d audits=%d reservations=%d dispatches=%d halted=%v", before, after, account, steps, audits, reservations, dispatches, w.dispatchReconciliationFailed)
				}
			})
		}
	}
}
