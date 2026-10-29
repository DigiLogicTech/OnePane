package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func messagePageFixture(t *testing.T) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE team_sessions(id TEXT PRIMARY KEY,workspace_id TEXT,team_id TEXT,task_id TEXT,status TEXT,accepted_plan_id TEXT,gateway_target_id TEXT,round_number INTEGER,config_json TEXT,revision INTEGER,created_by TEXT,created_at INTEGER,updated_at INTEGER)`,
		`CREATE TABLE team_messages(id TEXT PRIMARY KEY,workspace_id TEXT,session_id TEXT,author_member_id TEXT,author_principal_id TEXT,message_kind TEXT,content_json TEXT,reply_to_message_id TEXT,round_number INTEGER,created_at INTEGER,message_sequence INTEGER)`,
		`INSERT INTO team_sessions VALUES('room','tenant','team','task','deliberating',NULL,NULL,1,'{}',1,'human',1,1),('foreign','elsewhere','other','other-task','deliberating',NULL,NULL,1,'{}',1,'other-human',1,1)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 261; i++ {
		raw, _ := json.Marshal(map[string]string{"text": fmt.Sprintf("message %d", i)})
		if _, err = db.Exec(`INSERT INTO team_messages VALUES(?,?,?,NULL,'human','human',?,NULL,1,?,?)`, fmt.Sprintf("m%04d", i), "tenant", "room", string(raw), 100+i/60, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO team_messages VALUES('secret','elsewhere','foreign',NULL,'other-human','human','{"text":"FOREIGN_CANARY"}',NULL,1,999,1)`); err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}
}

func TestTeamMessagePageLongRoomNewestAndEqualTimestampPaging(t *testing.T) {
	s := messagePageFixture(t)
	ctx := context.Background()
	recent, err := s.RecentMessages(ctx, "room", 200)
	if err != nil || len(recent) != 200 || recent[0].ID != "m0061" || recent[199].ID != "m0260" {
		t.Fatalf("worker lost current room context: %v %v", len(recent), err)
	}
	page, err := s.PageMessages(ctx, "room", MessagePageOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.FirstID != "m0211" || page.LastID != "m0260" || !page.HasMore {
		t.Fatalf("wrong latest window: %s %s more=%v", page.FirstID, page.LastID, page.HasMore)
	}
	seen := map[string]bool{}
	for {
		for _, m := range page.Messages {
			if seen[m.ID] {
				t.Fatalf("duplicate %s", m.ID)
			}
			seen[m.ID] = true
			if m.SessionID != "room" {
				t.Fatal("foreign message")
			}
		}
		if !page.HasMore {
			break
		}
		page, err = s.PageMessages(ctx, "room", MessagePageOptions{BeforeID: page.FirstID, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 261 {
		t.Fatalf("lost history: %d", len(seen))
	}
	page, err = s.PageMessages(ctx, "room", MessagePageOptions{AfterID: "m0000", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.FirstID != "m0001" || page.LastID != "m0050" || !page.HasMore {
		t.Fatalf("bad forward catch-up: %+v", page)
	}
	page, err = s.PageMessages(ctx, "room", MessagePageOptions{AfterID: "m0260"})
	if err != nil || len(page.Messages) != 0 || page.HasMore {
		t.Fatalf("empty tail: %+v %v", page, err)
	}
	if _, err = s.db.Exec(`INSERT INTO team_messages VALUES('late','tenant','room',NULL,'human','human','{"text":"clock moved backwards"}',NULL,1,1,262)`); err != nil {
		t.Fatal(err)
	}
	page, err = s.PageMessages(ctx, "room", MessagePageOptions{AfterID: "m0260"})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "late" || page.Messages[0].Sequence != 262 {
		t.Fatalf("clock regression hid a new message: %+v %v", page, err)
	}
}

func TestTeamMessagePageRejectsForeignCursorAndBoundsUntrustedContent(t *testing.T) {
	s := messagePageFixture(t)
	ctx := context.Background()
	for _, o := range []MessagePageOptions{{BeforeID: "secret"}, {AfterID: "secret"}, {BeforeID: "missing"}, {BeforeID: "m0000", AfterID: "m0001"}, {Limit: 51}, {Limit: -1}, {BeforeID: strings.Repeat("x", 129)}} {
		if _, err := s.PageMessages(ctx, "room", o); err != ErrInvalid {
			t.Fatalf("accepted %+v: %v", o, err)
		}
	}
	raw, _ := json.Marshal(map[string]string{"text": strings.Repeat("PRIVATE_OUTPUT", 2000)})
	if _, err := s.db.Exec(`UPDATE team_messages SET content_json=? WHERE id='m0260'`, string(raw)); err != nil {
		t.Fatal(err)
	}
	page, err := s.PageMessages(ctx, "room", MessagePageOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page.Messages[0].Content), `"omitted":true`) || strings.Contains(string(page.Messages[0].Content), "PRIVATE_OUTPUT") {
		t.Fatal("large output was leaked or silently clipped")
	}
	if _, err = s.PageMessages(ctx, "missing-room", MessagePageOptions{}); err != sql.ErrNoRows {
		t.Fatalf("unknown room: %v", err)
	}
}
