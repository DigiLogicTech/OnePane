package team

import (
	"context"
	"fmt"
	"testing"
)

func TestManualCouncilTaskFences(t *testing.T) {
	for _, operation := range []string{"web_queue", "web_restart", "web_submit", "chair_queue", "chair_existing", "chair_submit", "chair_approve"} {
		t.Run(operation, func(t *testing.T) {
			s, db, ss, member := liveRoomFixture(t)
			ctx := context.Background()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.SQL().ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`UPDATE tasks SET state='paused' WHERE id=?`, ss.TaskID)
			exec(`UPDATE team_sessions SET status='deliberating',round_number=1 WHERE id=?`, ss.ID)
			exec(`UPDATE team_session_manifests SET execution_mode='council',research_mode=1,
			 snapshot_json=json_set(snapshot_json,'$.research.chair_mode','manual','$.research.chair_member_id',?,'$.research.chair_require_approval',json('true')) WHERE session_id=?`, member.ID, ss.ID)
			exec(`INSERT INTO team_turn_requests(id,workspace_id,session_id,member_id,status,round_number,created_at,updated_at)
			 VALUES('manual-fence-turn',?,?,?,'running',1,1,1)`, ss.WorkspaceID, ss.ID, member.ID)
			web := QueueManualWebTurnCommand{TurnID: "manual-fence-turn", SessionID: ss.ID, WorkspaceID: ss.WorkspaceID, MemberID: member.ID, ProviderID: "chatgpt", ModelLabel: "manual", Prompt: "Reviewed prompt"}
			chair := QueueManualWebChairCommand{SessionID: ss.ID, WorkspaceID: ss.WorkspaceID, MemberID: member.ID, ProviderID: "chatgpt", ModelLabel: "manual", Stage: "review", AfterRound: 1, Prompt: "Reviewed chair prompt"}
			if operation != "web_queue" {
				if _, err := s.QueueManualWebTurn(ctx, web); err != nil {
					t.Fatal(err)
				}
			}
			var chairID string
			if operation != "chair_queue" {
				queued, err := s.QueueManualWebChair(ctx, chair)
				if err != nil {
					t.Fatal(err)
				}
				chairID = queued.ID
			}
			if operation == "chair_approve" {
				if _, err := s.SubmitManualWebChair(ctx, SubmitManualWebChairCommand{ID: chairID, Actor: "human", Response: "Chair proposal"}); err != nil {
					t.Fatal(err)
				}
			}
			apply := func() error {
				switch operation {
				case "web_queue":
					_, err := s.QueueManualWebTurn(ctx, web)
					return err
				case "web_restart":
					_, err := s.RestartManualWebConversation(ctx, web.TurnID, "human")
					return err
				case "web_submit":
					_, err := s.SubmitManualWebResponse(ctx, SubmitManualWebResponseCommand{TurnID: web.TurnID, SubmittedBy: "human", ResponseText: "Manual answer", ConversationGeneration: 1})
					return err
				case "chair_queue", "chair_existing":
					_, err := s.QueueManualWebChair(ctx, chair)
					return err
				case "chair_submit":
					_, err := s.SubmitManualWebChair(ctx, SubmitManualWebChairCommand{ID: chairID, Actor: "human", Response: "Chair proposal"})
					return err
				default:
					_, err := s.ApproveManualWebChair(ctx, ApproveManualWebChairCommand{ID: chairID, Actor: "human", ApprovedText: "Approved proposal"})
					return err
				}
			}
			// Capture durable outcome and audit together so a rejected call cannot
			// leave a message, changed generation, success, or approval behind.
			snapshot := func() string {
				t.Helper()
				var events, messages int
				var turns, handoffs, chairs string
				if err := db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM team_messages),
				 (SELECT json_group_array(json_array(status,response_message_id)) FROM team_turn_requests),
				 (SELECT json_group_array(json_array(status,conversation_generation)) FROM manual_web_council_turns),
				 (SELECT json_group_array(json_array(status,response_text,approved_text)) FROM manual_web_council_chair_turns)`).Scan(&events, &messages, &turns, &handoffs, &chairs); err != nil {
					t.Fatal(err)
				}
				return fmt.Sprintf("%d/%d/%s/%s/%s", events, messages, turns, handoffs, chairs)
			}
			for _, state := range []string{"cancel_requested", "cancelling", "compensating", "cancelled", "failed", "complete", "blocked", "archived"} {
				actual, archived := state, any(nil)
				if state == "archived" {
					actual, archived = "paused", int64(1)
				}
				exec(`UPDATE tasks SET state=?,archived_at=? WHERE id=?`, actual, archived, ss.TaskID)
				before := snapshot()
				if err := apply(); err != ErrSessionState {
					t.Fatalf("%s: expected task fence, got %v", state, err)
				}
				if after := snapshot(); after != before {
					t.Fatalf("%s: rejected operation changed durable state: %s -> %s", state, before, after)
				}
			}
			exec(`UPDATE tasks SET state='paused',archived_at=NULL WHERE id=?`, ss.TaskID)
			if err := apply(); err != nil {
				t.Fatalf("legitimate paused deliberation rejected: %v", err)
			}
		})
	}
}
