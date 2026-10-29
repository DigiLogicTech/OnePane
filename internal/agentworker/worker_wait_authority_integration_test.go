//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

func applyWorkerWait(t *testing.T, w *Service, ctx context.Context, run Run, kind string) TickResult {
	t.Helper()
	current, err := w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	response := agentprotocol.Response{Proposal: json.RawMessage(`{"question":"Review the recorded contribution?","reason":"Wait for review"}`)}
	result := TickResult{TaskID: run.TaskID, RunID: run.ID}
	if kind == "human" {
		return w.handleHuman(ctx, run, current, response, result)
	}
	return w.handleWait(ctx, run, current, response, result)
}

func TestWorkerWaitStaleRedeliveryDoesNotHaltNewerOwner(t *testing.T) {
	for _, kind := range []string{"human", "wait"} {
		t.Run(kind, func(t *testing.T) {
			w, ctx, old := prepareProposalContinuation(t, kind)
			if result := applyWorkerWait(t, w, ctx, old, kind); result.Status == "failed" {
				t.Fatalf("healthy wait: %+v", result)
			}
			current, err := w.tasks.Get(ctx, old.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.tasks.Resume(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision}); err != nil {
				t.Fatal(err)
			}
			if err = w.syncResumedRuns(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := w.getRun(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before.Status != RunRunning || before.Revision <= old.Revision {
				t.Fatal("fixture did not resume exact original owner")
			}
			result := applyWorkerWait(t, w, ctx, old, kind)
			after, err := w.getRun(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			steps, audits := proposalContinuationJournalCounts(t, w, ctx, old.ID, kind)
			if result.Status != "failed" || !reflect.DeepEqual(before, after) || w.dispatchReconciliationFailed || steps != 1 || audits != 1 {
				t.Fatalf("stale returned wait changed/blocked resumed owner: result=%+v halted=%v steps=%d audits=%d", result, w.dispatchReconciliationFailed, steps, audits)
			}
			// Exact original binding still admits the next dispatch; no model or
			// scheduler is invoked and no new Attempt is created by this check.
			if err = w.markReasoningDispatch(ctx, &after, "model_deployment", "resumed-checked-candidate"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkerWaitRejectsChangedOriginalBinding(t *testing.T) {
	for _, kind := range []string{"human", "wait"} {
		for _, change := range []string{"worker_principal", "attempt_provenance", "task_archived", "stale_revision"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				w, ctx, run := prepareProposalContinuation(t, kind)
				var err error
				switch change {
				case "worker_principal":
					_, err = w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET worker_principal_id=? WHERE id=?`, AuthorityPrincipal, run.ID)
				case "attempt_provenance":
					_, err = w.db.ExecContext(ctx, `UPDATE task_attempts SET metadata_json='{}' WHERE id=?`, run.AttemptID)
				case "task_archived":
					_, err = w.db.ExecContext(ctx, `UPDATE tasks SET archived_at=1 WHERE id=?`, run.TaskID)
				case "stale_revision":
					run.Revision--
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				result := applyWorkerWait(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if result.Status != "failed" || !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 {
					t.Fatalf("changed %s accepted wait: result=%+v steps=%d audits=%d", change, result, steps, audits)
				}
			})
		}
	}
}

func TestWorkerWaitJournalFailurePreservesOriginalLineage(t *testing.T) {
	for _, kind := range []string{"human", "wait"} {
		for _, failure := range []string{"task", "attempt", "worker", "step", "audit"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				w, ctx, run := prepareProposalContinuation(t, kind)
				statement := `CREATE TRIGGER reject_wait BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'wait task rejected'); END`
				switch failure {
				case "attempt":
					statement = `CREATE TRIGGER reject_wait BEFORE UPDATE ON task_attempts BEGIN SELECT RAISE(ABORT,'wait attempt rejected'); END`
				case "worker":
					statement = `CREATE TRIGGER reject_wait BEFORE UPDATE ON agent_worker_runs BEGIN SELECT RAISE(ABORT,'wait worker rejected'); END`
				case "step":
					statement = fmt.Sprintf(`CREATE TRIGGER reject_wait BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='%s' BEGIN SELECT RAISE(ABORT,'wait journal rejected'); END`, kind)
				case "audit":
					statement = fmt.Sprintf(`CREATE TRIGGER reject_wait BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.kind')='%s' BEGIN SELECT RAISE(ABORT,'wait audit rejected'); END`, kind)
				}
				if _, err := w.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				beforeTask, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				var beforeEvents int
				if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&beforeEvents); err != nil {
					t.Fatal(err)
				}
				result := applyWorkerWait(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				afterTask, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				var attempt string
				var attempts, afterEvents, priorModels int
				if err = w.db.QueryRowContext(ctx, `SELECT status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=?),(SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='model' AND status='succeeded') FROM task_attempts WHERE id=?`, run.TaskID, run.ID, run.AttemptID).Scan(&attempt, &attempts, &afterEvents, &priorModels); err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if result.Status != "failed" || !w.dispatchReconciliationFailed || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeTask, afterTask) || attempt != "running" || attempts != 1 || beforeEvents != afterEvents || priorModels != 1 || steps != 0 || audits != 0 {
					t.Fatalf("failed %s partially applied wait: result=%+v attempt=%s attempts=%d events=%d->%d priorModels=%d steps=%d audits=%d", failure, result, attempt, attempts, beforeEvents, afterEvents, priorModels, steps, audits)
				}
			})
		}
	}
}

// A durable model response is not permission to park an owner after its
// tenancy, sealed accepted graph or ancestor hold changed. These tests call
// actual proposal handlers and storage; no transport/model/tool is executed.
func TestWorkerWaitRechecksAcceptedTeamAuthority(t *testing.T) {
	for _, kind := range []string{"human", "wait"} {
		for _, change := range []string{"none", "tenant_disabled", "root_criteria", "child_criteria", "ancestor_hold"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				w, ctx, run, teams, sessionID, planID := prepareTeamProposalContinuation(t, kind)
				var rootID string
				if err := w.db.QueryRowContext(ctx, `SELECT task_id FROM team_sessions WHERE id=?`, sessionID).Scan(&rootID); err != nil {
					t.Fatal(err)
				}
				var err error
				switch change {
				case "tenant_disabled":
					_, err = w.db.ExecContext(ctx, `UPDATE workspaces SET status='suspended' WHERE id=?`, run.WorkspaceID)
				case "root_criteria", "child_criteria":
					id := rootID
					if change == "child_criteria" {
						id = run.TaskID
					}
					_, err = w.db.ExecContext(ctx, `UPDATE tasks SET completion_json='{"changed_criteria":true}' WHERE id=?`, id)
				case "ancestor_hold":
					root, getErr := w.tasks.Get(ctx, rootID)
					if getErr != nil {
						t.Fatal(getErr)
					}
					_, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: sessionID, PlanID: planID, ActorPrincipalID: "proposal-human", ExpectedTaskRevision: root.Revision, Reason: "Hold before recorded human/wait proposal"})
					if err == nil {
						_, err = w.db.ExecContext(ctx, `UPDATE tasks SET state='running' WHERE id=?`, run.TaskID)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				beforeTask, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				var beforeAttempt string
				if err = w.db.QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id=?`, run.AttemptID).Scan(&beforeAttempt); err != nil {
					t.Fatal(err)
				}
				result := applyWorkerWait(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				afterTask, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				var afterAttempt string
				if err = w.db.QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id=?`, run.AttemptID).Scan(&afterAttempt); err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if change == "none" {
					want := task.StateWaitingApproval
					if kind == "wait" {
						want = task.StatePaused
					}
					if result.Status == "failed" || after.Status != RunWaiting || afterTask.State != want || afterAttempt != "waiting" || steps != 1 || audits != 1 {
						t.Fatalf("healthy accepted wait failed: result=%+v worker=%s task=%s attempt=%s steps=%d audits=%d", result, after.Status, afterTask.State, afterAttempt, steps, audits)
					}
					return
				}
				if result.Status != "failed" || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeTask, afterTask) || beforeAttempt != afterAttempt || steps != 0 || audits != 0 {
					t.Fatalf("changed %s authority accepted %s wait: result=%+v worker=%s->%s task=%s->%s attempt=%s->%s steps=%d audits=%d", change, kind, result, before.Status, after.Status, beforeTask.State, afterTask.State, beforeAttempt, afterAttempt, steps, audits)
				}
			})
		}
	}
}
