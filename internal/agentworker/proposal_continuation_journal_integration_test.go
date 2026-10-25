//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

// Exercise the real continuation handlers and SQLite journal/audit boundary.
// Inputs represent a response whose preceding successful model journal already
// committed. This focused boundary test executes no scheduler, transport, model,
// runtime or tool and makes no assertion about physical external side effects.
func applyProposalContinuation(t *testing.T, w *Service, ctx context.Context, run Run, kind string) TickResult {
	t.Helper()
	response := agentprotocol.Response{ProtocolVersion: agentprotocol.Version, ProposalType: agentprotocol.ProposalType(kind), Message: "Synthetic recorded reasoning", Proposal: json.RawMessage(`{"reason":"Review the next reasoning step","instructions":"Use the checked revised approach"}`)}
	result := TickResult{TaskID: run.TaskID, RunID: run.ID, Proposal: kind}
	if kind == "replan" {
		return w.handleReplan(ctx, run, response, result)
	}
	return w.handleEscalate(ctx, run, response, result)
}

func prepareProposalContinuation(t *testing.T, kind string) (*Service, context.Context, Run) {
	t.Helper()
	w, ctx, run, cleanup := prepareUnknownDispatch(t)
	t.Cleanup(cleanup)
	candidate := "previous-checked-candidate"
	if err := w.markReasoningDispatch(ctx, &run, "model_deployment", candidate); err != nil {
		t.Fatal(err)
	}
	// This immutable prior observation must survive rejected proposal continuation.
	if err := w.journal(ctx, run.ID, "model", "succeeded", strPtr("model_deployment"), &candidate, &kind, nil, map[string]any{"message": "Synthetic prior reasoning observation"}); err != nil {
		t.Fatal(err)
	}
	return w, ctx, run
}

func proposalContinuationJournalCounts(t *testing.T, w *Service, ctx context.Context, runID, kind string) (int, int) {
	t.Helper()
	var steps, audits int
	if err := w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind=?),(SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='agent_worker.step' AND json_extract(payload_json,'$.kind')=?)`, runID, kind, runID, kind).Scan(&steps, &audits); err != nil {
		t.Fatal(err)
	}
	return steps, audits
}

func TestProposalContinuationRequiresCommittedJournal(t *testing.T) {
	for _, kind := range []string{"replan", "escalate"} {
		for _, failure := range []string{"none", "worker", "step", "audit"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				w, ctx, run := prepareProposalContinuation(t, kind)
				if failure != "none" {
					statement := fmt.Sprintf(`CREATE TRIGGER reject_proposal_continuation BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='%s' BEGIN SELECT RAISE(ABORT,'proposal continuation journal rejected'); END`, kind)
					if failure == "audit" {
						statement = fmt.Sprintf(`CREATE TRIGGER reject_proposal_continuation BEFORE INSERT ON events WHEN NEW.event_type='agent_worker.step' AND json_extract(NEW.payload_json,'$.kind')='%s' BEGIN SELECT RAISE(ABORT,'proposal continuation audit rejected'); END`, kind)
					}
					if failure == "worker" {
						statement = `CREATE TRIGGER reject_proposal_continuation BEFORE UPDATE ON agent_worker_runs BEGIN SELECT RAISE(ABORT,'proposal continuation update rejected'); END`
					}
					if _, err := w.db.ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				result := applyProposalContinuation(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if failure != "none" {
					if result.Status != "failed" || !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 {
						t.Fatalf("rejected %s %s changed durable continuation: result=%+v revision=%d->%d replan=%d->%d escalation=%d->%d continuation=%s route=%s steps=%d audits=%d", kind, failure, result, before.Revision, after.Revision, before.ReplanCount, after.ReplanCount, before.EscalationCount, after.EscalationCount, after.Continuation, after.RoutePolicy, steps, audits)
					}
				} else {
					want := "replanning"
					if kind == "escalate" {
						want = "escalating"
					}
					if result.Status != want || after.Revision != before.Revision+1 || steps != 1 || audits != 1 || after.StepCount != before.StepCount {
						t.Fatalf("healthy continuation did not atomically record one result: result=%+v before=%+v after=%+v steps=%d audits=%d", result, before, after, steps, audits)
					}
					if kind == "replan" && after.ReplanCount != before.ReplanCount+1 || kind == "escalate" && after.EscalationCount != before.EscalationCount+1 {
						t.Fatal("healthy proposal lost bounded counter")
					}
				}
				currentTask, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				var attemptStatus string
				var attempts, priorSteps int
				if err := w.db.QueryRowContext(ctx, `SELECT status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=?),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='model' AND status='succeeded') FROM task_attempts WHERE id=?`, run.TaskID, run.ID, run.AttemptID).Scan(&attemptStatus, &attempts, &priorSteps); err != nil {
					t.Fatal(err)
				}
				if currentTask.State != task.StateRunning || attemptStatus != "running" || attempts != 1 || priorSteps != 1 {
					t.Fatalf("continuation changed original history: task=%s attempt=%s count=%d modelSteps=%d", currentTask.State, attemptStatus, attempts, priorSteps)
				}
				if failure != "none" {
					if _, err := w.Tick(ctx, 1); !errors.Is(err, ErrDispatchRequiresReconciliation) {
						t.Fatalf("failed continuation permitted another scheduler step: %v", err)
					}
					// Remove the injected Worker UPDATE failure before exercising
					// explicit startup recovery, which also updates that same row.
					if failure == "worker" {
						if _, err := w.db.ExecContext(ctx, `DROP TRIGGER reject_proposal_continuation`); err != nil {
							t.Fatal(err)
						}
					}
					rebuilt := New(w.db, w.tx, clock.Real{}, "node-local", w.tasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
					if recovered, err := rebuilt.RecoverLostRuns(ctx); err != nil || recovered != 1 {
						t.Fatalf("restart could not park original unapplied response owner: %d %v", recovered, err)
					}
					if recovered, err := rebuilt.RecoverLostRuns(ctx); err != nil || recovered != 0 {
						t.Fatalf("restart repeated original recovery: %d %v", recovered, err)
					}
					var taskState, attemptState, workerState string
					if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM task_attempts WHERE task_id=t.id) FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, run.ID).Scan(&taskState, &attemptState, &workerState, &attempts); err != nil {
						t.Fatal(err)
					}
					if taskState != "blocked" || attemptState != "interrupted" || workerState != "interrupted" || attempts != 1 {
						t.Fatalf("restart changed original lineage: task=%s attempt=%s worker=%s attempts=%d", taskState, attemptState, workerState, attempts)
					}
				} else {
					duplicate := applyProposalContinuation(t, w, ctx, run, kind)
					repeated, err := w.getRun(ctx, run.ID)
					if err != nil {
						t.Fatal(err)
					}
					if duplicate.Status != "failed" || !reflect.DeepEqual(after, repeated) || w.dispatchReconciliationFailed {
						t.Fatalf("stale redelivery changed or halted a newer owner: %+v", duplicate)
					}
				}
			})
		}
	}
}

// Task control and ownership can change after the response's successful model
// journal commits and before its proposal handler writes the continuation. No
// Worker revision change is needed for a human Task pause or cancelled intent.
func TestProposalContinuationRejectsChangedExecutionBinding(t *testing.T) {
	for _, kind := range []string{"replan", "escalate"} {
		for _, change := range []string{"pause", "cancel", "attempt_interrupted", "attempt_principal", "tenant_disabled", "task_foreign_workspace", "supplied_workspace"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				w, ctx, run := prepareProposalContinuation(t, kind)
				cur, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "pause":
					_, err = w.tasks.Pause(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: strPtr(WorkerPrincipal), Reason: "Pause before applying recorded proposal"})
				case "cancel":
					_, err = w.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: strPtr(WorkerPrincipal), Reason: "Cancel before applying recorded proposal"})
				case "attempt_interrupted":
					_, err = w.db.ExecContext(ctx, `UPDATE task_attempts SET status='interrupted' WHERE id=?`, run.AttemptID)
				case "attempt_principal":
					_, err = w.db.ExecContext(ctx, `UPDATE task_attempts SET worker_principal_id=? WHERE id=?`, AuthorityPrincipal, run.AttemptID)
				case "tenant_disabled":
					_, err = w.db.ExecContext(ctx, `UPDATE workspaces SET status='suspended' WHERE id=?`, run.WorkspaceID)
				case "task_foreign_workspace":
					now := clock.Real{}.UnixMilli()
					_, err = w.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Other','active',1,?,?)`, now, now)
					if err == nil {
						_, err = w.db.ExecContext(ctx, `UPDATE tasks SET workspace_id='foreign' WHERE id=?`, run.TaskID)
					}
				case "supplied_workspace":
					run.WorkspaceID = "foreign"
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				result := applyProposalContinuation(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if result.Status != "failed" || !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 {
					t.Fatalf("changed %s binding accepted %s continuation: result=%+v revision=%d->%d replan=%d->%d escalation=%d->%d steps=%d audits=%d", change, kind, result, before.Revision, after.Revision, before.ReplanCount, after.ReplanCount, before.EscalationCount, after.EscalationCount, steps, audits)
				}
			})
		}
	}
}

func TestProposalContinuationUsesDurableRouteAndLimits(t *testing.T) {
	for _, change := range []string{"replan_limit", "escalate_limit", "routing_disabled", "current_candidate", "current_exclusions", "cancelled_context"} {
		t.Run(change, func(t *testing.T) {
			kind := "escalate"
			if change == "replan_limit" {
				kind = "replan"
			}
			w, ctx, run := prepareProposalContinuation(t, kind)
			policy := defaultRoutePolicy()
			switch change {
			case "replan_limit":
				if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET replan_count=max_replans WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "escalate_limit":
				if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET escalation_count=max_escalations WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "routing_disabled":
				policy.RoutingEnabled = false
			case "current_candidate":
				if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET last_candidate_id='durable-candidate' WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "current_exclusions":
				policy.ExcludedCandidateIDs = []string{"already-excluded", "previous-checked-candidate"}
			}
			raw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET route_policy_json=? WHERE id=?`, string(raw), run.ID); err != nil {
				t.Fatal(err)
			}
			before, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			callCtx := ctx
			if change == "cancelled_context" {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			// Keep the original caller snapshot deliberately stale while binding
			// the unchanged exact revision. This helper must reread policy/counters.
			err = w.commitProposalContinuation(callCtx, run, kind, "Checked reason", "Checked instructions")
			after, readErr := w.getRun(ctx, run.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
			var want error
			switch change {
			case "replan_limit":
				want = ErrReplanLimit
			case "escalate_limit":
				want = ErrEscalationLimit
			case "routing_disabled":
				want = errProposalRoutingDisabled
			case "cancelled_context":
				want = context.Canceled
			}
			if want != nil {
				wantHalted := change == "cancelled_context"
				if !errors.Is(err, want) || !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 || w.dispatchReconciliationFailed != wantHalted {
					t.Fatalf("pre-write rejection changed state: err=%v want=%v before=%+v after=%+v steps=%d audits=%d halted=%v", err, want, before, after, steps, audits, w.dispatchReconciliationFailed)
				}
				if wantHalted {
					if _, err := w.Tick(ctx, 1); !errors.Is(err, ErrDispatchRequiresReconciliation) {
						t.Fatalf("unresolved canceled continuation permitted scheduler retry: %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var actual routePolicy
			if err = json.Unmarshal(after.RoutePolicy, &actual); err != nil {
				t.Fatal(err)
			}
			wantCandidates := []string{"durable-candidate"}
			if change == "current_exclusions" {
				wantCandidates = policy.ExcludedCandidateIDs
			}
			if !reflect.DeepEqual(actual.ExcludedCandidateIDs, wantCandidates) || steps != 1 || audits != 1 || after.EscalationCount != before.EscalationCount+1 {
				t.Fatalf("lost durable route: candidates=%v want=%v steps=%d audits=%d", actual.ExcludedCandidateIDs, wantCandidates, steps, audits)
			}
		})
	}
}

// Construct the real immutable accepted graph, then start its exact contribution
// owner without dispatching any inference. The existing unrelated fixture owner
// remains in its own Task and cannot grant authority to the contribution.
func prepareTeamProposalContinuation(t *testing.T, kind string) (*Service, context.Context, Run, *team.Service, string, string) {
	t.Helper()
	w, ctx, _ := prepareProposalContinuation(t, kind)
	for _, statement := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('proposal-human','human','Operator','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','proposal-human','active',1,1)`,
	} {
		if _, err := w.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	teams := team.NewService(w.db, w.tx, clock.Real{}, w.tasks)
	w.tasks.SetAdmissionGuard(teams)
	root, err := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Checked Team root"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Checked Team", CreatedBy: "proposal-human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Builder", CreatedBy: "proposal-human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: root.ID, TeamID: group.ID, CreatedBy: "proposal-human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: member.ID, Objective: "Checked contribution"}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "proposal-human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "proposal-human"}); err != nil {
		t.Fatal(err)
	}
	var childID string
	if err = w.db.QueryRowContext(ctx, `SELECT task_id FROM team_execution_assignments WHERE session_id=?`, session.ID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	child, err := w.tasks.Get(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	child, err = w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision, ActorPrincipalID: strPtr("proposal-human")})
	if err != nil {
		t.Fatal(err)
	}
	run, result := w.startRun(ctx, child)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if err = w.markReasoningDispatch(ctx, &run, "model_deployment", "previous-checked-candidate"); err != nil {
		t.Fatal(err)
	}
	if err = w.journal(ctx, run.ID, "model", "succeeded", strPtr("model_deployment"), strPtr("previous-checked-candidate"), &kind, nil, map[string]any{"message": "Synthetic recorded Team response"}); err != nil {
		t.Fatal(err)
	}
	return w, ctx, run, teams, session.ID, plan.ID
}

func TestProposalContinuationRechecksAcceptedTeamAndAncestorHolds(t *testing.T) {
	for _, kind := range []string{"replan", "escalate"} {
		for _, change := range []string{"none", "hold_with_running_child", "root_criteria", "child_criteria", "foreign_ancestor"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				w, ctx, run, teams, sessionID, planID := prepareTeamProposalContinuation(t, kind)
				var rootID string
				if err := w.db.QueryRowContext(ctx, `SELECT task_id FROM team_sessions WHERE id=?`, sessionID).Scan(&rootID); err != nil {
					t.Fatal(err)
				}
				var err error
				switch change {
				case "hold_with_running_child":
					root, e := w.tasks.Get(ctx, rootID)
					if e != nil {
						t.Fatal(e)
					}
					_, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: sessionID, PlanID: planID, ActorPrincipalID: "proposal-human", ExpectedTaskRevision: root.Revision, Reason: "Pause before applying recorded proposal"})
					if err == nil {
						// Deliberately model a damaged/lagging Task projection. The
						// durable ancestor hold must independently deny continuation.
						_, err = w.db.ExecContext(ctx, `UPDATE tasks SET state='running' WHERE id=?`, run.TaskID)
					}
				case "root_criteria":
					_, err = w.db.ExecContext(ctx, `UPDATE tasks SET completion_json='{"changed_criteria":true}' WHERE id=?`, rootID)
				case "child_criteria":
					_, err = w.db.ExecContext(ctx, `UPDATE tasks SET completion_json='{"changed_criteria":true}' WHERE id=?`, run.TaskID)
				case "foreign_ancestor":
					_, err = w.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign-ancestor','Other','active',1,1,1)`)
					if err == nil {
						_, err = w.db.ExecContext(ctx, `UPDATE tasks SET workspace_id='foreign-ancestor' WHERE id=?`, rootID)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				result := applyProposalContinuation(t, w, ctx, run, kind)
				after, err := w.getRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				steps, audits := proposalContinuationJournalCounts(t, w, ctx, run.ID, kind)
				if change == "none" {
					if result.Status == "failed" || steps != 1 || audits != 1 || after.Revision != before.Revision+1 {
						t.Fatalf("healthy Team continuation failed: %+v", result)
					}
				} else if result.Status != "failed" || !reflect.DeepEqual(before, after) || steps != 0 || audits != 0 || w.dispatchReconciliationFailed {
					t.Fatalf("Team %s continuation escaped authority fence: result=%+v steps=%d audits=%d halted=%v", change, result, steps, audits, w.dispatchReconciliationFailed)
				}
			})
		}
	}
}
