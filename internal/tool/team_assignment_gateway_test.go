package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamPausingAuthority struct {
	pause func() error
	calls int
}

func (a *teamPausingAuthority) Consume(context.Context, authority.ConsumeCommand) (authority.Lease, error) {
	a.calls++
	return authority.Lease{}, a.pause()
}

func TestTeamAssignmentPauseDuringPermitConsumptionNeverReachesToolAdapter(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/team-gateway.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','human','active',1,1)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
	parent, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "Team objective"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "ws", Name: "Team", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Builder", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: parent.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: member.ID, Objective: "Build"}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	var child string
	if err = db.SQL().QueryRowContext(ctx, `SELECT task_id FROM team_execution_assignments WHERE session_id=?`, session.ID).Scan(&child); err != nil {
		t.Fatal(err)
	}
	// A normal permitted call establishes that this is not merely an early
	// denial fixture. Production lease/context services are tested separately.
	gateway, adapter, _ := scopedReadGateway(t, db.SQL(), &fakeAuthority{})
	cmd := scopedReadCommand()
	cmd.TaskID = &child
	if _, err = gateway.Invoke(ctx, cmd); err != nil || adapter.calls != 1 {
		t.Fatalf("active assignment did not reach adapter: %v", err)
	}
	pausing := &teamPausingAuthority{pause: func() error {
		current, e := tasks.Get(ctx, parent.ID)
		if e != nil {
			return e
		}
		_, e = tasks.Pause(ctx, task.TransitionCommand{TaskID: parent.ID, ExpectedRevision: current.Revision})
		return e
	}}
	gateway, adapter, _ = scopedReadGateway(t, db.SQL(), pausing)
	denied, err := gateway.Invoke(ctx, cmd)
	if !errors.Is(err, ErrPolicyDenied) || denied.ErrorCode == nil || *denied.ErrorCode != "feature_policy_revoked" || pausing.calls != 1 || adapter.calls != 0 {
		t.Fatalf("paused Team reached Tool adapter: invocation=%+v auth=%d adapter=%d err=%v", denied, pausing.calls, adapter.calls, err)
	}
}
