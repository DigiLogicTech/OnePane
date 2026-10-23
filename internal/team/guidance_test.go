package team

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func TestExecutingTeamGuidanceIsScopedAndRevocable(t *testing.T) {
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	ids := acceptAssignments(t, s, ss, specs)
	post := func(text, target string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"text": text, "target_member_id": target})
		if _, err := s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Content: body}); err != nil {
			t.Fatal(err)
		}
	}
	post("Shared guidance", "")
	post("Builder guidance", specs[0].MemberID)
	post("Reviewer guidance", specs[1].MemberID)
	check := func(workspace, taskID string, expected ...string) {
		t.Helper()
		got, err := teamgate.Guidance(ctx, db.SQL(), workspace, taskID)
		if err != nil || len(got) != len(expected) {
			t.Fatalf("guidance for %s/%s: %+v, %v", workspace, taskID, got, err)
		}
		for i, text := range expected {
			if got[i].Text != text {
				t.Fatalf("guidance order/scope: %+v", got)
			}
		}
	}
	check("tenant", ids["build"], "Shared guidance", "Builder guidance")
	check("tenant", ids["review"], "Shared guidance", "Reviewer guidance")
	check("tenant", ss.TaskID, "Shared guidance")
	check("foreign", ids["build"])
	late, err := s.AddMember(ctx, AddMemberCommand{TeamID: ss.TeamID, MemberKind: "agent", DisplayName: "Late member", RoleName: "Helper", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"foreign-member", late.ID, 42, nil, []string{specs[0].MemberID}} {
		body, _ := json.Marshal(map[string]any{"text": "Invalid recipient", "target_member_id": value})
		if _, err := s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Content: body}); err != ErrInvalid {
			t.Fatalf("invalid or unfrozen recipient admitted (%v): %v", value, err)
		}
	}
	check("tenant", ids["build"], "Shared guidance", "Builder guidance")
	if _, err := s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Kind: "agent", Content: json.RawMessage(`{"text":"Forged instruction"}`)}); err != ErrSessionState {
		t.Fatalf("agent message admitted during execution: %v", err)
	}
	if _, err := db.SQL().Exec(`UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`); err != nil {
		t.Fatal(err)
	}
	check("tenant", ids["build"])
	if _, err := s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Content: json.RawMessage(`{"text":"Revoked"}`)}); err == nil {
		t.Fatal("revoked human posted guidance")
	}
}
