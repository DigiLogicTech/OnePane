//go:build integration

package verification

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestSQLiteVerificationCheckpointAndVerifiedTaskCompletion(t *testing.T) {
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
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('verifier','system','Verifier','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','verify','verifying','normal_task',0,'{}',7,?,?)`, []any{now, now}},
		{`INSERT INTO task_attempts(id,task_id,attempt_number,status,started_at,metadata_json) VALUES('attempt','task',1,'waiting',?,'{}')`, []any{now}},
	}
	for _, s := range stmts {
		if _, err := db.SQL().ExecContext(ctx, s.q, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	taskID := "task"
	v, err := svc.Create(ctx, CreateCommand{WorkspaceID: "ws", TaskID: &taskID, SubjectRef: "task:task", RequiredLevel: policy.VerificationV2, Spec: json.RawMessage(`{"kind":"integration"}`)})
	if err != nil {
		t.Fatal(err)
	}
	achieved := policy.VerificationV2
	v, err = svc.Resolve(ctx, ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: StatusPass, AchievedLevel: &achieved, Result: json.RawMessage(`{"observed":true}`), VerifiedBy: "verifier"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusPass {
		t.Fatalf("status=%s", v.Status)
	}
	cp, err := svc.CreateCheckpoint(ctx, CheckpointCommand{WorkspaceID: "ws", TaskID: "task", VerificationID: v.ID, State: json.RawMessage(`{"task_revision":7,"result":"verified"}`)})
	if err != nil {
		t.Fatal(err)
	}
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	completed, err := tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: "task", ExpectedRevision: 7, CheckpointID: cp.ID, Result: json.RawMessage(`{"done":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.StateComplete {
		t.Fatalf("task state=%s", completed.State)
	}
	var attemptState string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id='attempt'`).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if attemptState != "succeeded" {
		t.Fatalf("attempt state=%s", attemptState)
	}
}

func TestCheckpointRejectsUnknownMutationOutcome(t *testing.T) {
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
	seed := []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('verifier','system','Verifier','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','verify','verifying','normal_task',0,'{}',1,1,1)`,
		`INSERT INTO operations(id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,resource_ref,tool_id,tool_version,adapter_id,adapter_version,desired_state_json,precondition_json,reconciliation_json,policy_revision,input_hash,revision,created_at,updated_at) VALUES('op','ws','task','agent','k','unknown_outcome','c','r','t','1','a','1','{}','{}','{}',1,'h',1,1,1)`,
	}
	_ = now
	for _, q := range seed {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	taskID := "task"
	v, err := svc.Create(ctx, CreateCommand{WorkspaceID: "ws", TaskID: &taskID, SubjectRef: "task:task", RequiredLevel: policy.VerificationV1})
	if err != nil {
		t.Fatal(err)
	}
	level := policy.VerificationV1
	v, err = svc.Resolve(ctx, ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: StatusPass, AchievedLevel: &level, Result: json.RawMessage(`{}`), VerifiedBy: "verifier"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCheckpoint(ctx, CheckpointCommand{WorkspaceID: "ws", TaskID: "task", VerificationID: v.ID, State: json.RawMessage(`{}`)}); err != ErrUnknownMutationOutcome {
		t.Fatalf("err=%v", err)
	}
}
