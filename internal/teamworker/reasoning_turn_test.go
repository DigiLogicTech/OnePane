package teamworker

import (
	"context"
	"errors"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"testing"
)

func TestReasoningTurnClaimDispatchAndLateFailureCancellationFences(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/reasoning.db")
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
	} {
		if _, err = db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	job, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Reason"})
	if err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Reasoning", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Reviewer", RoleName: "Reviewer", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: job.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	tr := turns[0]
	worker := New(db.SQL(), db, clock.Real{}, teams, nil, nil, nil, nil, nil)
	setState := func(state string) {
		t.Helper()
		if _, err := db.SQL().Exec(`UPDATE tasks SET state=? WHERE id=?`, state, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	claim := func() error { return worker.claimReasoningTurn(ctx, "tenant", ss.ID, tr.ID, member.ID, tr.RoundNumber) }
	check := func() error {
		return worker.requireRunningReasoningTurn(ctx, "tenant", ss.ID, tr.ID, member.ID, tr.RoundNumber)
	}
	setState("cancel_requested")
	if err = claim(); err == nil {
		t.Fatal("cancelled Task claimed")
	}
	var status string
	if err = db.SQL().QueryRow(`SELECT status FROM team_turn_requests WHERE id=?`, tr.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("claim changed pending turn: %s %v", status, err)
	}
	setState("paused")
	if err = claim(); err != nil {
		t.Fatalf("paused deliberation claim: %v", err)
	}
	if err = check(); err != nil {
		t.Fatal(err)
	}
	setState("cancel_requested")
	if err = check(); err == nil {
		t.Fatal("dispatch allowed after cancellation")
	}
	setState("created")
	if err = worker.requireRunningReasoningTurn(ctx, "tenant", ss.ID, tr.ID, "foreign-member", tr.RoundNumber); err == nil {
		t.Fatal("foreign seat dispatched")
	}
	if err = worker.requireRunningReasoningTurn(ctx, "tenant", ss.ID, tr.ID, member.ID, tr.RoundNumber+1); err == nil {
		t.Fatal("stale round dispatched")
	}
	for _, terminal := range []string{"cancelled", "succeeded"} {
		if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status=? WHERE id=?`, terminal, tr.ID); err != nil {
			t.Fatal(err)
		}
		failed := worker.fail(ctx, Result{TurnID: tr.ID}, errors.New("late transport failure"))
		blocked := worker.block(ctx, Result{TurnID: tr.ID}, errors.New("late admission failure"))
		if failed.Status != terminal || blocked.Status != terminal {
			t.Fatalf("lost owner reported invented outcome: %+v %+v", failed, blocked)
		}
		if err = db.SQL().QueryRow(`SELECT status FROM team_turn_requests WHERE id=?`, tr.ID).Scan(&status); err != nil || status != terminal {
			t.Fatalf("late failure overwrote %s: %s %v", terminal, status, err)
		}
	}
	if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status='running' WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`CREATE TRIGGER reject_turn_failure BEFORE UPDATE OF status ON team_turn_requests WHEN NEW.status='failed' BEGIN SELECT RAISE(ABORT,'turn persistence unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	failed := worker.fail(ctx, Result{TurnID: tr.ID}, errors.New("transport failed"))
	if failed.Status != "unknown" || failed.Error == "transport failed" {
		t.Fatalf("persistence failure hidden: %+v", failed)
	}
	if err = db.SQL().QueryRow(`SELECT status FROM team_turn_requests WHERE id=?`, tr.ID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("persistence failure changed turn: %s %v", status, err)
	}
	if _, err = db.SQL().Exec(`DROP TRIGGER reject_turn_failure`); err != nil {
		t.Fatal(err)
	}
	// A rejected claim observes the existing owner without failing its turn.
	observed := worker.process(ctx, tr.ID, "tenant", ss.ID, member.ID, tr.RoundNumber, "")
	if observed.Status != "running" {
		t.Fatalf("claim rejection invented outcome: %+v", observed)
	}
	if err = db.SQL().QueryRow(`SELECT status FROM team_turn_requests WHERE id=?`, tr.ID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("claim rejection changed another owner: %s %v", status, err)
	}
	// Sixteen older pending turns whose Tasks lost authority must not consume
	// the whole bounded scan forever. The healthy later turn reaches routing.
	for i := 0; i < 16; i++ {
		old, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Old pending reasoning"})
		if err != nil {
			t.Fatal(err)
		}
		oldSession, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: old.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: oldSession.ID, RequestedByPrincipalID: "human"}); err != nil {
			t.Fatal(err)
		}
		if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET created_at=1 WHERE session_id=?`, oldSession.ID); err != nil {
			t.Fatal(err)
		}
		state := "cancel_requested"
		if i%2 == 1 {
			state = "blocked"
		}
		if _, err = db.SQL().Exec(`UPDATE tasks SET state=? WHERE id=?`, state, old.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status='pending',created_at=100 WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}
	worker.scheduler = scheduler.NewService(db.SQL(), db, clock.Real{})
	results, err := worker.Tick(ctx, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].TurnID != tr.ID || results[0].Status != "blocked" {
		t.Fatalf("old ineligible work starved healthy routing: %+v", results)
	}
	var preserved int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE id<>? AND status='pending'`, tr.ID).Scan(&preserved); err != nil || preserved != 16 {
		t.Fatalf("ineligible pending evidence modified: %d %v", preserved, err)
	}
}
