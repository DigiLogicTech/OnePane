//go:build integration

package agentworker

import (
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestBudgetReleaseFailureIsReportedAtUndispatchedWorkerBoundaries(t *testing.T) {
	for _, boundary := range []string{"budget_journal", "agent_after_selection", "dispatch_cas", "agent_before_dispatch", "prepared_abort", "known_preflight_failure", "cancelled_preflight_failure"} {
		for _, rejectRelease := range []bool{false, true} {
			label := "release_succeeds"
			if rejectRelease {
				label = "release_fails"
			}
			t.Run(boundary+"/"+label, func(t *testing.T) {
				w, ctx, run, accountID := prepareBudgetJournal(t, 3)
				if rejectRelease {
					if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_release_failure_audit BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'undispatched reservation release unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				}
				if strings.HasPrefix(boundary, "agent_") {
					for _, query := range []string{
						`INSERT INTO agent_profiles(id,workspace_id,name,instructions_md,source_kind,status,created_at,updated_at) VALUES('pinned','tenant','Pinned','Original instructions','custom','active',1,1)`,
						`UPDATE tasks SET completion_json=json_set(completion_json,'$.onepane_routing.agent_profile','pinned')`,
					} {
						if _, err := w.db.ExecContext(ctx, query); err != nil {
							t.Fatal(err)
						}
					}
				}
				switch boundary {
				case "budget_journal":
					if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_reserved_journal BEFORE INSERT ON agent_worker_steps WHEN json_extract(NEW.detail_json,'$.action')='budget_reservation' BEGIN SELECT RAISE(ABORT,'budget reservation journal unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				case "agent_after_selection":
					if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER change_selected_agent AFTER INSERT ON events WHEN NEW.event_type='budget.reserved' BEGIN UPDATE agent_profiles SET revision=revision+1 WHERE id='pinned'; END`); err != nil {
						t.Fatal(err)
					}
				case "dispatch_cas":
					if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_worker_dispatch BEFORE UPDATE OF step_count ON agent_worker_runs WHEN NEW.step_count=OLD.step_count+1 BEGIN SELECT RAISE(ABORT,'reasoning dispatch CAS unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				case "agent_before_dispatch":
					if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER change_dispatch_agent AFTER UPDATE OF step_count ON agent_worker_runs WHEN NEW.step_count=OLD.step_count+1 BEGIN UPDATE agent_profiles SET revision=revision+1 WHERE id='pinned'; END`); err != nil {
						t.Fatal(err)
					}
				case "known_preflight_failure", "cancelled_preflight_failure":
					// Exercise the actual inference service with an empty transport
					// registry. It records a terminal preflight failure; no transport
					// exists to perform inference or consume any provider quota.
					store, err := artifact.NewLocalStore(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
					w.inference = inference.NewService(w.db, w.tx, clock.Real{})
					if err = w.inference.ConfigureExecution(w.artifacts, inference.NewTransportRegistry(), nil, "node-local"); err != nil {
						t.Fatal(err)
					}
					if err = w.inference.ConfigureBudget(w.budgets); err != nil {
						t.Fatal(err)
					}
				}
				prepared := w.prepareStep(ctx, run)
				result := prepared.result
				if boundary == "prepared_abort" {
					if prepared.abort == nil || prepared.dispatch == nil {
						t.Fatalf("preparation failed: %+v", prepared.result)
					}
					result = prepared.abort()
				} else if strings.Contains(boundary, "preflight_failure") {
					if prepared.dispatch == nil {
						t.Fatalf("preparation failed: %+v", prepared.result)
					}
					if boundary == "cancelled_preflight_failure" {
						current, err := w.tasks.Get(ctx, run.TaskID)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = w.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}); err != nil {
							t.Fatal(err)
						}
					}
					result = prepared.dispatch()()
				} else if prepared.dispatch != nil {
					t.Fatal("revoked or rejected execution reached transport boundary")
				}
				var reservationID string
				if err := w.db.QueryRowContext(ctx, `SELECT id FROM budget_reservations WHERE budget_account_id=?`, accountID).Scan(&reservationID); err != nil {
					t.Fatal(err)
				}
				reservation, err := w.budgets.Reservation(ctx, reservationID)
				if err != nil {
					t.Fatal(err)
				}
				account, err := w.budgets.Account(ctx, accountID)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus, wantReserved := budget.ReservationReleased, int64(0)
				wantReleaseAudits := 1
				if rejectRelease {
					wantStatus, wantReserved, wantReleaseAudits = budget.ReservationReserved, 1, 0
				}
				if reservation.Status != wantStatus || reservation.Amount != 1 || account.ReservedAmount != wantReserved || account.CommittedAmount != 0 {
					t.Fatalf("failed release changed conservative accounting: reservation=%+v account=%+v", reservation, account)
				}
				var requests, releases int
				if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_requests`).Scan(&requests); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(boundary, "preflight_failure") {
					var status string
					if err = w.db.QueryRowContext(ctx, `SELECT status FROM inference_requests`).Scan(&status); err != nil || status != "failed" || requests != 1 {
						t.Fatalf("preflight did not fail before transport: status=%s count=%d err=%v", status, requests, err)
					}
				} else if requests != 0 {
					t.Fatalf("undispatched boundary created request: count=%d", requests)
				}
				if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_type='budget.released'`).Scan(&releases); err != nil || releases != wantReleaseAudits {
					t.Fatalf("failed release published audit: count=%d err=%v", releases, err)
				}
				if strings.Contains(result.Error, "undispatched reservation release unavailable") != rejectRelease {
					t.Errorf("concealed undispatched release failure: boundary=%s result=%+v reservation=%s reserved=%d committed=%d", boundary, result, reservation.Status, account.ReservedAmount, account.CommittedAmount)
				}
				if rejectRelease {
					if boundary == "known_preflight_failure" && result.Status != "blocked" {
						t.Errorf("cleanup failure allowed fallback: %+v", result)
					}
					if boundary == "cancelled_preflight_failure" && result.Status != "blocked" {
						t.Errorf("cleanup failed to persist cancellation recovery: %+v", result)
					}
					if boundary == "dispatch_cas" {
						after, e := w.getRun(ctx, run.ID)
						if e != nil || after.Revision != run.Revision || after.StepCount != 0 || after.Status != RunRunning || after.AttemptID != run.AttemptID {
							t.Fatalf("rejected ownership was mutated: %+v %v", after, e)
						}
					}
					_, tickErr := w.Tick(ctx, 1)
					if tickErr != nil {
						t.Fatalf("cleanup failure did not park locally: boundary=%s err=%v", boundary, tickErr)
					}
					var reservations, attempts int
					if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM budget_reservations WHERE budget_account_id=?`, accountID).Scan(&reservations); err != nil || reservations != 1 {
						t.Fatalf("cleanup failure duplicated reservation: %d %v", reservations, err)
					}
					if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, run.TaskID).Scan(&attempts); err != nil || attempts != 1 {
						t.Fatalf("cleanup failure duplicated Attempt: %d %v", attempts, err)
					}
					if boundary == "budget_journal" || boundary == "dispatch_cas" || boundary == "prepared_abort" {
						reconstructed := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, w.verification)
						n, e := reconstructed.RecoverLostRuns(ctx)
						if e != nil || n != 0 {
							t.Fatalf("live park was not already durable: count=%d err=%v", n, e)
						}
						original, e := reconstructed.getRun(ctx, run.ID)
						if e != nil || original.Status != RunInterrupted || original.AttemptID != run.AttemptID {
							t.Fatalf("startup changed original incarnation: %+v %v", original, e)
						}
						persisted, e := w.budgets.Reservation(ctx, reservationID)
						if e != nil || persisted.Status != budget.ReservationReserved {
							t.Fatalf("startup guessed refund: %+v %v", persisted, e)
						}
					}
					if strings.Contains(boundary, "preflight_failure") || strings.HasPrefix(boundary, "agent_") {
						reconstructed := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, w.verification)
						if n, e := reconstructed.RecoverLostRuns(ctx); e != nil || n != 0 {
							t.Fatalf("durable accounting fence required startup rewriting: %d %v", n, e)
						}
						original, e := reconstructed.getRun(ctx, run.ID)
						if e != nil || original.Status != RunInterrupted || original.AttemptID != run.AttemptID {
							t.Fatalf("accounting fence lost original incarnation: %+v %v", original, e)
						}
						current, e := w.tasks.Get(ctx, run.TaskID)
						if e != nil {
							t.Fatal(e)
						}
						if _, e = w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}); e == nil {
							t.Fatal("unresolved accounting recovery admitted another Attempt")
						}
						if _, e = reconstructed.Tick(ctx, 1); e != nil {
							t.Fatal(e)
						}
						var journalCount, requestCount int
						if e = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND json_extract(detail_json,'$.outcome')='budget_release_uncommitted'`, run.ID).Scan(&journalCount); e != nil || journalCount != 1 {
							t.Fatalf("missing exact accounting recovery journal: %d %v", journalCount, e)
						}
						if e = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_requests`).Scan(&requestCount); e != nil || requestCount != requests {
							t.Fatalf("restart replayed preflight: %d %v", requestCount, e)
						}
					}
				}
			})
		}
	}
}

func TestBudgetReleaseCleanupRecoveryBeforePauseOrCancelPark(t *testing.T) {
	for _, control := range []string{"pause", "cancel"} {
		for _, rejectRecovery := range []bool{false, true} {
			t.Run(control+map[bool]string{false: "/committed", true: "/rejected"}[rejectRecovery], func(t *testing.T) {
				w, ctx, run, accountID := prepareBudgetJournal(t, 3)
				store, err := artifact.NewLocalStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
				w.inference = inference.NewService(w.db, w.tx, clock.Real{})
				if err = w.inference.ConfigureExecution(w.artifacts, inference.NewTransportRegistry(), nil, "node-local"); err != nil {
					t.Fatal(err)
				}
				if err = w.inference.ConfigureBudget(w.budgets); err != nil {
					t.Fatal(err)
				}
				if _, err = w.db.ExecContext(ctx, `CREATE TRIGGER reject_cleanup_release BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'cleanup release unavailable'); END`); err != nil {
					t.Fatal(err)
				}
				prepared := w.prepareStep(ctx, run)
				if prepared.dispatch == nil {
					t.Fatalf("preparation failed: %+v", prepared.result)
				}
				finish := prepared.dispatch()
				current, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				cmd := task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}
				if control == "pause" {
					_, err = w.tasks.Pause(ctx, cmd)
				} else {
					_, err = w.tasks.RequestCancel(ctx, cmd)
				}
				if err != nil {
					t.Fatal(err)
				}
				if rejectRecovery {
					if _, err = w.db.ExecContext(ctx, `CREATE TRIGGER reject_cleanup_recovery BEFORE INSERT ON agent_worker_steps WHEN json_extract(NEW.detail_json,'$.outcome')='budget_release_uncommitted' BEGIN SELECT RAISE(ABORT,'cleanup recovery unavailable'); END`); err != nil {
						t.Fatal(err)
					}
				}
				result := finish()
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := RunInterrupted
				if rejectRecovery {
					want = RunRunning
				}
				if after.Status != want || after.AttemptID != run.AttemptID || w.dispatchReconciliationFailed != rejectRecovery {
					t.Fatalf("cleanup lost owner: %+v result=%+v halted=%v", after, result, w.dispatchReconciliationFailed)
				}
				if rejectRecovery {
					var journals int
					if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE json_extract(detail_json,'$.outcome')='budget_release_uncommitted'`).Scan(&journals); err != nil || journals != 0 {
						t.Fatalf("rejected journal persisted: %d %v", journals, err)
					}
					current, err = w.tasks.Get(ctx, run.TaskID)
					if err != nil {
						t.Fatal(err)
					}
					if (control == "pause" && current.State != task.StatePaused) || (control == "cancel" && current.State != task.StateCancelRequested) {
						t.Fatalf("recovery rollback changed Task: %+v", current)
					}
				}
				reconstructed := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, w.verification)
				wantRecovered := 0
				if rejectRecovery {
					wantRecovered = 1
				}
				if n, e := reconstructed.RecoverLostRuns(ctx); e != nil || n != wantRecovered {
					t.Fatalf("startup recovery: %d %v", n, e)
				}
				current, err = w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: run.TaskID, ExpectedRevision: current.Revision}); err == nil {
					t.Fatal("cleanup history allowed retry")
				}
				if _, err = reconstructed.Tick(ctx, 1); err != nil {
					t.Fatal(err)
				}
				account, err := w.budgets.Account(ctx, accountID)
				if err != nil || account.ReservedAmount != 1 || account.CommittedAmount != 0 {
					t.Fatalf("restart changed accounting: %+v %v", account, err)
				}
				var requests, attempts int
				if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_requests`).Scan(&requests); err != nil || requests != 1 {
					t.Fatalf("restart replay: %d %v", requests, err)
				}
				if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, run.TaskID).Scan(&attempts); err != nil || attempts != 1 {
					t.Fatalf("restart readmission: %d %v", attempts, err)
				}
			})
		}
	}
}

func TestBudgetReleaseDoesNotRefundCommittedReservationOnPreparedAbort(t *testing.T) {
	w, ctx, run, accountID := prepareBudgetJournal(t, 3)
	prepared := w.prepareStep(ctx, run)
	if prepared.dispatch == nil || prepared.abort == nil {
		t.Fatalf("preparation failed: %+v", prepared.result)
	}
	var reservationID string
	if err := w.db.QueryRowContext(ctx, `SELECT id FROM budget_reservations WHERE budget_account_id=?`, accountID).Scan(&reservationID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.budgets.Commit(ctx, budget.CloseCommand{ReservationID: reservationID}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.ExecContext(ctx, `CREATE TRIGGER reject_committed_refund BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'must not refund committed quota'); END`); err != nil {
		t.Fatal(err)
	}
	result := prepared.abort()
	if strings.Contains(result.Error, "must not refund") || w.dispatchReconciliationFailed {
		t.Fatalf("committed accounting triggered refund cleanup: %+v", result)
	}
	reservation, err := w.budgets.Reservation(ctx, reservationID)
	if err != nil {
		t.Fatal(err)
	}
	account, err := w.budgets.Account(ctx, accountID)
	if err != nil || reservation.Status != budget.ReservationCommitted || account.ReservedAmount != 0 || account.CommittedAmount != 1 {
		t.Fatalf("committed quota altered: reservation=%+v account=%+v err=%v", reservation, account, err)
	}
}
