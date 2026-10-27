//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/assurance"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func prepareAssuranceRetryJournal(t *testing.T) (*Service, context.Context, Run, string, func()) {
	return prepareAssuranceJournal(t, verification.StatusFail)
}

func prepareAssuranceJournal(t *testing.T, outcome verification.Status) (*Service, context.Context, Run, string, func()) {
	t.Helper()
	w, ctx, r, cleanup := prepareUnknownDispatch(t)
	cur, err := w.tasks.Get(ctx, r.TaskID)
	if err == nil {
		cur, err = w.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: r.TaskID, ExpectedRevision: cur.Revision})
	}
	if err == nil {
		_, err = w.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: r.TaskID, ExpectedRevision: cur.Revision})
	}
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	vs := verification.NewService(w.db, w.tx, clock.Real{})
	if err := assurance.New(w.db, w.tx, clock.Real{}, vs, w.tasks, nil, nil, nil).EnsureSystemPrincipal(ctx); err != nil {
		cleanup()
		t.Fatal(err)
	}
	spec, _ := json.Marshal(map[string]any{"assurance_version": 1, "worker_run_id": r.ID})
	v, err := vs.Create(ctx, verification.CreateCommand{WorkspaceID: r.WorkspaceID, TaskID: &r.TaskID, SubjectRef: "agent_worker_run:" + r.ID, RequiredLevel: policy.VerificationV2, Spec: spec})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	out := w.parkWorkerForIndependentAssurance(ctx, r, TickResult{TaskID: r.TaskID, RunID: r.ID}, json.RawMessage(`{}`), v.ID, "V2")
	if out.Status != "blocked_verification" {
		cleanup()
		t.Fatalf("park: %+v", out)
	}
	level := policy.VerificationV2
	_, err = vs.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: outcome, AchievedLevel: &level, Result: json.RawMessage(`{"reason":"stored evidence"}`), VerifiedBy: "system:assurance-verifier"})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	return w, ctx, r, v.ID, cleanup
}

type assuranceJournalBoundary struct {
	storage.Transactor
	before func()
}

func (b *assuranceJournalBoundary) Within(ctx context.Context, f func(context.Context, storage.Tx) error) error {
	if b.before != nil {
		run := b.before
		b.before = nil
		run()
	}
	return b.Transactor.Within(ctx, f)
}

func TestAssuranceRetryJournalRejectsChangedOwners(t *testing.T) {
	for _, scenario := range []string{"worker_revision", "worker_task", "attempt_failed", "task_paused", "task_cancelled", "execution_hold", "verification_pass", "verification_missing", "verification_foreign_task", "verification_other_run", "verification_other_principal", "verification_current_ref", "recovery_owner"} {
		t.Run(scenario, func(t *testing.T) {
			w, ctx, r, verificationID, cleanup := prepareAssuranceRetryJournal(t)
			defer cleanup()
			boundary := &assuranceJournalBoundary{Transactor: w.tx}
			boundary.before = func() {
				var err error
				switch scenario {
				case "worker_revision":
					_, err = w.db.Exec(`UPDATE agent_worker_runs SET revision=revision+1 WHERE id=?`, r.ID)
				case "worker_task":
					other, e := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: r.WorkspaceID, Objective: "Other Task"})
					err = e
					if err == nil {
						_, err = w.db.Exec(`UPDATE agent_worker_runs SET task_id=? WHERE id=?`, other.ID, r.ID)
					}
				case "attempt_failed":
					_, err = w.db.Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, r.AttemptID)
				case "task_paused":
					_, err = w.db.Exec(`UPDATE tasks SET state='paused',revision=revision+1 WHERE id=?`, r.TaskID)
				case "task_cancelled":
					_, err = w.db.Exec(`UPDATE tasks SET state='cancel_requested',revision=revision+1 WHERE id=?`, r.TaskID)
				case "execution_hold":
					// A Team hold can arrive while VERIFYING without rewriting
					// the Task state. Persist its real ancestry-visible records.
					for _, statement := range []string{
						`INSERT INTO teams(id,workspace_id,name,status,created_by,created_at,updated_at) VALUES('held-team','tenant','Held','active','system:agent-worker',1,1)`,
						`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,config_json,created_by,created_at,updated_at) VALUES('held-session','tenant','held-team','` + r.TaskID + `','paused_for_deliberation','{}','system:agent-worker',1,1)`,
						`INSERT INTO team_plans(id,workspace_id,session_id,version,status,plan_json,created_at) VALUES('held-plan','tenant','held-session',1,'accepted','{}',1)`,
						`INSERT INTO team_execution_holds(id,session_id,plan_id,root_task_id,created_by,created_at) VALUES('held-control','held-session','held-plan','` + r.TaskID + `','system:agent-worker',1)`,
					} {
						if _, err = w.db.Exec(statement); err != nil {
							break
						}
					}
				case "verification_pass":
					_, err = w.db.Exec(`UPDATE verifications SET status='pass' WHERE id=?`, verificationID)
				case "verification_missing":
					_, err = w.db.Exec(`DELETE FROM verifications WHERE id=?`, verificationID)
				case "verification_foreign_task":
					other, e := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: r.WorkspaceID, Objective: "Other Task"})
					err = e
					if err == nil {
						_, err = w.db.Exec(`UPDATE verifications SET task_id=? WHERE id=?`, other.ID, verificationID)
					}
				case "verification_other_run":
					_, err = w.db.Exec(`UPDATE verifications SET spec_json=json_set(spec_json,'$.worker_run_id','other') WHERE id=?`, verificationID)
				case "verification_other_principal":
					_, err = w.db.Exec(`UPDATE verifications SET verified_by=? WHERE id=?`, WorkerPrincipal, verificationID)
				case "verification_current_ref":
					_, err = w.db.Exec(`UPDATE agent_worker_steps SET result_ref='newer-verification' WHERE run_id=? AND step_kind='verification'`, r.ID)
				case "recovery_owner":
					_, err = w.db.Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('historical-interrupted',?,2,'interrupted','{}')`, r.TaskID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			w.tx = boundary
			if err := w.AssuranceRetry(ctx, r.ID, verificationID, json.RawMessage(`{}`)); err == nil {
				t.Fatal("changed/foreign owner accepted")
			}
			var retries int
			if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.verification_retry'`, r.TaskID).Scan(&retries); err != nil || retries != 0 {
				t.Fatalf("rejected callback wrote retry: %d %v", retries, err)
			}
			var state string
			if err := w.db.QueryRow(`SELECT status FROM agent_worker_runs WHERE id=?`, r.ID).Scan(&state); err != nil || state != "blocked" {
				t.Fatalf("rejected callback changed Worker: %s %v", state, err)
			}
		})
	}
}

func prepareAssurancePassedJournal(t *testing.T) (*Service, context.Context, Run, string, string, func()) {
	t.Helper()
	w, ctx, r, verificationID, cleanup := prepareAssuranceJournal(t, verification.StatusPass)
	vs := verification.NewService(w.db, w.tx, clock.Real{})
	cp, err := vs.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: r.WorkspaceID, TaskID: r.TaskID, VerificationID: verificationID, State: json.RawMessage(`{}`)})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	cur, err := w.tasks.Get(ctx, r.TaskID)
	if err == nil {
		_, err = w.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: r.TaskID, ExpectedRevision: cur.Revision, CheckpointID: cp.ID, Result: json.RawMessage(`{}`)})
	}
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	return w, ctx, r, verificationID, cp.ID, cleanup
}

func TestAssurancePassedJournalAtomicAndIdempotent(t *testing.T) {
	w, ctx, r, verificationID, checkpointID, cleanup := prepareAssurancePassedJournal(t)
	defer cleanup()
	if err := w.AssurancePassed(ctx, r.ID, verificationID, checkpointID); err != nil {
		t.Fatal(err)
	}
	if err := w.AssurancePassed(ctx, r.ID, verificationID, checkpointID); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	var state string
	var n int
	if err := w.db.QueryRow(`SELECT status FROM agent_worker_runs WHERE id=?`, r.ID).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("Worker completion: %s %v", state, err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' AND status='succeeded' AND result_ref=?`, r.ID, checkpointID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("completion journal: %d %v", n, err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='agent_worker.step' AND json_extract(payload_json,'$.detail.checkpoint_id')=?`, r.ID, checkpointID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("completion audit: %d %v", n, err)
	}
}

func TestAssurancePassedJournalFailureDoesNotLoseCompletionAudit(t *testing.T) {
	w, ctx, r, verificationID, checkpointID, cleanup := prepareAssurancePassedJournal(t)
	defer cleanup()
	if _, err := w.db.Exec(`CREATE TRIGGER reject_passed_journal BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.detail.independent')=1 BEGIN SELECT RAISE(ABORT,'audit rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := w.AssurancePassed(ctx, r.ID, verificationID, checkpointID); err == nil {
		t.Fatal("audit failure hidden")
	}
	var state string
	var n int
	if err := w.db.QueryRow(`SELECT status FROM agent_worker_runs WHERE id=?`, r.ID).Scan(&state); err != nil || state != "blocked" {
		t.Fatalf("audit failure terminalized Worker: %s %v", state, err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND status='succeeded'`, r.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed audit kept journal: %d %v", n, err)
	}
	if _, err := w.db.Exec(`DROP TRIGGER reject_passed_journal`); err != nil {
		t.Fatal(err)
	}
	if err := w.AssurancePassed(ctx, r.ID, verificationID, checkpointID); err != nil {
		t.Fatalf("durable retry after failure: %v", err)
	}
}

func TestAssurancePassedJournalRejectsStaleBindings(t *testing.T) {
	for _, scenario := range []string{"worker_revision", "attempt_failed", "task_paused", "verification_failed", "verification_other_principal", "later_attempt", "checkpoint_stale", "checkpoint_other_completion", "parked_other_verification", "parked_not_waiting", "unrecorded_success"} {
		t.Run(scenario, func(t *testing.T) {
			w, ctx, r, verificationID, checkpointID, cleanup := prepareAssurancePassedJournal(t)
			defer cleanup()
			boundary := &assuranceJournalBoundary{Transactor: w.tx, before: func() {
				var err error
				switch scenario {
				case "worker_revision":
					_, err = w.db.Exec(`UPDATE agent_worker_runs SET revision=revision+1 WHERE id=?`, r.ID)
				case "attempt_failed":
					_, err = w.db.Exec(`UPDATE task_attempts SET status='failed' WHERE id=?`, r.AttemptID)
				case "task_paused":
					_, err = w.db.Exec(`UPDATE tasks SET state='paused' WHERE id=?`, r.TaskID)
				case "verification_failed":
					_, err = w.db.Exec(`UPDATE verifications SET status='fail' WHERE id=?`, verificationID)
				case "verification_other_principal":
					_, err = w.db.Exec(`UPDATE verifications SET verified_by=? WHERE id=?`, WorkerPrincipal, verificationID)
				case "later_attempt":
					_, err = w.db.Exec(`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('later-completed',?,2,'succeeded','{}')`, r.TaskID)
				case "checkpoint_stale":
					_, err = w.db.Exec(`UPDATE checkpoints SET status='stale' WHERE id=?`, checkpointID)
				case "checkpoint_other_completion":
					vs := verification.NewService(w.db, w.tx, clock.Real{})
					var cp verification.Checkpoint
					cp, err = vs.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: r.WorkspaceID, TaskID: r.TaskID, VerificationID: verificationID, State: json.RawMessage(`{}`)})
					if err == nil {
						checkpointID = cp.ID
					}
				case "parked_other_verification":
					_, err = w.db.Exec(`UPDATE agent_worker_steps SET result_ref='different-verification' WHERE run_id=? AND step_kind='verification'`, r.ID)
				case "parked_not_waiting":
					_, err = w.db.Exec(`UPDATE agent_worker_steps SET status='failed' WHERE run_id=? AND step_kind='verification'`, r.ID)
				case "unrecorded_success":
					_, err = w.db.Exec(`UPDATE agent_worker_runs SET status='succeeded' WHERE id=?`, r.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			w.tx = boundary
			// For the alternate checkpoint, mutate before argument evaluation.
			if scenario == "checkpoint_other_completion" {
				boundary.before()
				boundary.before = nil
			}
			if err := w.AssurancePassed(ctx, r.ID, verificationID, checkpointID); err == nil {
				t.Fatal("stale completion accepted")
			}
			var n int
			if err := w.db.QueryRow(`SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND status='succeeded'`, r.ID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("rejected callback wrote completion: %d %v", n, err)
			}
		})
	}
}

func TestAssuranceRetryJournalPreservesOriginalAttemptAndAudit(t *testing.T) {
	w, ctx, r, verificationID, cleanup := prepareAssuranceRetryJournal(t)
	defer cleanup()
	if err := w.AssuranceRetry(ctx, r.ID, verificationID, json.RawMessage(`{"reason":"missing evidence"}`)); err != nil {
		t.Fatalf("valid failed-verification continuation: %v", err)
	}
	var taskState, attemptState, workerState, status, action, ref string
	var attempts, retries, journals int
	if err := w.db.QueryRow(`SELECT t.state,a.status,r.status FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, r.ID).Scan(&taskState, &attemptState, &workerState); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT status,json_extract(detail_json,'$.action'),result_ref FROM agent_worker_steps WHERE run_id=? AND json_extract(detail_json,'$.action')='retry'`, r.ID).Scan(&status, &action, &ref); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, r.TaskID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.verification_retry'`, r.TaskID).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='agent_worker.step' AND json_extract(payload_json,'$.detail.action')='retry'`, r.ID).Scan(&journals); err != nil {
		t.Fatal(err)
	}
	if taskState != "running" || attemptState != "running" || workerState != "running" || status != "succeeded" || action != "retry" || ref != verificationID || attempts != 1 || retries != 1 || journals != 1 {
		t.Fatalf("inconsistent continuation: %s/%s/%s step=%s/%s ref=%s attempts=%d retries=%d journals=%d", taskState, attemptState, workerState, status, action, ref, attempts, retries, journals)
	}
	assertStoredWorkerReference(t, w, ctx, r.ID, "verification", "succeeded", "", "verifications", verificationID)
	var storedFeedback string
	if err := w.db.QueryRow(`SELECT json_extract(continuation_json,'$.verification_feedback.result.reason') FROM agent_worker_runs WHERE id=?`, r.ID).Scan(&storedFeedback); err != nil || storedFeedback != "stored evidence" {
		t.Fatalf("callback replaced persisted verification result: %q %v", storedFeedback, err)
	}
	if err := w.AssuranceRetry(ctx, r.ID, verificationID, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	var again int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.verification_retry'`, r.TaskID).Scan(&again); err != nil || again != 1 {
		t.Fatalf("redelivery duplicated transition: %d %v", again, err)
	}
}

func TestAssuranceRetryJournalFailureRollsBackAllOwners(t *testing.T) {
	for _, failure := range []string{"worker_audit", "task_audit", "worker_update"} {
		t.Run(failure, func(t *testing.T) {
			w, ctx, r, verificationID, cleanup := prepareAssuranceRetryJournal(t)
			defer cleanup()
			statement := `CREATE TRIGGER reject_retry_journal BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.detail.action')='retry' BEGIN SELECT RAISE(ABORT,'audit rejected'); END`
			if failure == "task_audit" {
				statement = `CREATE TRIGGER reject_retry_journal BEFORE INSERT ON events WHEN NEW.event_type='task.verification_retry' BEGIN SELECT RAISE(ABORT,'audit rejected'); END`
			} else if failure == "worker_update" {
				statement = `CREATE TRIGGER reject_retry_journal BEFORE UPDATE ON agent_worker_runs WHEN NEW.status='running' BEGIN SELECT RAISE(ABORT,'Worker update rejected'); END`
			}
			if _, err := w.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := w.AssuranceRetry(ctx, r.ID, verificationID, json.RawMessage(`{}`)); err == nil {
				t.Fatal("audit failure hidden")
			}
			var taskState, attemptState, workerState string
			if err := w.db.QueryRow(`SELECT t.state,a.status,r.status FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, r.ID).Scan(&taskState, &attemptState, &workerState); err != nil {
				t.Fatal(err)
			}
			if taskState != "verifying" || attemptState != "waiting" || workerState != "blocked" {
				t.Fatalf("failed audit resumed owners: %s/%s/%s", taskState, attemptState, workerState)
			}
			var n int
			if err := w.db.QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.verification_retry'`, r.TaskID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("failed transaction kept retry: %d %v", n, err)
			}
		})
	}
}
