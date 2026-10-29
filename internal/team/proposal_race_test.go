package team

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// Interleave a committed competing operation after ProposePlan's initial
// reads but immediately before its real SQLite transaction begins.
type proposalBoundaryTransactor struct {
	storage.Transactor
	before func()
}

func (p *proposalBoundaryTransactor) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	if before := p.before; before != nil {
		p.before = nil
		before()
	}
	return p.Transactor.Within(ctx, fn)
}

func TestProposePlanRejectsCommittedChangesBeforeTransaction(t *testing.T) {
	for _, change := range []string{"task_cancel", "session_cancel", "accepted_plan", "revision", "membership", "principal", "workspace", "member", "accepted_root"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, specs := assignmentFixture(t)
			body, err := json.Marshal(map[string]any{"assignments": specs})
			if err != nil {
				t.Fatal(err)
			}
			original, err := s.ProposePlan(ctx, ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body})
			if err != nil {
				t.Fatal(err)
			}
			command := ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body}
			// Bind an agent to the existing human principal for the explicit
			// member authorization path; then revoke it at the boundary.
			if change == "member" {
				if _, err = db.SQL().Exec(`UPDATE team_members SET principal_id='human' WHERE id=?`, specs[0].MemberID); err != nil {
					t.Fatal(err)
				}
				command.ProposedByMemberID = &specs[0].MemberID
			}
			var expected Session
			var expectedEvents int
			s.tx = &proposalBoundaryTransactor{Transactor: s.tx, before: func() {
				var query string
				var args []any
				switch change {
				case "task_cancel":
					root, e := s.tasks.Get(ctx, ss.TaskID)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = s.tasks.RequestCancel(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision}); e != nil {
						t.Fatal(e)
					}
				case "accepted_plan", "accepted_root":
					if _, e := s.AcceptPlan(ctx, AcceptPlanCommand{SessionID: ss.ID, PlanID: original.ID, AcceptedByPrincipalID: "human"}); e != nil {
						t.Fatal(e)
					}
					if change == "accepted_root" {
						// Even an inconsistent legacy deliberating row must not
						// permit replacement of an already accepted task graph.
						query = `UPDATE team_sessions SET status='plan_proposed',accepted_plan_id=NULL,revision=revision-1 WHERE id=?`
						args = []any{ss.ID}
					}
				case "session_cancel":
					query = `UPDATE team_sessions SET status='cancelled',revision=revision+1 WHERE id=?`
					args = []any{ss.ID}
				case "revision":
					query = `UPDATE team_sessions SET revision=revision+1 WHERE id=?`
					args = []any{ss.ID}
				case "membership":
					query = `DELETE FROM workspace_memberships WHERE principal_id='human' AND workspace_id='tenant'`
				case "principal":
					query = `UPDATE principals SET status='disabled' WHERE id='human'`
				case "workspace":
					query = `UPDATE workspaces SET status='archived' WHERE id='tenant'`
				case "member":
					query = `UPDATE team_members SET status='disabled' WHERE id=?`
					args = []any{specs[0].MemberID}
				}
				if query != "" {
					if _, e := db.SQL().Exec(query, args...); e != nil {
						t.Fatal(e)
					}
				}
				var e error
				expected, e = s.Session(ctx, ss.ID)
				if e != nil {
					t.Fatal(e)
				}
				if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&expectedEvents); e != nil {
					t.Fatal(e)
				}
			}}
			proposal, err := s.ProposePlan(ctx, command)
			if !errors.Is(err, ErrSessionState) && !errors.Is(err, ErrInvalid) {
				t.Fatalf("proposal after %s: %+v %v", change, proposal, err)
			}
			if proposal.ID != "" {
				t.Fatalf("rejected proposal returned a saved identity: %+v", proposal)
			}
			got, err := s.Session(ctx, ss.ID)
			if err != nil || got.Status != expected.Status || got.Revision != expected.Revision {
				t.Fatalf("competing session overwritten: %+v want %+v; %v", got, expected, err)
			}
			var count, events int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_plans WHERE session_id=?`, ss.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("late proposal persisted: %d %v", count, err)
			}
			var status string
			wantStatus := "candidate"
			if change == "accepted_plan" || change == "accepted_root" {
				wantStatus = "accepted"
			}
			if err = db.SQL().QueryRow(`SELECT status FROM team_plans WHERE id=?`, original.ID).Scan(&status); err != nil || status != wantStatus {
				t.Fatalf("original plan mutated: %s want %s; %v", status, wantStatus, err)
			}
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil || events != expectedEvents {
				t.Fatalf("late proposal emitted events: %d want %d; %v", events, expectedEvents, err)
			}
		})
	}
}
