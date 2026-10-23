//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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

type teamResultTransport struct{ calls []string }

func (m *teamResultTransport) Dispatch(_ context.Context, request inference.DispatchRequest, _ inference.SecretResolver) (inference.DispatchResult, error) {
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
	m.calls = append(m.calls, req.Objective)
	var contributions []teamgate.Contribution
	for _, section := range req.Context {
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
	switch req.Objective {
	case "Builder contribution":
		if len(contributions) != 0 {
			return inference.DispatchResult{}, fmt.Errorf("builder received unapproved inputs")
		}
	case "Reviewer contribution":
		if len(contributions) != 1 || contributions[0].Key != "build" {
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
	t.Run("complete", func(t *testing.T) { runTeamAssignmentScenario(t, false) })
	t.Run("pause_while_admitted", func(t *testing.T) { runTeamAssignmentScenario(t, true) })
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

func runTeamAssignmentScenario(t *testing.T, pause bool) {
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
	model := &teamResultTransport{}
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
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: builder.ID, Objective: "Builder contribution"}, {Key: "review", MemberID: reviewer.ID, Objective: "Reviewer contribution", DependsOn: []string{"build"}}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	worker := New(db.SQL(), db, clock.Real{}, "node", tasks, nil, nil, scheduler.NewService(db.SQL(), db, clock.Real{}), infer, nil, artifacts, nil, nil, nil, verification.NewService(db.SQL(), db, clock.Real{}))
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
