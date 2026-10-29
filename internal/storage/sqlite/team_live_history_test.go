package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/migrations"
)

func TestTeamLiveSequenceUpgradePreservesHistoryAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE team_messages(id TEXT PRIMARY KEY,session_id TEXT,content_json TEXT,created_at INTEGER)`,
		`CREATE TABLE team_turn_requests(session_id TEXT,workspace_id TEXT,member_id TEXT,round_number INTEGER,created_at INTEGER,id TEXT)`,
		`INSERT INTO team_messages VALUES('original-a','room','{"text":"preserve α"}',200),('original-b','room','{"text":"preserve β"}',100)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, q); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	body, err := migrations.FS.ReadFile("0096_team_live_history.sql")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.SQL().ExecContext(ctx, string(body)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.SQL().ExecContext(ctx, `INSERT INTO team_messages(id,session_id,content_json,created_at) VALUES('after-restart','room','{"text":"new after clock rollback"}',1)`); err != nil {
		t.Fatal(err)
	}
	var seq int64
	var content string
	if err = db.SQL().QueryRowContext(ctx, `SELECT message_sequence,content_json FROM team_messages WHERE id='original-a'`).Scan(&seq, &content); err != nil || seq != 1 || content != `{"text":"preserve α"}` {
		t.Fatalf("history changed: %d %s %v", seq, content, err)
	}
	if err = db.SQL().QueryRowContext(ctx, `SELECT message_sequence FROM team_messages WHERE id='after-restart'`).Scan(&seq); err != nil || seq != 3 {
		t.Fatalf("durable insertion order lost: %d %v", seq, err)
	}
	if _, err = db.SQL().ExecContext(ctx, `UPDATE team_messages SET message_sequence=99 WHERE id='original-a'`); err == nil {
		t.Fatal("existing sequence was mutable")
	}
	if _, err = db.SQL().ExecContext(ctx, `INSERT INTO team_messages(id,session_id,content_json,created_at,message_sequence) VALUES('forged','room','{}',1,999)`); err == nil {
		t.Fatal("caller replaced storage sequence")
	}
}
