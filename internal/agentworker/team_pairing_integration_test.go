//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type teamPairingFixture struct {
	worker              *Service
	ctx                 context.Context
	teams               *team.Service
	root, session, plan string
	ids                 []string // Team A, ordinary Worker, Team B: intentional selected order.
}

// Every selected Task/Attempt is created through real Task and Team services.
// The deterministic HTTP endpoint below validates source orchestration only.
func newTeamPairingFixture(t *testing.T, ready bool, sameMember ...bool) *teamPairingFixture {
	t.Helper()
	w, ctx, ordinary, cleanup := prepareUnknownDispatch(t)
	t.Cleanup(cleanup)
	for _, query := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('pair-human','human','Operator','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','pair-human','active',1,1)`,
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node-local','Local',1,'pairing-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
		`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('pair-model','synthetic-pair','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('pair-deployment','pair-model','node-local','pair-http','{}','ready','pair-fp',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('pair-compat','pair-deployment','inference.general','L2',0,64000,'low','supported','{}','{}','pair-cp',1,1,1)`,
	} {
		if _, err := w.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	teams := team.NewService(w.db, w.tx, w.clock, w.tasks)
	w.tasks.SetAdmissionGuard(teams)
	root, err := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Pairing root"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Pairing", CreatedBy: "pair-human"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "A", RoleName: "A", CreatedBy: "pair-human"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "B", RoleName: "B", CreatedBy: "pair-human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: root.ID, TeamID: group.ID, CreatedBy: "pair-human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	secondMember := b.ID
	if len(sameMember) > 0 && sameMember[0] {
		secondMember = a.ID
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "a", MemberID: a.ID, Objective: "Pair A"}, {Key: "b", MemberID: secondMember, Objective: "Pair B"}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "pair-human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "pair-human"}); err != nil {
		t.Fatal(err)
	}
	f := &teamPairingFixture{worker: w, ctx: ctx, teams: teams, root: root.ID, session: session.ID, plan: plan.ID, ids: make([]string, 3)}
	if err = w.db.QueryRowContext(ctx, `SELECT task_id FROM team_execution_assignments WHERE session_id=? AND assignment_key='a'`, session.ID).Scan(&f.ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = w.db.QueryRowContext(ctx, `SELECT task_id FROM team_execution_assignments WHERE session_id=? AND assignment_key='b'`, session.ID).Scan(&f.ids[2]); err != nil {
		t.Fatal(err)
	}
	f.ids[1] = ordinary.TaskID
	if ready {
		// The base fixture's independent already-running Task is not rewritten as
		// never-started work. Park it and create a fresh genuine READY ordinary Task.
		if _, err = w.tasks.Pause(ctx, task.TransitionCommand{TaskID: ordinary.TaskID, ExpectedRevision: 3}); err != nil {
			t.Fatal(err)
		}
		if err = w.syncResumedRuns(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET status='waiting' WHERE id=?`, ordinary.ID); err != nil {
			t.Fatal(err)
		}
		fresh, e := w.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Ordinary Worker"})
		if e != nil {
			t.Fatal(e)
		}
		fresh, e = w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: fresh.ID, ExpectedRevision: fresh.Revision})
		if e != nil {
			t.Fatal(e)
		}
		f.ids[1] = fresh.ID
	}
	for _, index := range []int{0, 2} {
		child, e := w.tasks.Get(ctx, f.ids[index])
		if e != nil {
			t.Fatal(e)
		}
		child, e = w.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
		if e != nil {
			t.Fatal(e)
		}
		if !ready {
			if _, r := w.startRun(ctx, child); r.Error != "" {
				t.Fatal(r.Error)
			}
		}
	}
	for index, id := range f.ids {
		if ready {
			_, err = w.db.ExecContext(ctx, `UPDATE tasks SET ready_at=? WHERE id=?`, index+1, id)
		} else {
			_, err = w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET updated_at=? WHERE task_id=?`, index+1, id)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifact.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
	w.inference = inference.NewService(w.db, w.tx, w.clock)
	w.scheduler = scheduler.NewService(w.db, w.tx, w.clock)
	w.verification = verification.NewService(w.db, w.tx, w.clock)
	return f
}

type teamPairingProbe struct {
	mu                                 sync.Mutex
	calls                              map[string]int
	active, peak, teamActive, teamPeak int
	entered                            chan struct{}
	once                               sync.Once
	overlap                            bool
	observe                            func(agentprotocol.Request) error
}

func (p *teamPairingProbe) handler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req agentprotocol.Request
	for _, m := range body.Messages {
		if m.Role == "user" {
			if err := json.Unmarshal([]byte(m.Content), &req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}
	}
	isTeam := req.Objective == "Pair A" || req.Objective == "Pair B"
	p.mu.Lock()
	p.calls[req.TaskID]++
	p.active++
	if p.active > p.peak {
		p.peak = p.active
	}
	if isTeam {
		p.teamActive++
		if p.teamActive > p.teamPeak {
			p.teamPeak = p.teamActive
		}
		if p.teamActive == 2 {
			p.once.Do(func() { close(p.entered) })
		}
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.active--
		if isTeam {
			p.teamActive--
		}
		p.mu.Unlock()
	}()
	if p.observe != nil {
		if err := p.observe(req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if isTeam && p.overlap {
		select {
		case <-p.entered:
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
	raw, _ := json.Marshal(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalHuman, Message: "Synthetic paired model", Proposal: json.RawMessage(`{"question":"Review this contribution?"}`)})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}, "usage": map[string]any{"completion_tokens": 1}})
}

func (f *teamPairingFixture) transport(t *testing.T, p *teamPairingProbe, constrained bool) {
	t.Helper()
	p.calls = make(map[string]int)
	p.entered = make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(p.handler))
	t.Cleanup(server.Close)
	transports := inference.NewTransportRegistry()
	if err := transports.Register("pair-http", inference.LocalOpenAITransport{Resolver: responseJournalEndpoint(server.URL), Client: server.Client()}); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.inference.ConfigureExecution(f.worker.artifacts, transports, nil, "node-local"); err != nil {
		t.Fatal(err)
	}
	coordinator := runtimecoord.NewService(f.worker.db, f.worker.clock)
	if constrained {
		if err := f.worker.inference.ConfigureRuntimeCoordinator(coordinator); err != nil {
			t.Fatal(err)
		}
	} else {
		// Per-Task synthetic Node identities simulate independent capacity;
		// they do not establish actual multi-Node or physical model acceptance.
		if err := f.worker.inference.ConfigureRuntimeCoordinator(teamLaneCoordinator{inner: coordinator}); err != nil {
			t.Fatal(err)
		}
	}
}

// Baseline regression: the pre-fix Tick paired adjacent elements only, so the
// ordinary Worker between two accepted independent contributors forced serial
// inference. Both branches must still process that ordinary Worker exactly once.
func TestTeamPairingNonadjacentSelectedBatch(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprintf("ready_%v", ready), func(t *testing.T) {
			f := newTeamPairingFixture(t, ready)
			p := &teamPairingProbe{overlap: true}
			f.transport(t, p, false)
			results, err := f.worker.Tick(f.ctx, 3)
			if err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if len(results) != 3 || p.teamPeak != 2 || p.peak > 2 {
				t.Fatalf("selected Team inference did not overlap boundedly results=%+v calls=%v teamPeak=%d peak=%d", results, p.calls, p.teamPeak, p.peak)
			}
			for _, id := range f.ids {
				if p.calls[id] != 1 {
					t.Fatalf("selected ordinary/Team work duplicated or starved %s calls=%v", id, p.calls)
				}
			}
			seen := map[string]bool{}
			for _, r := range results {
				if seen[r.TaskID] || r.Status != "waiting_human" {
					t.Fatalf("duplicate or unsuccessful selected result %+v", results)
				}
				seen[r.TaskID] = true
			}
		})
	}
}

func TestTeamPairingPreservesSelectedLimitAndSchedulingTiers(t *testing.T) {
	for _, scenario := range []string{"limit_two", "priority_barrier", "class_barrier", "partner_priority", "partner_class"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTeamPairingFixture(t, false)
			limit := 3
			query := ""
			switch scenario {
			case "limit_two":
				limit = 2
			case "priority_barrier":
				query = `UPDATE tasks SET priority=1 WHERE id=?`
			case "class_barrier":
				query = `UPDATE tasks SET scheduling_class='user_interactive' WHERE id=?`
			case "partner_priority":
				query = `UPDATE tasks SET priority=1 WHERE id=?`
			case "partner_class":
				query = `UPDATE tasks SET scheduling_class='user_interactive' WHERE id=?`
			}
			if query != "" {
				id := f.ids[1]
				if scenario == "partner_priority" || scenario == "partner_class" {
					id = f.ids[2]
				}
				if _, err := f.worker.db.ExecContext(f.ctx, query, id); err != nil {
					t.Fatal(err)
				}
			}
			p := &teamPairingProbe{overlap: true}
			f.transport(t, p, false)
			results, err := f.worker.Tick(f.ctx, limit)
			if err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			if len(results) != limit || p.teamPeak != 1 || p.peak != 1 {
				t.Fatalf("selection limit/tier crossed %+v calls=%v peak=%d/%d", results, p.calls, p.teamPeak, p.peak)
			}
			for i := 0; i < limit; i++ {
				if results[i].TaskID != f.ids[i] || p.calls[f.ids[i]] != 1 {
					t.Fatalf("selected order changed %+v %v", results, p.calls)
				}
			}
			if limit == 2 && p.calls[f.ids[2]] != 0 {
				t.Fatal("unselected Team partner was dispatched")
			}
			p.mu.Unlock()
			if limit == 2 {
				// The next cursor opportunity still reaches the unselected member.
				more, err := f.worker.Tick(f.ctx, 1)
				if err != nil || len(more) != 1 || more[0].TaskID != f.ids[2] {
					t.Fatalf("ordinary bounded selection starved tail %+v %v", more, err)
				}
			}
		})
	}
}

func TestTeamPairingPreservesRemainingReadyOpportunityAfterActiveWork(t *testing.T) {
	f := newTeamPairingFixture(t, true)
	active, err := f.worker.tasks.Create(f.ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Active ordinary work"})
	if err != nil {
		t.Fatal(err)
	}
	active, err = f.worker.tasks.MarkReady(f.ctx, task.TransitionCommand{TaskID: active.ID, ExpectedRevision: active.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, result := f.worker.startRun(f.ctx, active); result.Error != "" {
		t.Fatal(result.Error)
	}
	p := &teamPairingProbe{overlap: true}
	f.transport(t, p, false)
	results, err := f.worker.Tick(f.ctx, 3)
	if err != nil || len(results) != 3 {
		t.Fatalf("mixed selected batch %+v %v", results, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, id := range []string{active.ID, f.ids[0], f.ids[1]} {
		if results[i].TaskID != id || p.calls[id] != 1 {
			t.Fatalf("remaining READY opportunity bypassed %+v calls=%v", results, p.calls)
		}
	}
	if p.calls[f.ids[2]] != 0 || p.teamPeak != 1 {
		t.Fatalf("unselected READY partner dispatched calls=%v peak=%d", p.calls, p.teamPeak)
	}
}

func TestTeamPairingHonorsSharedConstrainedCapacity(t *testing.T) {
	f := newTeamPairingFixture(t, false)
	p := &teamPairingProbe{}
	var once sync.Once
	var observeMu sync.Mutex
	var queueErr error
	p.observe = func(req agentprotocol.Request) error {
		if req.Objective != "Pair A" && req.Objective != "Pair B" {
			return nil
		}
		once.Do(func() {
			observeMu.Lock()
			defer observeMu.Unlock()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			for {
				var queued int
				if err := f.worker.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM runtime_execution_queue WHERE state='queued'`).Scan(&queued); err != nil {
					queueErr = err
					return
				}
				if queued == 1 {
					return
				}
				select {
				case <-timer.C:
					queueErr = fmt.Errorf("second selected Team member did not queue behind shared CPU pool")
					return
				case <-f.ctx.Done():
					queueErr = f.ctx.Err()
					return
				case <-time.After(time.Millisecond):
				}
			}
		})
		observeMu.Lock()
		defer observeMu.Unlock()
		return queueErr
	}
	f.transport(t, p, true)
	results, err := f.worker.Tick(f.ctx, 3)
	observeMu.Lock()
	defer observeMu.Unlock()
	if err != nil || queueErr != nil || len(results) != 3 {
		t.Fatalf("constrained pair %+v %v queue=%v", results, err, queueErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.teamPeak != 1 || p.peak != 1 {
		t.Fatalf("shared pool capacity bypassed peak=%d/%d", p.teamPeak, p.peak)
	}
	for _, id := range f.ids {
		if p.calls[id] != 1 {
			t.Fatalf("capacity queue lost selected work %v", p.calls)
		}
	}
	var live, completed int
	if err = f.worker.db.QueryRowContext(f.ctx, `SELECT SUM(CASE WHEN state IN ('queued','running') THEN 1 ELSE 0 END),SUM(CASE WHEN state='completed' THEN 1 ELSE 0 END) FROM runtime_execution_queue`).Scan(&live, &completed); err != nil || live != 0 || completed != 3 {
		t.Fatalf("stranded shared compute live=%d completed=%d err=%v", live, completed, err)
	}
}

func TestTeamPairingReturnedProposalsRespectTeamHoldAndOrdinaryProgress(t *testing.T) {
	f := newTeamPairingFixture(t, false)
	p := &teamPairingProbe{overlap: true}
	var once sync.Once
	var observeMu sync.Mutex
	var holdErr error
	p.observe = func(req agentprotocol.Request) error {
		if req.Objective != "Pair A" && req.Objective != "Pair B" {
			return nil
		}
		once.Do(func() {
			observeMu.Lock()
			defer observeMu.Unlock()
			root, err := f.worker.tasks.Get(f.ctx, f.root)
			if err != nil {
				holdErr = err
				return
			}
			_, holdErr = f.teams.PauseExecution(f.ctx, team.ExecutionControlCommand{SessionID: f.session, PlanID: f.plan, ActorPrincipalID: "pair-human", ExpectedTaskRevision: root.Revision, Reason: "Hold during paired source transport"})
		})
		observeMu.Lock()
		defer observeMu.Unlock()
		return holdErr
	}
	f.transport(t, p, false)
	results, err := f.worker.Tick(f.ctx, 3)
	observeMu.Lock()
	defer observeMu.Unlock()
	if err != nil || holdErr != nil || len(results) != 3 {
		t.Fatalf("held pair %+v %v hold=%v", results, err, holdErr)
	}
	var requests, holds int
	if err = f.worker.db.QueryRowContext(f.ctx, `SELECT (SELECT COUNT(*) FROM team_assistance_requests WHERE session_id=?),(SELECT COUNT(*) FROM team_execution_holds WHERE session_id=? AND released_at IS NULL)`, f.session, f.session).Scan(&requests, &holds); err != nil || requests != 0 || holds != 1 {
		t.Fatalf("returned proposal bypassed hold requests=%d holds=%d err=%v", requests, holds, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls[f.ids[1]] != 1 {
		t.Fatalf("Team hold starved selected ordinary Worker %v", p.calls)
	}
	for _, r := range results {
		if r.TaskID == f.ids[1] {
			if r.Status != "waiting_human" {
				t.Fatalf("independent ordinary result %+v", r)
			}
		} else if r.Status == "waiting_human" {
			t.Fatalf("held Team proposal applied %+v", r)
		}
	}
}

func TestTeamPairingRecoveryHaltStopsRemainingSelectedWork(t *testing.T) {
	f := newTeamPairingFixture(t, false)
	p := &teamPairingProbe{overlap: true}
	f.transport(t, p, false)
	if _, err := f.worker.db.ExecContext(f.ctx, `CREATE TRIGGER reject_pair_model_journal BEFORE INSERT ON agent_worker_steps WHEN NEW.step_kind='model' BEGIN SELECT RAISE(ABORT,'response and recovery journal rejected'); END`); err != nil {
		t.Fatal(err)
	}
	results, err := f.worker.Tick(f.ctx, 3)
	if !errors.Is(err, ErrDispatchRequiresReconciliation) || !f.worker.dispatchReconciliationFailed || len(results) != 2 {
		t.Fatalf("pair did not retain recovery halt %+v %v", results, err)
	}
	p.mu.Lock()
	if p.calls[f.ids[0]] != 1 || p.calls[f.ids[2]] != 1 || p.calls[f.ids[1]] != 0 {
		t.Fatalf("halt leaked to ordinary selected transport %v", p.calls)
	}
	p.mu.Unlock()
	if _, err = f.worker.Tick(f.ctx, 3); !errors.Is(err, ErrDispatchRequiresReconciliation) {
		t.Fatalf("halt replayed paired work %v", err)
	}
	var requests int
	if err = f.worker.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM team_assistance_requests WHERE session_id=?`, f.session).Scan(&requests); err != nil || requests != 0 {
		t.Fatalf("unjournalled paired proposal applied %d %v", requests, err)
	}
}

func TestTeamPairingHintsRejectDependencyDriftAndRecheckCurrentAuthority(t *testing.T) {
	f := newTeamPairingFixture(t, false)
	partner, err := f.worker.parallelTeamPartner(f.ctx, f.ids[0], []string{f.ids[1], f.ids[2]})
	if err != nil || partner != 1 {
		t.Fatalf("missing selected nonadjacent hint %d %v", partner, err)
	}
	if f.worker.parallelTeamPair(f.ctx, f.ids[0], f.ids[1]) {
		t.Fatal("ordinary Task treated as Team member")
	}
	if f.worker.parallelTeamPair(f.ctx, f.ids[0], f.ids[0]) {
		t.Fatal("same contribution paired twice")
	}
	if _, err = f.worker.db.ExecContext(f.ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{}')`, f.ids[2], f.ids[0]); err != nil {
		t.Fatal(err)
	}
	partner, err = f.worker.parallelTeamPartner(f.ctx, f.ids[0], []string{f.ids[1], f.ids[2]})
	if err != nil || partner != -1 || f.worker.parallelTeamPair(f.ctx, f.ids[0], f.ids[2]) {
		t.Fatalf("dependency drift paired %d %v", partner, err)
	}
	// A previously obtained hint is never an execution grant. Both production
	// preparation boundaries re-read the graph. A remains independently eligible,
	// while B's undeclared extra dependency must prevent B from reaching transport.
	p := &teamPairingProbe{}
	f.transport(t, p, false)
	results, err := f.worker.Tick(f.ctx, 3)
	if err != nil || len(results) != 3 {
		t.Fatalf("drift Tick %+v %v", results, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls[f.ids[0]] != 1 || p.calls[f.ids[2]] != 0 || p.calls[f.ids[1]] != 1 {
		t.Fatalf("cached pair hint bypassed admission or starved ordinary %v", p.calls)
	}
}

func TestTeamPairingHintsAreBoundedReadOnlyAndRequireDistinctMembers(t *testing.T) {
	f := newTeamPairingFixture(t, false, true)
	if _, err := f.worker.db.ExecContext(f.ctx, `PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	defer f.worker.db.ExecContext(f.ctx, `PRAGMA query_only=OFF`)
	partner, err := f.worker.parallelTeamPartner(f.ctx, f.ids[0], []string{f.ids[1], f.ids[2]})
	if err != nil || partner != -1 || f.worker.parallelTeamPair(f.ctx, f.ids[0], f.ids[2]) {
		t.Fatalf("same member paired %d %v", partner, err)
	}
	for _, ids := range [][]string{{f.ids[0]}, {f.ids[1], f.ids[1]}, {""}, {string([]byte{0xff})}, make([]string, 101)} {
		if _, err := f.worker.parallelTeamPartner(f.ctx, f.ids[0], ids); !errors.Is(err, ErrInvalidWorkerState) {
			t.Fatalf("invalid selected batch accepted %q: %v", ids, err)
		}
	}
	if partner, err := f.worker.parallelTeamPartner(f.ctx, f.ids[0], []string{"missing-task", f.ids[2]}); err != nil || partner != -1 {
		t.Fatalf("missing Task barrier crossed %d %v", partner, err)
	}
}
