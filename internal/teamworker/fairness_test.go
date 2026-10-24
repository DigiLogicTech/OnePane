package teamworker

import (
	"context"

	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type fairnessFixture struct {
	db     *sqlitestore.DB
	tasks  *task.Service
	teams  *team.Service
	groups map[string]string
}

func newFairnessFixture(t *testing.T) fairnessFixture {
	t.Helper()
	ctx := context.Background()
	db := migratedWorkerDatabase(t)
	var err error
	db.SQL().SetMaxOpenConns(1)
	f := fairnessFixture{db: db, groups: map[string]string{}}
	f.tasks = task.NewService(db.SQL(), db, clock.Real{})
	f.teams = team.NewService(db.SQL(), db, clock.Real{}, f.tasks)
	f.exec(t, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`)
	for _, ws := range []string{"denied", "healthy"} {
		f.exec(t, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,1,1)`, ws, ws)
		f.exec(t, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,'human','active',1,1)`, ws)
		g, e := f.teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: ws, Name: ws, CreatedBy: "human"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.teams.AddMember(ctx, team.AddMemberCommand{TeamID: g.ID, MemberKind: "agent", DisplayName: "Worker", RoleName: "Reviewer", CreatedBy: "human"}); e != nil {
			t.Fatal(e)
		}
		f.groups[ws] = g.ID
	}
	worker := f.worker()
	for _, ws := range []string{"denied", "healthy"} {
		if err = worker.ensureWorkspace(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f fairnessFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.db.SQL().Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f fairnessFixture) worker() *Service {
	return New(f.db.SQL(), f.db, clock.Real{}, f.teams, scheduler.NewService(f.db.SQL(), f.db, clock.Real{}), nil, nil, nil, nil)
}
func (f fairnessFixture) session(t *testing.T, ws string, order int, research bool) (team.Session, string) {
	t.Helper()
	ctx := context.Background()
	job, e := f.tasks.Create(ctx, task.CreateCommand{WorkspaceID: ws, Objective: "Fair reasoning"})
	if e != nil {
		t.Fatal(e)
	}
	ss, e := f.teams.StartSession(ctx, team.StartSessionCommand{TaskID: job.ID, TeamID: f.groups[ws], CreatedBy: "human", ExecutionMode: "team"})
	if e != nil {
		t.Fatal(e)
	}
	turns, e := f.teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human"})
	if e != nil || len(turns) != 1 {
		t.Fatalf("round: %+v %v", turns, e)
	}
	f.exec(t, `UPDATE team_sessions SET created_at=? WHERE id=?`, order, ss.ID)
	f.exec(t, `UPDATE team_turn_requests SET created_at=? WHERE session_id=?`, order, ss.ID)
	if research {
		f.exec(t, `UPDATE team_session_manifests SET research_mode=1,execution_mode='council' WHERE session_id=?`, ss.ID)
	}
	return ss, turns[0].ID
}

func TestTeamFairScanSkipsRevokedPrefixAfterReconstruction(t *testing.T) {
	for _, revoke := range []string{"membership", "workspace"} {
		t.Run(revoke, func(t *testing.T) {
			f := newFairnessFixture(t)
			for i := 0; i < 3; i++ {
				f.session(t, "denied", i, false)
			}
			_, healthy := f.session(t, "healthy", 100, false)
			if revoke == "membership" {
				f.exec(t, `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='denied' AND principal_id=?`, WorkerPrincipal)
			} else {
				f.exec(t, `UPDATE workspaces SET status='archived' WHERE id='denied'`)
			}
			for i := 0; i < 2; i++ {
				results, e := f.worker().Tick(context.Background(), 1)
				if e != nil {
					t.Fatal(e)
				}
				if i == 0 && (len(results) != 1 || results[0].TurnID != healthy || results[0].Status != "blocked") {
					t.Fatalf("healthy turn starved: %+v", results)
				}
				if len(results) > 1 {
					t.Fatalf("limit exceeded: %+v", results)
				}
			}
			var count int
			if e := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE workspace_id='denied' AND status='pending'`).Scan(&count); e != nil || count != 3 {
				t.Fatalf("denied evidence changed: %d %v", count, e)
			}
			var status string
			if e := f.db.SQL().QueryRow(`SELECT status FROM workspace_memberships WHERE workspace_id='denied' AND principal_id=?`, WorkerPrincipal).Scan(&status); e != nil || revoke == "membership" && status != "revoked" {
				t.Fatalf("membership changed: %s %v", status, e)
			}
		})
	}
}

func TestResearchFairScanSkipsDeniedAndRotatesParkedPrefix(t *testing.T) {
	f := newFairnessFixture(t)
	denied, _ := f.session(t, "denied", 0, true)
	f.exec(t, `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='denied' AND principal_id=?`, WorkerPrincipal)
	for i := 1; i <= 65; i++ {
		f.session(t, "healthy", i, true)
	}
	last, turn := f.session(t, "healthy", 100, true)
	f.exec(t, `UPDATE team_sessions SET round_number=99 WHERE id=?`, last.ID)
	f.exec(t, `UPDATE team_turn_requests SET round_number=99,status='succeeded' WHERE id=?`, turn)
	w := f.worker()
	for i := 0; i < 3; i++ {
		if e := w.advanceResearchSessions(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	got, e := f.teams.Session(context.Background(), last.ID)
	if e != nil || got.Status != "team_review" {
		t.Fatalf("later Council starved: %+v %v", got, e)
	}
	got, e = f.teams.Session(context.Background(), denied.ID)
	if e != nil || got.Status != "deliberating" || got.RoundNumber != 1 {
		t.Fatalf("denied Council changed: %+v %v", got, e)
	}
	// Reconstruction must also skip denied sessions without regranting them.
	if e = f.worker().advanceResearchSessions(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestTeamWorkspaceAuthorityDoesNotHideDatabaseFailure(t *testing.T) {
	f := newFairnessFixture(t)
	f.session(t, "healthy", 1, false)
	w := f.worker()
	if e := f.db.Close(); e != nil {
		t.Fatal(e)
	}
	if e := w.ensureWorkspace(context.Background(), "healthy"); e == nil {
		t.Fatal("closed database accepted authority")
	}
	if _, e := w.Tick(context.Background(), 1); e == nil {
		t.Fatal("closed database hidden by Tick")
	}

}

func TestResearchFairScanWrapsToEarlierNewWork(t *testing.T) {
	f := newFairnessFixture(t)
	w := f.worker()
	first, _ := f.session(t, "healthy", 100, true)
	if e := w.advanceResearchSessions(context.Background()); e != nil {
		t.Fatal(e)
	}
	// New work can sort before the retained cursor after a clock rollback.
	earlier, turn := f.session(t, "healthy", 1, true)
	f.exec(t, `UPDATE team_sessions SET round_number=99 WHERE id=?`, earlier.ID)
	f.exec(t, `UPDATE team_turn_requests SET round_number=99,status='succeeded' WHERE id=?`, turn)
	for i := 0; i < 2; i++ {
		if e := w.advanceResearchSessions(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	got, e := f.teams.Session(context.Background(), earlier.ID)
	if e != nil || got.Status != "team_review" {
		t.Fatalf("earlier work starved: %+v %v", got, e)
	}
	got, e = f.teams.Session(context.Background(), first.ID)
	if e != nil || got.Status != "deliberating" {
		t.Fatalf("parked work changed: %+v %v", got, e)
	}
}

func TestTeamFairScanPreservesDisabledPrincipalAndBootstrapsMissingMembership(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmtBool(disabled), func(t *testing.T) {
			f := newFairnessFixture(t)
			_, turn := f.session(t, "healthy", 1, false)
			if disabled {
				f.exec(t, `UPDATE principals SET status='disabled' WHERE id=?`, WorkerPrincipal)
			} else {
				f.exec(t, `DELETE FROM workspace_memberships WHERE workspace_id='healthy' AND principal_id=?`, WorkerPrincipal)
			}
			results, e := f.worker().Tick(context.Background(), 1)
			if e != nil {
				t.Fatal(e)
			}
			if disabled {
				if len(results) != 0 {
					t.Fatalf("disabled principal processed work: %+v", results)
				}
			} else if len(results) != 1 || results[0].TurnID != turn || results[0].Status != "blocked" {
				t.Fatalf("missing membership not bootstrapped: %+v", results)
			}
			var status string
			if e := f.db.SQL().QueryRow(`SELECT status FROM principals WHERE id=?`, WorkerPrincipal).Scan(&status); e != nil || disabled && status != "disabled" {
				t.Fatalf("principal changed: %s %v", status, e)
			}
		})
	}
}

func fmtBool(value bool) string {
	if value {
		return "disabled"
	}
	return "bootstrap"
}

type researchTransitionClock struct {
	clock.Real
	change func()
}

func (c researchTransitionClock) UnixMilli() int64 { c.change(); return time.Now().UnixMilli() }

func TestResearchFairScanRejectsStaleFinalTransition(t *testing.T) {
	for _, change := range []string{"task_cancel", "session_round", "membership"} {
		t.Run(change, func(t *testing.T) {
			f := newFairnessFixture(t)
			ss, turn := f.session(t, "healthy", 1, true)
			f.exec(t, `UPDATE team_sessions SET round_number=99 WHERE id=?`, ss.ID)
			f.exec(t, `UPDATE team_turn_requests SET round_number=99,status='succeeded' WHERE id=?`, turn)
			w := f.worker()
			w.clock = researchTransitionClock{change: func() {
				switch change {
				case "task_cancel":
					f.exec(t, `UPDATE tasks SET state='cancel_requested',revision=revision+1 WHERE id=?`, ss.TaskID)
				case "session_round":
					f.exec(t, `UPDATE team_sessions SET round_number=100,revision=revision+1 WHERE id=?`, ss.ID)
				case "membership":
					f.exec(t, `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='healthy' AND principal_id=?`, WorkerPrincipal)
				}
			}}
			if e := w.advanceResearchSessions(context.Background()); e != nil {
				t.Fatal(e)
			}
			got, e := f.teams.Session(context.Background(), ss.ID)
			if e != nil || got.Status != "deliberating" {
				t.Fatalf("stale Council observation applied: %+v %v", got, e)
			}
		})
	}
}
