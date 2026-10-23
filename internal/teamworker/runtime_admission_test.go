package teamworker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

// Exercise the real Team dispatch -> inference -> shared coordinator path.
// No model process is launched: cancellation happens while compute is queued.
func TestTeamDispatchSharesComputeAdmissionWithAssistantAndOtherTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	db, err := sqlitestore.Open(root + "/team-runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(root + "/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	infer := inference.NewService(db.SQL(), db, clock.Real{})
	transports := inference.NewTransportRegistry()
	if err = inference.RegisterBuiltinTransports(transports); err != nil {
		t.Fatal(err)
	}
	if err = infer.ConfigureExecution(artifacts, transports, nil, "node"); err != nil {
		t.Fatal(err)
	}
	coordinator := runtimecoord.NewService(db.SQL(), clock.Real{})
	if err = infer.ConfigureRuntimeCoordinator(coordinator); err != nil {
		t.Fatal(err)
	}
	model, err := infer.RegisterModel(ctx, inference.RegisterModelCommand{ModelRef: "builtin/fake", TrustState: inference.ModelUserTrusted})
	if err != nil {
		t.Fatal(err)
	}
	node, runtime := "node", "fake"
	dep, err := infer.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: model.ID, NodeID: &node, RuntimeName: &runtime, RuntimeConfigJSON: json.RawMessage(`{"runtime_backend":"colibri","placement":{"mode":"cpu"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = infer.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentQualifying})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = infer.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentReady})
	if err != nil {
		t.Fatal(err)
	}
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	job, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Team shares local compute"})
	if err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Shared compute", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Reviewer", RoleName: "Reviewer", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: job.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: session.ID, RequestedByPrincipalID: "human"})
	if err != nil || len(turns) != 1 {
		t.Fatalf("turn creation: %v", err)
	}
	if _, err = db.SQL().ExecContext(ctx, `UPDATE team_turn_requests SET status='running',selected_candidate_id=?,selected_candidate_kind='model_deployment' WHERE id=?`, dep.ID, turns[0].ID); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]string{"session_id": session.ID, "turn_id": turns[0].ID, "member_id": member.ID})
	if err != nil {
		t.Fatal(err)
	}
	worker := &Service{db: db.SQL(), tx: db, clock: clock.Real{}, inference: infer, artifacts: artifacts}
	if err = worker.ensureWorkspace(ctx, "tenant"); err != nil {
		t.Fatal(err)
	}
	busy, err := coordinator.Acquire(ctx, inference.RuntimeScheduleRequest{WorkspaceID: "tenant", Deployment: dep, InferenceRequestID: "assistant-running"})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Release(context.Background(), nil)
	turnCtx, cancelTurn := context.WithCancel(ctx)
	defer cancelTurn()
	result := make(chan error, 1)
	go func() {
		_, err := worker.dispatch(turnCtx, agentprotocol.Request{ProtocolVersion: agentprotocol.Version, PrincipalID: WorkerPrincipal, PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete}, WorkspaceID: "tenant", TaskID: job.ID, Role: "Reviewer", Objective: "Review the objective", Constraints: json.RawMessage(`{"deliberation_only":true}`), ContextManifest: manifest}, scheduler.Candidate{ID: dep.ID, Kind: scheduler.CandidateModel, ProtocolLevel: "L2"}, policy.DataLabel{WorkspaceID: "tenant", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}, "")
		result <- err
	}()
	found := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		entries, e := coordinator.Snapshot(ctx, 20)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			if entry.TaskID != nil && *entry.TaskID == job.ID && entry.State == "queued" {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("Team bypassed shared admission or failed before queue: %v", err)
		default:
		}
		time.Sleep(time.Millisecond)
	}
	if !found {
		t.Fatal("Team inference did not enter the shared queue with its Task identity")
	}
	room, err := teams.LiveRoom(ctx, session.ID)
	if err != nil || len(room.Members) != 1 || room.Members[0].ComputeState != "queued" || room.Members[0].Backend != "colibri" || room.Members[0].ComputeMode != "cpu" {
		t.Fatalf("live room lost actual compute wait: %+v %v", room, err)
	}
	if _, err = db.SQL().ExecContext(ctx, `UPDATE runtime_execution_queue SET workspace_id='foreign' WHERE task_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	room, err = teams.LiveRoom(ctx, session.ID)
	if err != nil || room.Members[0].ComputeState != "" {
		t.Fatalf("foreign queue projected into room: %+v %v", room, err)
	}
	if _, err = db.SQL().ExecContext(ctx, `UPDATE runtime_execution_queue SET workspace_id='tenant' WHERE task_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().ExecContext(ctx, `UPDATE inference_requests SET context_manifest_json=json_set(context_manifest_json,'$.member_id','foreign-member') WHERE task_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	room, err = teams.LiveRoom(ctx, session.ID)
	if err != nil || room.Members[0].ComputeState != "" {
		t.Fatalf("another member's turn projected into room: %+v %v", room, err)
	}
	cancelTurn()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel queued Team dispatch: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("queued Team dispatch did not cancel")
	}
}
