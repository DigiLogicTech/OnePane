package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func liveRoomFixture(t *testing.T) (*Service, *sqlitestore.DB, Session, Member) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/team.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.SQL().SetMaxOpenConns(1)
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
	job, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Build a deliverable"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db.SQL(), db, clock.Real{}, tasks)
	group, err := s.CreateTeam(ctx, CreateTeamCommand{WorkspaceID: "tenant", Name: "Builders", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.AddMember(ctx, AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Build", CreatedBy: "human", Config: json.RawMessage(`{"private":"PROFILE_CANARY"}`)})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := s.StartSession(ctx, StartSessionCommand{TaskID: job.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	return s, db, ss, one
}

func TestLiveTeamRoomUsesFrozenMembersAndOnlyObservedTurnMetadata(t *testing.T) {
	s, db, ss, one := liveRoomFixture(t)
	ctx := context.Background()
	turns, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatal("missing turn")
	}
	if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status='running',selected_candidate_id='local-model',selected_candidate_kind='model_deployment',error_text='PROVIDER_SECRET' WHERE id=?`, turns[0].ID); err != nil {
		t.Fatal(err)
	}
	// Editing the reusable roster cannot rewrite an in-flight room's identities.
	if _, err = db.SQL().Exec(`UPDATE team_members SET display_name='Replaced name' WHERE id=?`, one.ID); err != nil {
		t.Fatal(err)
	}
	room, err := s.LiveRoom(ctx, ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(room.Members) != 1 || room.Members[0].Name != "Builder" || room.Members[0].Status != "running" || room.Members[0].ModelID != "local-model" || room.TaskID != ss.TaskID {
		t.Fatalf("wrong room: %+v", room)
	}
	raw, _ := json.Marshal(room)
	if strings.Contains(string(raw), "PROFILE_CANARY") || strings.Contains(string(raw), "PROVIDER_SECRET") {
		t.Fatal("private context leaked into live metadata")
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatalf("additive migration not repeatable: %v", err)
	}
}

func TestTeamRoundAndReplyScopeRejectsForeignCausalReferences(t *testing.T) {
	s, db, ss, _ := liveRoomFixture(t)
	ctx := context.Background()
	if _, err := db.SQL().Exec(`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Other tenant','active',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "outsider"}); err != ErrInvalid {
		t.Fatalf("nonmember human queued team: %v", err)
	}
	if _, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{"foreign-member"}}); err != ErrInvalid {
		t.Fatalf("unknown target queued: %v", err)
	}
	otherTask, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Other room"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.StartSession(ctx, StartSessionCommand{TaskID: otherTask.ID, TeamID: ss.TeamID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.PostMessage(ctx, PostMessageCommand{SessionID: other.ID, AuthorPrincipalID: "human", Kind: "human", Content: json.RawMessage(`{"text":"Other room"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PostMessage(ctx, PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: "human", Kind: "human", ReplyToMessageID: &foreign.ID, Content: json.RawMessage(`{"text":"Forged reply"}`)}); err != ErrInvalid {
		t.Fatalf("foreign reply accepted: %v", err)
	}
	if _, err = s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", TriggerMessageID: &foreign.ID}); err != ErrInvalid {
		t.Fatalf("foreign trigger accepted: %v", err)
	}
	current, err := s.Session(ctx, ss.ID)
	if err != nil || current.RoundNumber != 0 {
		t.Fatalf("denials changed room: %+v %v", current, err)
	}
	if _, err = db.SQL().Exec(`UPDATE team_sessions SET status='plan_proposed' WHERE id=?`, ss.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	current, err = s.Session(ctx, ss.ID)
	if err != nil || current.Status != "deliberating" {
		t.Fatalf("requested discussion was left undispatchable: %+v %v", current, err)
	}
}
