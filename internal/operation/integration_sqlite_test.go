//go:build integration

package operation

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
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type mutationAdapter struct{ calls int }

func (a *mutationAdapter) ID() string      { return "test.mutation.adapter" }
func (a *mutationAdapter) Version() string { return "1" }
func (a *mutationAdapter) Invoke(_ context.Context, req tool.AdapterRequest) (tool.AdapterResult, error) {
	a.calls++
	return tool.AdapterResult{Summary: "test external state accepted request", Result: json.RawMessage(`{"request_id":"external-1","accepted":true}`)}, nil
}

func TestSQLiteOperationCoordinatorMutationLifecycle(t *testing.T) {
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
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO system_state(singleton_id,mode,revision,reason,updated_at) VALUES(1,'normal',1,'test',?)`, []any{now}},
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('verifier','human','Verifier','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','verifier','active',?,?)`, []any{now, now}},
		{`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','mutate','running','normal_task',0,'{}',1,?,?)`, []any{now, now}},
		{`INSERT INTO task_attempts(id,task_id,attempt_number,worker_principal_id,status,started_at,metadata_json) VALUES('attempt','task',1,'agent','running',?,'{}')`, []any{now}},
	}
	for _, x := range seed {
		if _, err := db.SQL().ExecContext(ctx, x.q, x.args...); err != nil {
			t.Fatal(err)
		}
	}

	auth := authority.NewService(db.SQL(), db, clock.Real{})
	taskID := "task"
	limit := int64(1)
	lease, err := auth.Issue(ctx, authority.IssueCommand{WorkspaceID: "ws", PrincipalID: "agent", TaskID: &taskID, CapabilityID: "test.resource.set", Scope: authority.Scope{ResourceRefs: []string{"resource://demo"}, Actions: []authority.ActionMode{authority.ActionMutate}}, IssuedBy: "admin", ExpiresAt: now + 120000, UsageLimit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	sys := system.NewService(db.SQL(), db, clock.Real{})
	pol := policy.NewEngine(auth, sys, clock.Real{})
	reg := tool.NewRegistry()
	adapter := &mutationAdapter{}
	if err := reg.Register(tool.Definition{ID: "test.resource.set", Version: "1", CapabilityID: "test.resource.set", Mode: authority.ActionMutate, AdapterID: adapter.ID(), AdapterVersion: adapter.Version(), Risk: policy.RiskMedium, MinimumVerification: policy.VerificationV2, MinimumApproval: policy.ApprovalNone}, adapter); err != nil {
		t.Fatal(err)
	}
	gateway := tool.NewGateway(db.SQL(), db, pol, auth, reg, clock.Real{})
	gate := NewMutationGate(db.SQL())
	gateway.SetMutationPermitValidator(gate)
	coord := NewCoordinator(db.SQL(), db, pol, gateway, gate, clock.Real{})
	attemptID := "attempt"
	input := json.RawMessage(`{"value":"new"}`)
	op, err := coord.Prepare(ctx, PrepareCommand{WorkspaceID: "ws", TaskID: &taskID, AttemptID: &attemptID, PrincipalID: "agent", CapabilityLeaseID: lease.ID, IdempotencyKey: "idem-1", ToolID: "test.resource.set", ToolVersion: "1", ResourceRef: "resource://demo", Input: input, DesiredState: json.RawMessage(`{"value":"new"}`), Precondition: json.RawMessage(`{"value":"old"}`), Reconciliation: json.RawMessage(`{"probe":"resource.get"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != StatePrepared || op.ResourceLeaseID == nil {
		t.Fatalf("prepared=%+v", op)
	}
	before, _ := auth.Get(ctx, lease.ID)
	if before.UsageCount != 0 {
		t.Fatalf("prepare consumed authority usage=%d", before.UsageCount)
	}

	// Evidence that predates mutation dispatch must not survive the execution
	// boundary as valid completion evidence.
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO verifications(id,workspace_id,task_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision) VALUES('pre-ver','ws','task','task:task','V1','V1','pass','{}','{}','verifier',?,?,1)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO checkpoints(id,workspace_id,task_id,verification_id,state_json,status,created_at) VALUES('pre-cp','ws','task','pre-ver','{}','valid',?)`, now); err != nil {
		t.Fatal(err)
	}

	// Even with a valid operation ID, bypassing the coordinator lacks the ephemeral permit.
	_, directErr := gateway.Invoke(ctx, tool.InvokeCommand{WorkspaceID: "ws", TaskID: &taskID, AttemptID: &attemptID, PrincipalID: "agent", LeaseID: lease.ID, OperationID: &op.ID, ToolID: "test.resource.set", ToolVersion: "1", ResourceRef: "resource://demo", Input: input})
	if directErr == nil || adapter.calls != 0 {
		t.Fatalf("direct mutation escaped gate err=%v calls=%d", directErr, adapter.calls)
	}

	op, err = coord.Execute(ctx, ExecuteCommand{OperationID: op.ID, ExpectedRevision: op.Revision, LeaseID: lease.ID, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != StateObserving || adapter.calls != 1 {
		t.Fatalf("after execute=%+v calls=%d", op, adapter.calls)
	}
	var checkpointStatus string
	var checkpointInvalidatedAt *int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,invalidated_at FROM checkpoints WHERE id='pre-cp'`).Scan(&checkpointStatus, &checkpointInvalidatedAt); err != nil {
		t.Fatal(err)
	}
	if checkpointStatus != "stale" || checkpointInvalidatedAt == nil {
		t.Fatalf("pre-mutation checkpoint remained usable: status=%s invalidated_at=%v", checkpointStatus, checkpointInvalidatedAt)
	}
	after, _ := auth.Get(ctx, lease.ID)
	if after.UsageCount != 1 {
		t.Fatalf("execute usage=%d", after.UsageCount)
	}

	ver := verification.NewService(db.SQL(), db, clock.Real{})
	v, err := ver.Create(ctx, verification.CreateCommand{WorkspaceID: "ws", OperationID: &op.ID, SubjectRef: "resource://demo", RequiredLevel: policy.VerificationV2, Spec: json.RawMessage(`{"probe":"independent"}`)})
	if err != nil {
		t.Fatal(err)
	}
	ach := policy.VerificationV2
	v, err = ver.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &ach, Result: json.RawMessage(`{"observed":{"value":"new"}}`), VerifiedBy: "verifier"})
	if err != nil {
		t.Fatal(err)
	}
	op, err = coord.CommitVerified(ctx, CommitCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != StateCommitted {
		t.Fatalf("state=%s", op.State)
	}
	var status string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM resource_leases WHERE id=?`, *op.ResourceLeaseID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "released" {
		t.Fatalf("resource lease=%s", status)
	}
}
