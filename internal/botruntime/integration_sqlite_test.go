//go:build integration

package botruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteHostedBotLifecycleDoesNotFakeInAppDispatch(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/onepane.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','human','active',?,?)`, []any{now, now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(db.SQL(), db, clock.Real{}, nil)
	conn, err := svc.CreateConnection(ctx, CreateConnectionCommand{
		WorkspaceID: "ws",
		PresetID:    "chatgpt-gpt",
		DisplayName: "ChatGPT GPTs",
		CreatedBy:   "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.AccessMode != "hosted_surface" {
		t.Fatalf("access mode=%q", conn.AccessMode)
	}

	launch := "https://chatgpt.com/g/g-example"
	bot, err := svc.CreateBot(ctx, CreateBotCommand{
		WorkspaceID:  "ws",
		ConnectionID: conn.ID,
		RemoteBotID:  "g-example",
		DisplayName:  "Example GPT",
		LaunchURL:    &launch,
		CreatedBy:    "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bot.LaunchURL == nil || *bot.LaunchURL != launch {
		t.Fatalf("launch url=%v", bot.LaunchURL)
	}

	session, err := svc.CreateSession(ctx, CreateSessionCommand{
		BotID:     bot.ID,
		Title:     "Bot Chat",
		Canonical: true,
		CreatedBy: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.RemoteSessionID != nil {
		t.Fatalf("hosted bot unexpectedly acquired remote session id %q", *session.RemoteSessionID)
	}

	result, err := svc.SendMessage(ctx, SendMessageCommand{
		SessionID:      session.ID,
		Text:           "hello",
		IdempotencyKey: "hosted-do-not-dispatch",
		CreatedBy:      "human",
	})
	if !errors.Is(err, ErrHostedSurface) {
		t.Fatalf("send err=%v", err)
	}
	if result.LaunchURL == nil || *result.LaunchURL != launch {
		t.Fatalf("send launch url=%v", result.LaunchURL)
	}

	var turns int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_turns WHERE session_id=?`, session.ID).Scan(&turns); err != nil {
		t.Fatal(err)
	}
	if turns != 0 {
		t.Fatalf("hosted-only bot created %d local dispatch turns", turns)
	}

	canonicalAgain, err := svc.CreateSession(ctx, CreateSessionCommand{
		BotID:     bot.ID,
		Title:     "ignored",
		Canonical: true,
		CreatedBy: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if canonicalAgain.ID != session.ID {
		t.Fatalf("canonical session not reused: %s != %s", canonicalAgain.ID, session.ID)
	}
}

func TestSQLiteHostedBotRejectsCrossWorkspaceRegistration(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/onepane.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws-a','A','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws-b','B','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws-a','human','active',?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws-b','human','active',?,?)`, []any{now, now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(db.SQL(), db, clock.Real{}, nil)
	conn, err := svc.CreateConnection(ctx, CreateConnectionCommand{WorkspaceID: "ws-a", PresetID: "chatgpt-gpt", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	launch := "https://chatgpt.com/g/g-example"
	_, err = svc.CreateBot(ctx, CreateBotCommand{
		WorkspaceID:  "ws-b",
		ConnectionID: conn.ID,
		RemoteBotID:  "g-example",
		DisplayName:  "Wrong workspace",
		LaunchURL:    &launch,
		CreatedBy:    "human",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-workspace registration err=%v", err)
	}
}
