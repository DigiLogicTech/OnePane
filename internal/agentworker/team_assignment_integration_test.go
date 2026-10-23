//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type teamResultTransport struct {
	mu          sync.Mutex
	calls       []string
	independent bool
	observe     func(context.Context) error
}

func (m *teamResultTransport) Dispatch(ctx context.Context, request inference.DispatchRequest, _ inference.SecretResolver) (inference.DispatchResult, error) {
	var body struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if json.Unmarshal(request.RequestJSON, &body) != nil {
		return inference.DispatchResult{}, fmt.Errorf("invalid model request")
	}
	var req agentprotocol.Request
	for _, message := range body.Messages {
		if message.Role == "user" {
			if err := json.Unmarshal([]byte(message.Content), &req); err != nil {
				return inference.DispatchResult{}, err
			}
		}
	}
	m.mu.Lock()
	m.calls = append(m.calls, req.Objective)
	m.mu.Unlock()
	if m.observe != nil && req.Objective != "Consolidate Team result" {
		if err := m.observe(ctx); err != nil {
			return inference.DispatchResult{}, err
		}
	}
	var contributions []teamgate.Contribution
	var guidance []teamgate.HumanGuidance
	for _, section := range req.Context {
		if section.Kind == "team_guidance" {
			if !section.Authoritative || section.Trust != "USER_INSTRUCTION" || json.Unmarshal(section.Content, &guidance) != nil {
				return inference.DispatchResult{}, fmt.Errorf("invalid human guidance context")
			}
		}
		if section.Kind == "team_contribution" {
			if section.Authoritative || section.Trust != "UNVERIFIED_DERIVED" {
				return inference.DispatchResult{}, fmt.Errorf("contribution text elevated to instructions")
			}
			var c teamgate.Contribution
			if err := json.Unmarshal(section.Content, &c); err != nil {
				return inference.DispatchResult{}, err
			}
			if c.AchievedLevel != "V0" {
				return inference.DispatchResult{}, fmt.Errorf("declaration verification level changed")
			}
			contributions = append(contributions, c)
		}
	}
	expectedGuidance := []string{"Shared direction"}
	if req.Objective != "Consolidate Team result" {
		expectedGuidance = append(expectedGuidance, req.Objective+" direction")
	}
	if len(guidance) != len(expectedGuidance) {
		return inference.DispatchResult{}, fmt.Errorf("wrong scoped human guidance: %+v", guidance)
	}
	for i, text := range expectedGuidance {
		if guidance[i].Text != text {
			return inference.DispatchResult{}, fmt.Errorf("human guidance leaked or reordered: %+v", guidance)
		}
	}
	switch req.Objective {
	case "Builder contribution":
		if len(contributions) != 0 {
			return inference.DispatchResult{}, fmt.Errorf("builder received unapproved inputs")
		}
	case "Reviewer contribution":
		if (!m.independent && (len(contributions) != 1 || contributions[0].Key != "build")) || (m.independent && len(contributions) != 0) {
			return inference.DispatchResult{}, fmt.Errorf("reviewer did not consume builder checkpoint")
		}
	case "Consolidate Team result":
		if len(contributions) != 2 {
			return inference.DispatchResult{}, fmt.Errorf("consolidator missing contributions")
		}
	default:
		return inference.DispatchResult{}, fmt.Errorf("unplanned objective: %s", req.Objective)
	}
	proposal, _ := json.Marshal(map[string]any{"result": map[string]any{"objective": req.Objective, "contribution_count": len(contributions)}})
	response, _ := json.Marshal(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalComplete, Message: "Synthetic test contribution", Proposal: proposal})
	return inference.DispatchResult{ResponseJSON: response, UsageJSON: json.RawMessage(`{}`)}, nil
}

func TestTeamAssignmentsExecuteThroughWorkerAndShareOnlyAcceptedCheckpointResults(t *testing.T) {
	t.Run("complete", func(t *testing.T) { runTeamAssignmentScenario(t, false, "") })
	t.Run("pause_while_admitted", func(t *testing.T) { runTeamAssignmentScenario(t, true, "") })
}

type pausingTeamCoordinator struct {
	inner inference.RuntimeCoordinator
	pause func() error
}

func (c pausingTeamCoordinator) Acquire(ctx context.Context, req inference.RuntimeScheduleRequest) (inference.RuntimeScheduleLease, error) {
	lease, err := c.inner.Acquire(ctx, req)
	if err != nil {
		return nil, err
	}
	if err = c.pause(); err != nil {
		lease.Release(context.Background(), err)
		return nil, err
	}
	return lease, nil
}

func runTeamAssignmentScenario(t *testing.T, pause bool, parallel string, cancellation ...string) {
	cancelPhase := ""
	if len(cancellation) > 0 {
		cancelPhase = cancellation[0]
	}
	holdPhase := strings.TrimPrefix(cancelPhase, "hold_")
	holding := holdPhase != cancelPhase
	ctx := context.Background()
	root := t.TempDir()
	db, err := sqlitestore.Open(root + "/team-worker.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.SQL().SetMaxOpenConns(1)
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Operator','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
		`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','synthetic-team','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('deployment','model','node','fake','{}','ready','fp',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('compatibility','deployment','inference.general','L2',0,64000,'low','supported','{}','{}','cp',1,1,1)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(root + "/blobs")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	infer := inference.NewService(db.SQL(), db, clock.Real{})
	registry := inference.NewTransportRegistry()
	model := &teamResultTransport{independent: parallel != ""}
	if err = registry.Register("fake", model); err != nil {
		t.Fatal(err)
	}
	if err = infer.ConfigureExecution(artifacts, registry, nil, "node"); err != nil {
		t.Fatal(err)
	}
	if err = infer.ConfigureRuntimeCoordinator(runtimecoord.NewService(db.SQL(), clock.Real{})); err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
	tasks.SetAdmissionGuard(teams)
	job, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Consolidate Team result"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Delivery", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	builder, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Builder", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Reviewer", RoleName: "Reviewer", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: job.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	deps := []string{"build"}
	if parallel != "" {
		deps = nil
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: builder.ID, Objective: "Builder contribution"}, {Key: "review", MemberID: reviewer.ID, Objective: "Reviewer contribution", DependsOn: deps}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []map[string]string{
		{"text": "Shared direction"},
		{"text": "Builder contribution direction", "target_member_id": builder.ID},
		{"text": "Reviewer contribution direction", "target_member_id": reviewer.ID},
	} {
		body, _ := json.Marshal(message)
		if _, err = teams.PostMessage(ctx, team.PostMessageCommand{SessionID: session.ID, AuthorPrincipalID: "human", Content: body}); err != nil {
			t.Fatal(err)
		}
	}
	worker := New(db.SQL(), db, clock.Real{}, "node", tasks, nil, nil, scheduler.NewService(db.SQL(), db, clock.Real{}), infer, nil, artifacts, nil, nil, nil, verification.NewService(db.SQL(), db, clock.Real{}))
	if cancelPhase != "" {
		cancelTeam := func() error {
			current, e := tasks.Get(ctx, job.ID)
			if e != nil {
				return e
			}
			command := team.ExecutionControlCommand{SessionID: session.ID, PlanID: plan.ID, ActorPrincipalID: "human", ExpectedTaskRevision: current.Revision, Reason: "Synthetic control boundary"}
			if holding {
				if _, e = teams.PauseExecution(ctx, command); e != nil {
					return e
				}
				room, e := teams.LiveRoom(ctx, session.ID)
				if e != nil || !room.ExecutionPaused || !room.PauseDraining {
					return fmt.Errorf("missing draining projection: %+v %v", room, e)
				}
				current, e = tasks.Get(ctx, job.ID)
				if e != nil {
					return e
				}
				command.ExpectedTaskRevision = current.Revision
				if _, e = teams.ResumeExecution(ctx, command); e != team.ErrSessionState {
					return fmt.Errorf("in-flight Worker resumed: %v", e)
				}
				return nil
			}
			_, e = teams.RequestExecutionCancellation(ctx, command)
			return e
		}
		if cancelPhase == "queued" || holding && holdPhase == "queued" {
			if err = infer.ConfigureRuntimeCoordinator(pausingTeamCoordinator{inner: runtimecoord.NewService(db.SQL(), clock.Real{}), pause: cancelTeam}); err != nil {
				t.Fatal(err)
			}
		} else {
			model.observe = func(context.Context) error { return cancelTeam() }
		}
	}
	if parallel != "" {
		coordinator := runtimecoord.NewService(db.SQL(), clock.Real{})
		if parallel == "capacity" {
			infer.ConfigureRuntimeCoordinator(teamLaneCoordinator{inner: coordinator})
			entered := make(chan struct{}, 2)
			model.observe = func(ctx context.Context) error {
				entered <- struct{}{}
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for len(entered) < 2 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-timer.C:
						return fmt.Errorf("independent assignments did not overlap")
					case <-time.After(time.Millisecond):
					}
				}
				if _, e := worker.Tick(ctx, 1); e != ErrWorkerCycleInProgress {
					return fmt.Errorf("overlapping Tick was admitted: %v", e)
				}
				if _, e := worker.RecoverLostRuns(ctx); e != ErrWorkerCycleInProgress {
					return fmt.Errorf("live dispatch was recovered: %v", e)
				}
				return nil
			}
		} else {
			infer.ConfigureRuntimeCoordinator(coordinator)
			observed := false
			model.observe = func(ctx context.Context) error {
				if observed {
					return nil
				}
				observed = true
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for {
					var waiting int
					if e := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_execution_queue WHERE state='queued'`).Scan(&waiting); e != nil {
						return e
					}
					if waiting == 1 {
						return nil
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-timer.C:
						return fmt.Errorf("constrained Team assignment never queued")
					case <-time.After(time.Millisecond):
					}
				}
			}
		}
	}
	if pause {
		if err = infer.ConfigureRuntimeCoordinator(pausingTeamCoordinator{inner: runtimecoord.NewService(db.SQL(), clock.Real{}), pause: func() error {
			current, e := tasks.Get(ctx, job.ID)
			if e != nil {
				return e
			}
			_, e = tasks.Pause(ctx, task.TransitionCommand{TaskID: job.ID, ExpectedRevision: current.Revision})
			return e
		}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		results, e := worker.Tick(ctx, 10)
		if e != nil {
			t.Fatal(e)
		}
		if cancelPhase != "" {
			if holding {
				room, e := teams.LiveRoom(ctx, session.ID)
				if e != nil || !room.ExecutionPaused || room.PauseDraining || worker.dispatchReconciliationFailed {
					t.Fatalf("pause failed to drain: %+v %v", room, e)
				}
				var checkpoints int
				if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM checkpoints`).Scan(&checkpoints); e != nil || checkpoints != 0 {
					t.Fatalf("paused response applied: %d %v", checkpoints, e)
				}
				var runID, attemptID string
				if e = db.SQL().QueryRow(`SELECT r.id,r.attempt_id FROM agent_worker_runs r JOIN task_attempts a ON a.id=r.attempt_id WHERE r.status='waiting' AND a.status='waiting'`).Scan(&runID, &attemptID); e != nil {
					t.Fatal(e)
				}
				// Reconstruct the service from durable records while held. Merely
				// observing/reconciling after restart must not dispatch another call.
				worker = New(db.SQL(), db, clock.Real{}, "node", tasks, nil, nil, scheduler.NewService(db.SQL(), db, clock.Real{}), infer, nil, artifacts, nil, nil, nil, verification.NewService(db.SQL(), db, clock.Real{}))
				callsBeforeRestart := len(model.calls)
				if _, e = worker.Tick(ctx, 10); e != nil || len(model.calls) != callsBeforeRestart {
					t.Fatalf("held restart dispatched: %v %v", model.calls, e)
				}
				if _, e = teams.ResumeExecution(ctx, team.ExecutionControlCommand{SessionID: session.ID, PlanID: plan.ID, ActorPrincipalID: "human", ExpectedTaskRevision: room.TaskRevision}); e != nil {
					t.Fatal(e)
				}
				model.observe = nil
				if e = infer.ConfigureRuntimeCoordinator(runtimecoord.NewService(db.SQL(), clock.Real{})); e != nil {
					t.Fatal(e)
				}
				for n := 0; n < 5; n++ {
					if _, e = worker.Tick(ctx, 10); e != nil {
						t.Fatal(e)
					}
				}
				current, e := tasks.Get(ctx, job.ID)
				if e != nil || current.State != task.StateComplete {
					t.Fatalf("resumed Team did not finish: %+v %v", current, e)
				}
				var retained int
				if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM agent_worker_runs WHERE id=? AND attempt_id=? AND status='succeeded'`, runID, attemptID).Scan(&retained); e != nil || retained != 1 {
					t.Fatalf("original Worker/Attempt lost: %d %v", retained, e)
				}
				return
			}
			current, e := tasks.Get(ctx, job.ID)
			if e != nil || current.State != task.StateCancelRequested || worker.dispatchReconciliationFailed {
				t.Fatalf("cancellation halted unrelated scheduling or lost root intent: %+v %v", current, e)
			}
			expectedCalls := 0
			if cancelPhase == "response" {
				expectedCalls = 1
			}
			if len(model.calls) != expectedCalls {
				t.Fatalf("unexpected cancelled model effects: %v", model.calls)
			}
			var checkpointCount, parked int
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM checkpoints`).Scan(&checkpointCount); e != nil || checkpointCount != 0 {
				t.Fatalf("cancelled proposal created checkpoint: %d %v", checkpointCount, e)
			}
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM agent_worker_runs r JOIN task_attempts a ON a.id=r.attempt_id WHERE r.status='waiting' AND a.status='waiting'`).Scan(&parked); e != nil || parked != 1 {
				t.Fatalf("original cancelled Attempt not parked: %d %v", parked, e)
			}
			if _, e = worker.Tick(ctx, 10); e != nil {
				t.Fatalf("cancellation poisoned subsequent tick: %v", e)
			}
			return
		}
		if pause {
			current, e := tasks.Get(ctx, job.ID)
			if e != nil || current.State != task.StatePaused || len(model.calls) != 0 {
				t.Fatalf("paused Team reached inference transport: state=%s calls=%v err=%v", current.State, model.calls, e)
			}
			return
		}
		for _, result := range results {
			if result.Error != "" {
				t.Fatalf("worker result: %+v", result)
			}
		}
		current, e := tasks.Get(ctx, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.State == task.StateComplete {
			break
		}
	}
	current, err := tasks.Get(ctx, job.ID)
	if err != nil || current.State != task.StateComplete || len(model.calls) != 3 {
		t.Fatalf("Team did not finish once through ordinary workers: state=%s calls=%v err=%v", current.State, model.calls, err)
	}
	room, err := teams.LiveRoom(ctx, session.ID)
	if err != nil || len(room.Assignments) != 2 {
		t.Fatalf("live contributions: %+v %v", room, err)
	}
	for _, a := range room.Assignments {
		if a.State != "complete" || a.WorkerStatus != "succeeded" {
			t.Fatalf("unobserved completion: %+v", a)
		}
	}
}

// Synthetic independent Node CPU pools exercise the real shared coordinator;
// constrained mode retains one physical pool and must queue the second member.
type teamLaneCoordinator struct{ inner inference.RuntimeCoordinator }

func (c teamLaneCoordinator) Acquire(ctx context.Context, req inference.RuntimeScheduleRequest) (inference.RuntimeScheduleLease, error) {
	lane := *req.Deployment.NodeID + ":" + *req.TaskID
	req.Deployment.NodeID = &lane
	return c.inner.Acquire(ctx, req)
}
func TestTeamAssignmentsExecuteIndependentWorkThroughSharedCapacity(t *testing.T) {
	for _, mode := range []string{"capacity", "constrained"} {
		t.Run(mode, func(t *testing.T) { runTeamAssignmentScenario(t, false, mode) })
	}
}

func TestTeamCancellationDiscardsQueuedAndReturnedModelProposals(t *testing.T) {
	for _, phase := range []string{"queued", "response"} {
		t.Run(phase, func(t *testing.T) { runTeamAssignmentScenario(t, false, "", phase) })
	}
}

func TestTeamPauseDrainsAndResumesOriginalWorkerAfterQueuedAndReturnedReasoning(t *testing.T) {
	for _, phase := range []string{"queued", "response"} {
		t.Run(phase, func(t *testing.T) { runTeamAssignmentScenario(t, false, "", "hold_"+phase) })
	}
}
