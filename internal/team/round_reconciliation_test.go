package team

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReasoningTurnRoundCannotSupersedeRunningHistory(t *testing.T) {
	for _, scenario := range []string{"current", "historical", "claimed_after_read", "completed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, db, ss, specs := assignmentFixture(t)
			cmd := RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}}
			turns, err := s.RequestRound(ctx, cmd)
			if err != nil {
				t.Fatal(err)
			}
			turn := turns[0]
			if scenario != "claimed_after_read" {
				status := "running"
				if scenario == "completed" {
					status = "succeeded"
				}
				if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status=? WHERE id=?`, status, turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "historical" {
				if _, err = db.SQL().Exec(`UPDATE team_sessions SET round_number=round_number+1,revision=revision+1 WHERE id=?`, ss.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.Session(ctx, ss.ID)
			if err != nil {
				t.Fatal(err)
			}
			var audits int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if scenario == "claimed_after_read" {
				s.tx = &proposalBoundaryTransactor{Transactor: s.tx, before: func() {
					if _, e := db.SQL().Exec(`UPDATE team_turn_requests SET status='running' WHERE id=?`, turn.ID); e != nil {
						t.Fatal(e)
					}
				}}
			}
			_, err = s.RequestRound(ctx, cmd)
			if scenario == "completed" {
				if err != nil {
					t.Fatalf("completed deliberation blocked next round: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrSessionState) {
				t.Fatalf("running history was superseded: %v", err)
			}
			after, e := s.Session(ctx, ss.ID)
			if e != nil {
				t.Fatal(e)
			}
			if after.Revision != before.Revision || after.RoundNumber != before.RoundNumber || after.Status != before.Status {
				t.Fatalf("rejected round changed session: before=%+v after=%+v", before, after)
			}
			var count, afterAudits int
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=?`, ss.ID).Scan(&count); e != nil || count != 1 {
				t.Fatalf("rejected round inserted turn: %d %v", count, e)
			}
			if e = db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&afterAudits); e != nil || afterAudits != audits {
				t.Fatalf("rejected round changed audits: %d want %d %v", afterAudits, audits, e)
			}
		})
	}
}

func TestReasoningTurnHistoryUsesBoundedIndexSearch(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	turns, err := s.RequestRound(context.Background(), RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<512)
 INSERT INTO team_turn_requests(id,workspace_id,session_id,member_id,status,round_number,created_at,updated_at)
 SELECT 'completed-'||i,workspace_id,session_id,member_id,'succeeded',i,1,1 FROM n CROSS JOIN team_turn_requests WHERE id=?`, turns[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, err := db.SQL().Query(`EXPLAIN QUERY PLAN `+reasoningRunningTurnHistorySQL, ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "SEARCH team_turn_requests USING COVERING INDEX idx_team_turn_requests_session_status (session_id=? AND status=?)") || strings.Contains(joined, "SCAN team_turn_requests") {
		t.Fatalf("running history probe is not a keyed search: %s", joined)
	}
}
