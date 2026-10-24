//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func prepareBudgetJournal(t *testing.T, limit int64) (*Service, context.Context, Run, string) {
	t.Helper()
	w, ctx, run, cleanup := prepareUnknownDispatch(t)
	t.Cleanup(cleanup)
	for _, statement := range []string{
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('budget-provider','tenant','synthetic','Synthetic','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
		`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('budget-model','synthetic-budget','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('budget-deployment','budget-model','budget-provider','no-transport','{}','ready','budget-fp',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('budget-compatibility','budget-deployment','inference.general','L2',0,64000,'low','supported','{}','{}','budget-cp',1,1,1)`,
	} {
		if _, err := w.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	w.budgets = budget.NewService(w.db, w.tx, clock.Real{})
	w.scheduler = scheduler.NewService(w.db, w.tx, clock.Real{})
	w.verification = verification.NewService(w.db, w.tx, clock.Real{})
	workspace := run.WorkspaceID
	account, err := w.budgets.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &workspace, Name: "Reasoning", Unit: "requests", LimitAmount: limit})
	if err != nil {
		t.Fatal(err)
	}
	rp := defaultRoutePolicy()
	rp.AllowPotentialMonetarySpend = true
	rp.BudgetAccountID = account.ID
	rp.BudgetReserveAmount = 1
	raw, err := json.Marshal(rp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET route_policy_json=? WHERE id=?`, string(raw), run.ID); err != nil {
		t.Fatal(err)
	}
	return w, ctx, run, account.ID
}

func TestBudgetJournalRecordsActualReservationAndDenial(t *testing.T) {
	for _, limit := range []int64{0, 1} {
		t.Run(map[int64]string{0: "denied", 1: "reserved"}[limit], func(t *testing.T) {
			w, ctx, run, accountID := prepareBudgetJournal(t, limit)
			prepared := w.prepareStep(ctx, run)
			wantStatus, wantReservationStatus := "failed", "denied"
			if limit > 0 {
				wantStatus, wantReservationStatus = "succeeded", "reserved"
				if prepared.dispatch == nil {
					t.Fatalf("qualified budget failed preparation: %+v", prepared.result)
				}
			} else if prepared.dispatch != nil || prepared.result.Status != "blocked" {
				t.Fatalf("unfunded request admitted: %+v", prepared.result)
			}
			var kind, status, reservationStatus, savedAccount string
			var count int
			if err := w.db.QueryRowContext(ctx, `SELECT step_kind,status,json_extract(detail_json,'$.reservation_status'),json_extract(detail_json,'$.budget_account_id') FROM agent_worker_steps WHERE run_id=? AND json_extract(detail_json,'$.action')='budget_reservation'`, run.ID).Scan(&kind, &status, &reservationStatus, &savedAccount); err != nil {
				t.Fatalf("budget observation missing from real journal: %v", err)
			}
			if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_type='agent_worker.step' AND aggregate_id=? AND json_extract(payload_json,'$.detail.action')='budget_reservation'`, run.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if kind != "route" || status != wantStatus || reservationStatus != wantReservationStatus || savedAccount != accountID || count != 1 {
				t.Fatalf("incorrect budget observation: kind=%s status=%s reservation=%s account=%s audit=%d", kind, status, reservationStatus, savedAccount, count)
			}
			if limit > 0 {
				var reservationID string
				if err := w.db.QueryRowContext(ctx, `SELECT result_ref FROM agent_worker_steps WHERE run_id=? AND json_extract(detail_json,'$.action')='budget_reservation'`, run.ID).Scan(&reservationID); err != nil {
					t.Fatal(err)
				}
				reservation, err := w.budgets.Reservation(ctx, reservationID)
				if err != nil || reservation.Status != budget.ReservationReserved || reservation.TaskID == nil || *reservation.TaskID != run.TaskID {
					t.Fatalf("journal did not bind actual Task reservation: %+v %v", reservation, err)
				}
				// No transport is configured or invoked. Release the proven
				// undispatched prepared work through its existing abort owner.
				prepared.abort()
			}
		})
	}
}

func TestBudgetJournalFailurePreventsDispatchAndAccountsReservation(t *testing.T) {
	for _, scenario := range []string{"insert", "audit", "release", "denial_audit"} {
		t.Run(scenario, func(t *testing.T) {
			limit := int64(1)
			if scenario == "denial_audit" {
				limit = 0
			}
			w, ctx, run, accountID := prepareBudgetJournal(t, limit)
			statement := `CREATE TRIGGER reject_budget_observation BEFORE INSERT ON agent_worker_steps WHEN json_extract(NEW.detail_json,'$.action')='budget_reservation' BEGIN SELECT RAISE(ABORT,'budget journal rejected'); END`
			if scenario == "audit" || scenario == "denial_audit" {
				statement = `CREATE TRIGGER reject_budget_observation BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.detail.action')='budget_reservation' BEGIN SELECT RAISE(ABORT,'budget audit rejected'); END`
			}
			if _, err := w.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			if scenario == "release" {
				if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_budget_release BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'budget release rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			prepared := w.prepareStep(ctx, run)
			if prepared.dispatch != nil || prepared.result.Status != "failed" || !strings.Contains(prepared.result.Error, "record reasoning budget") {
				t.Fatalf("unrecorded budget admitted or concealed persistence failure: %+v", prepared.result)
			}
			if scenario == "release" && !strings.Contains(prepared.result.Error, "budget release rejected") {
				t.Fatalf("release failure concealed: %+v", prepared.result)
			}
			after, err := w.getRun(ctx, run.ID)
			if err != nil || after.StepCount != 0 || after.Revision != run.Revision || after.Status != RunRunning || after.AttemptID != run.AttemptID {
				t.Fatalf("pre-dispatch failure changed execution owner: %+v %v", after, err)
			}
			var observed, audited, dispatched int
			if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND json_extract(detail_json,'$.action')='budget_reservation'`, run.ID).Scan(&observed); err != nil {
				t.Fatal(err)
			}
			if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_type='agent_worker.step' AND aggregate_id=? AND json_extract(payload_json,'$.detail.action')='budget_reservation'`, run.ID).Scan(&audited); err != nil {
				t.Fatal(err)
			}
			if err := w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM inference_requests WHERE task_id=?)+(SELECT COUNT(*) FROM agent_runtime_invocations WHERE task_id=?)`, run.TaskID, run.TaskID).Scan(&dispatched); err != nil {
				t.Fatal(err)
			}
			account, err := w.budgets.Account(ctx, accountID)
			wantReserved := int64(0)
			if scenario == "release" {
				wantReserved = 1
			}
			if err != nil || observed != 0 || audited != 0 || dispatched != 0 || account.ReservedAmount != wantReserved || account.CommittedAmount != 0 {
				t.Fatalf("failed journal lost accounting/rollback: observations=%d audits=%d dispatched=%d account=%+v err=%v", observed, audited, dispatched, account, err)
			}
		})
	}
}
