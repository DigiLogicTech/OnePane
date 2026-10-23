package team

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestReasoningTaskCancellationBetweenReadAndWrite(t *testing.T) {
	for _, action := range []string{"round", "message"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, specs := assignmentFixture(t)
			var eventCount int
			if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			s.clock = syncBoundaryClock{beforeApply: func() {
				once.Do(func() {
					if _, err := db.SQL().Exec(`UPDATE tasks SET state='cancel_requested',revision=revision+1 WHERE id=?`, ss.TaskID); err != nil {
						t.Fatal(err)
					}
				})
			}}
			var err error
			if action == "round" {
				_, err = s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}})
			} else {
				_, err = s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Kind: "human", Content: json.RawMessage(`{"text":"late"}`)})
			}
			if err == nil {
				t.Fatal("stale read wrote after cancellation")
			}
			got, e := s.Session(ctx, ss.ID)
			if e != nil || got.RoundNumber != ss.RoundNumber || got.Revision != ss.Revision || got.Status != ss.Status {
				t.Fatalf("session changed: %+v %v", got, e)
			}
			for _, table := range []string{"team_turn_requests", "team_messages"} {
				var n int
				if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE session_id=?`, ss.ID).Scan(&n); e != nil || n != 0 {
					t.Fatalf("%s wrote rows: %d %v", table, n, e)
				}
			}
			var after int
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after); e != nil || after != eventCount {
				t.Fatalf("event count changed: %d want %d %v", after, eventCount, e)
			}
		})
	}
}

func TestReasoningTaskCancellationFencesAllAuthorsAndRounds(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	for _, q := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('system:team-worker','system','Worker','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','system:team-worker','active',1,1)`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"cancel_requested", "cancelling", "compensating", "cancelled", "complete", "failed", "blocked"} {
		t.Run(state, func(t *testing.T) {
			if _, err := db.SQL().Exec(`UPDATE tasks SET state=? WHERE id=?`, state, ss.TaskID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}}); err == nil {
				t.Fatal("round admitted after Task lost authority")
			}
			for _, kind := range []string{"human", "agent", "supervisor", "system"} {
				actor := "system:team-worker"
				var member *string
				if kind == "human" {
					actor = "human"
				} else {
					member = &specs[0].MemberID
				}
				if _, err := s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: actor, AuthorMemberID: member, Kind: kind, Content: json.RawMessage(`{"text":"late response"}`)}); err == nil {
					t.Fatalf("%s message admitted", kind)
				}
			}
		})
	}
	if _, err := db.SQL().Exec(`UPDATE tasks SET state='paused' WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}}); err != nil {
		t.Fatalf("paused deliberation rejected: %v", err)
	}
}

func TestCompleteReasoningTurnAtomicOwnershipAndCancellation(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	for _, q := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('system:team-worker','system','Worker','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','system:team-worker','active',1,1)`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	turns, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}})
	if err != nil {
		t.Fatal(err)
	}
	turn := turns[0]
	if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status='running' WHERE id=?`, turn.ID); err != nil {
		t.Fatal(err)
	}
	complete := func() (Message, error) {
		return s.CompleteReasoningTurn(ctx, ss.ID, turn.ID, turn.MemberID, "system:team-worker", turn.RoundNumber, json.RawMessage(`{"text":"response"}`))
	}
	count := func() int {
		var n int
		if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM team_messages WHERE session_id=?`, ss.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if _, err = db.SQL().Exec(`UPDATE tasks SET state='cancel_requested' WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = complete(); err == nil {
		t.Fatal("cancelled Task published response")
	}
	if count() != before {
		t.Fatal("rejected response left message")
	}
	if _, err = db.SQL().Exec(`UPDATE tasks SET state='paused' WHERE id=?`, ss.TaskID); err != nil {
		t.Fatal(err)
	}
	for _, revocation := range []struct {
		query, restore string
		id             string
	}{
		{`UPDATE team_members SET status='disabled' WHERE id=?`, `UPDATE team_members SET status='active' WHERE id=?`, turn.MemberID},
		{`UPDATE workspaces SET status='suspended' WHERE id=?`, `UPDATE workspaces SET status='active' WHERE id=?`, ss.WorkspaceID},
	} {
		if _, err = db.SQL().Exec(revocation.query, revocation.id); err != nil {
			t.Fatal(err)
		}
		if _, err = complete(); err == nil {
			t.Fatal("revoked authority published response")
		}
		if count() != before {
			t.Fatal("revoked response left message")
		}
		if _, err = db.SQL().Exec(revocation.restore, revocation.id); err != nil {
			t.Fatal(err)
		}
	}
	// A ledger failure must roll back BOTH the transcript and turn status.
	if _, err = db.SQL().Exec(`CREATE TRIGGER fail_reasoning_audit BEFORE INSERT ON events WHEN NEW.event_type='team.message_posted' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = complete(); err == nil {
		t.Fatal("audit failure ignored")
	}
	if count() != before {
		t.Fatal("audit failure left message")
	}
	var status string
	if err = db.SQL().QueryRow(`SELECT status FROM team_turn_requests WHERE id=?`, turn.ID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("rollback status %s %v", status, err)
	}
	if _, err = db.SQL().Exec(`DROP TRIGGER fail_reasoning_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err = complete(); err != nil {
		t.Fatal(err)
	}
	if _, err = complete(); err == nil {
		t.Fatal("lost owner reported success")
	}
	if count() != before+1 {
		t.Fatal("duplicate response")
	}
}
