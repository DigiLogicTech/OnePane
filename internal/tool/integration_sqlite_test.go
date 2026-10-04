//go:build integration

package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/system"
)

func TestSQLiteGatewaySyntheticReadLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().UnixMilli()
	stmts := []string{
		`INSERT INTO system_state(singleton_id,mode,revision,reason,updated_at) VALUES(1,'normal',1,'test',?)`,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','tool test','running','normal_task',0,'{}',1,?,?)`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,worker_principal_id,status,started_at,metadata_json) VALUES('attempt','task',1,'agent','running',?,'{}')`,
	}
	args := [][]any{{now}, {now, now}, {now, now}, {now, now}, {now, now}, {now, now}, {now, now}, {now}}
	for i, stmt := range stmts {
		if _, err := db.SQL().ExecContext(ctx, stmt, args[i]...); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	auth := authority.NewService(db.SQL(), db, clock.Real{})
	taskID := "task"
	limit := int64(2)
	lease, err := auth.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws", PrincipalID: "agent", TaskID: &taskID, CapabilityID: SyntheticEchoCapability,
		Scope:    authority.Scope{ResourceRefs: []string{"synthetic://demo"}, Actions: []authority.ActionMode{authority.ActionRead}},
		IssuedBy: "admin", ExpiresAt: now + 60000, UsageLimit: &limit,
	})
	if err != nil {
		t.Fatal(err)
	}

	sys := system.NewService(db.SQL(), db, clock.Real{})
	pol := policy.NewEngine(auth, sys, clock.Real{})
	reg := NewRegistry()
	if err := RegisterSynthetic(reg); err != nil {
		t.Fatal(err)
	}
	gateway := NewGateway(db.SQL(), db, pol, auth, reg, clock.Real{})
	attemptID := "attempt"
	inv, err := gateway.Invoke(ctx, InvokeCommand{
		WorkspaceID: "ws", TaskID: &taskID, AttemptID: &attemptID, PrincipalID: "agent", LeaseID: lease.ID,
		ToolID: SyntheticEchoToolID, ToolVersion: SyntheticEchoToolVersion, ResourceRef: "synthetic://demo",
		Input: json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != StatusSucceeded {
		t.Fatalf("status=%s", inv.Status)
	}

	used, err := auth.Get(ctx, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if used.UsageCount != 1 {
		t.Fatalf("usage=%d", used.UsageCount)
	}
	var eventCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE aggregate_type='tool_invocation' AND aggregate_id=?`, inv.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 4 {
		t.Fatalf("tool events=%d want 4", eventCount)
	}

	denied, err := gateway.Invoke(ctx, InvokeCommand{
		WorkspaceID: "ws", TaskID: &taskID, AttemptID: &attemptID, PrincipalID: "agent", LeaseID: used.ID,
		ToolID: SyntheticEchoToolID, ToolVersion: SyntheticEchoToolVersion, ResourceRef: "synthetic://outside",
		Input: json.RawMessage(`{"message":"denied"}`),
	})
	if err == nil {
		t.Fatal("expected policy denial")
	}
	if denied.Status != StatusFailed || denied.ErrorCode == nil || *denied.ErrorCode != "policy_denied" {
		t.Fatalf("denied=%+v", denied)
	}
	after, err := auth.Get(ctx, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UsageCount != 1 {
		t.Fatalf("denied call consumed lease: usage=%d", after.UsageCount)
	}
}
