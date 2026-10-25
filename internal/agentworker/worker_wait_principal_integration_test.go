//go:build integration

package agentworker

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// A response recorded while the Worker was authorized cannot acquire current
// authority from its old principal ID after that principal is revoked or has
// changed type. Actual handlers and migrated storage run here; no model/tool
// transport executes and these tests are not physical inference acceptance.
func TestWorkerWaitPrincipalRevocationRejectsRecordedResponses(t *testing.T) {
	for _, scope := range []string{"ordinary", "team"} {
		for _, kind := range []string{"human", "wait", "replan", "escalate"} {
			for _, change := range []string{"disabled", "revoked", "human_type", "membership_suspended", "membership_revoked", "membership_missing"} {
				t.Run(scope+"/"+kind+"/"+change, func(t *testing.T) {
					w, ctx, run := func() (*Service, context.Context, Run) {
						if scope == "team" {
							w, ctx, run, _, _, _ := prepareTeamProposalContinuation(t, kind)
							return w, ctx, run
						}
						return prepareProposalContinuation(t, kind)
					}()
					statement := `UPDATE principals SET status=? WHERE id=?`
					value := change
					if change == "human_type" {
						statement = `UPDATE principals SET principal_type=? WHERE id=?`
						value = "human"
					}
					args := []any{value, WorkerPrincipal}
					switch change {
					case "membership_suspended", "membership_revoked":
						statement = `UPDATE workspace_memberships SET status=? WHERE workspace_id=? AND principal_id=?`
						status := "suspended"
						if change == "membership_revoked" {
							status = "revoked"
						}
						args = []any{status, run.WorkspaceID, WorkerPrincipal}
					case "membership_missing":
						statement = `DELETE FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`
						args = []any{run.WorkspaceID, WorkerPrincipal}
					}
					if _, err := w.db.ExecContext(ctx, statement, args...); err != nil {
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
					var beforeEvents, beforeMessages int
					if err = w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM team_messages)`).Scan(&beforeEvents, &beforeMessages); err != nil {
						t.Fatal(err)
					}
					var result TickResult
					if kind == "human" || kind == "wait" {
						result = applyWorkerWait(t, w, ctx, run, kind)
					} else {
						result = applyProposalContinuation(t, w, ctx, run, kind)
					}
					after, err := w.getRun(ctx, run.ID)
					if err != nil {
						t.Fatal(err)
					}
					afterTask, err := w.tasks.Get(ctx, run.TaskID)
					if err != nil {
						t.Fatal(err)
					}
					var afterEvents, afterMessages, requests, attempts, priorModels int
					var attemptState string
					if err = w.db.QueryRowContext(ctx, `SELECT status,
					 (SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM team_messages),
					 (SELECT COUNT(*) FROM team_assistance_requests),
					 (SELECT COUNT(*) FROM task_attempts WHERE task_id=?),
					 (SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='model' AND status='succeeded')
					 FROM task_attempts WHERE id=?`, run.TaskID, run.ID, run.AttemptID).Scan(&attemptState, &afterEvents, &afterMessages, &requests, &attempts, &priorModels); err != nil {
						t.Fatal(err)
					}
					steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
					if result.Status != "failed" || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeTask, afterTask) || attemptState != "running" || afterEvents != beforeEvents || afterMessages != beforeMessages || requests != 0 || attempts != 1 || priorModels != 1 || steps != 0 || audits != 0 || w.dispatchReconciliationFailed {
						t.Fatalf("revoked principal applied recorded response: result=%+v attempt=%s events=%d->%d messages=%d->%d requests=%d attempts=%d prior=%d steps=%d audits=%d halt=%v", result, attemptState, beforeEvents, afterEvents, beforeMessages, afterMessages, requests, attempts, priorModels, steps, audits, w.dispatchReconciliationFailed)
					}
					if err = w.requireActiveWorkerExecutionBinding(ctx, run); err == nil {
						t.Fatal("revoked principal passed execution preflight")
					}
					if err = w.markReasoningDispatch(ctx, &run, "model_deployment", fmt.Sprintf("denied-%s", change)); err == nil {
						t.Fatal("revoked principal claimed next dispatch")
					}
				})
			}
		}
	}
}
