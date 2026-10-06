package routineworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectroutine"
	"github.com/DigiLogicTech/OnePane/internal/routine"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

const (
	AuthorityPrincipal = "system:routine-authority"
	WorkerPrincipal    = "system:routine-worker"
	VerifierPrincipal  = "system:routine-verifier"
)

type Service struct {
	db           *sql.DB
	tx           storage.Transactor
	tasks        *task.Service
	authority    *authority.Service
	executor     *projectroutine.Executor
	observations *observation.Service
	verification *verification.Service
	routines     *routine.Service
	events       event.Store
	ids          id.Generator
	clock        clock.Clock
}

type Result struct {
	OccurrenceID   string `json:"occurrence_id"`
	TaskID         string `json:"task_id"`
	InvocationID   string `json:"invocation_id,omitempty"`
	ObservationID  string `json:"observation_id,omitempty"`
	VerificationID string `json:"verification_id,omitempty"`
	CheckpointID   string `json:"checkpoint_id,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

type workItem struct {
	OccurrenceID string
	RoutineID    string
	TaskID       string
	WorkspaceID  string
	BindingID    string
	RuntimeID    string
}

func New(db *sql.DB, tx storage.Transactor, clk clock.Clock, tasks *task.Service, auth *authority.Service, executor *projectroutine.Executor, observations *observation.Service, verifications *verification.Service, routines *routine.Service) *Service {
	return &Service{db: db, tx: tx, tasks: tasks, authority: auth, executor: executor, observations: observations, verification: verifications, routines: routines, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) EnsureSystemPrincipals(ctx context.Context) error {
	if s == nil || s.db == nil || s.tx == nil || s.clock == nil {
		return errors.New("routine worker dependencies unavailable")
	}
	for _, p := range []struct{ id, name string }{{AuthorityPrincipal, "Routine Authority"}, {WorkerPrincipal, "Routine Worker"}, {VerifierPrincipal, "Routine Verifier"}} {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM principals WHERE id=?`, p.id).Scan(&status)
		if err == nil {
			if status != "active" {
				return fmt.Errorf("system principal %s is not active", p.id)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.clock.UnixMilli()
		pid := p.id
		if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?, 'system', ?, 'active', 1, ?, ?)`, p.id, p.name, now, now); err != nil {
				return err
			}
			eid, _ := s.ids.New("evt")
			payload, _ := json.Marshal(map[string]any{"principal_id": p.id, "principal_type": "system", "purpose": "routine_execution"})
			return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "system_principal.registered", AggregateType: "principal", AggregateID: p.id, ActorPrincipalID: &pid, Payload: payload, OccurredAt: now})
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureWorkspaceAccess(ctx context.Context, workspaceID string) error {
	if err := s.EnsureSystemPrincipals(ctx); err != nil {
		return err
	}
	for _, principal := range []string{AuthorityPrincipal, WorkerPrincipal, VerifierPrincipal} {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, workspaceID, principal).Scan(&status)
		if err == nil {
			if status != "active" {
				return fmt.Errorf("system membership %s/%s is %s", workspaceID, principal, status)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.clock.UnixMilli()
		actor := AuthorityPrincipal
		if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var wsStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, workspaceID).Scan(&wsStatus); err != nil {
				return err
			}
			if wsStatus != "active" {
				return fmt.Errorf("workspace %s is not active", workspaceID)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, workspaceID, principal, now, now); err != nil {
				return err
			}
			eid, _ := s.ids.New("evt")
			payload, _ := json.Marshal(map[string]any{"workspace_id": workspaceID, "principal_id": principal, "purpose": "routine_execution"})
			return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &workspaceID, Type: "workspace.system_membership_added", AggregateType: "workspace_membership", AggregateID: workspaceID + ":" + principal, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
		}); err != nil {
			return err
		}
	}
	return nil
}

// RecoverLostAttempts runs once after daemon bootstrap. A sandbox command may
// have produced an effect before the process disappeared, so these attempts are
// interrupted and blocked rather than retried automatically.
func (s *Service) RecoverLostAttempts(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.id,t.revision FROM tasks t JOIN task_attempts a ON a.task_id=t.id WHERE a.worker_principal_id=? AND a.status IN ('created','queued','running','waiting')`, WorkerPrincipal)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type x struct {
		id  string
		rev int64
	}
	var xs []x
	for rows.Next() {
		var v x
		if err := rows.Scan(&v.id, &v.rev); err != nil {
			return 0, err
		}
		xs = append(xs, v)
	}
	count := 0
	for _, v := range xs {
		actor := AuthorityPrincipal
		if _, err := s.tasks.InterruptForRecovery(ctx, task.TransitionCommand{TaskID: v.id, ExpectedRevision: v.rev, ActorPrincipalID: &actor, Reason: "routine_worker_restart_unknown_outcome"}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Service) pending(ctx context.Context, limit int) ([]workItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ro.id,ro.routine_id,ro.task_id,r.workspace_id,b.id,b.project_runtime_id
FROM routine_occurrences ro
JOIN routines r ON r.id=ro.routine_id
JOIN tasks t ON t.id=ro.task_id
JOIN project_routine_bindings b ON b.routine_id=ro.routine_id AND b.status='active'
WHERE ro.state='instantiated' AND t.state='created' AND b.action_kind='app_command'
ORDER BY ro.trigger_time_utc,ro.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workItem
	for rows.Next() {
		var w workItem
		if err := rows.Scan(&w.OccurrenceID, &w.RoutineID, &w.TaskID, &w.WorkspaceID, &w.BindingID, &w.RuntimeID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Service) Tick(ctx context.Context, limit int) ([]Result, error) {
	items, err := s.pending(ctx, limit)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(items))
	for _, item := range items {
		r := s.execute(ctx, item)
		results = append(results, r)
	}
	return results, nil
}

func (s *Service) execute(ctx context.Context, item workItem) Result {
	res := Result{OccurrenceID: item.OccurrenceID, TaskID: item.TaskID, Status: "failed"}
	if err := s.ensureWorkspaceAccess(ctx, item.WorkspaceID); err != nil {
		res.Error = err.Error()
		return res
	}
	t, err := s.tasks.Get(ctx, item.TaskID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	actor := AuthorityPrincipal
	t, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: t.ID, ExpectedRevision: t.Revision, ActorPrincipalID: &actor, Reason: "routine occurrence admitted"})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	worker := WorkerPrincipal
	t, attempt, err := s.tasks.Start(ctx, task.StartCommand{TaskID: t.ID, ExpectedRevision: t.Revision, WorkerPrincipalID: &worker, ActorPrincipalID: &actor, Metadata: json.RawMessage(`{"routine_worker":"v1"}`)})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	started := true
	fail := func(cause error) Result {
		res.Error = cause.Error()
		if started {
			if cur, e := s.tasks.Get(ctx, t.ID); e == nil && cur.State == task.StateRunning {
				_, _ = s.tasks.Fail(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &worker, Reason: "routine execution failed: " + bounded(cause.Error(), 512)})
			}
			_, _ = s.routines.SyncOccurrenceState(ctx, item.OccurrenceID)
		}
		return res
	}
	usage := int64(1)
	lease, err := s.authority.Issue(ctx, authority.IssueCommand{WorkspaceID: item.WorkspaceID, PrincipalID: WorkerPrincipal, TaskID: &t.ID, CapabilityID: sandboxrunner.CapabilityExecute, Scope: authority.Scope{ResourceRefs: []string{"project_runtime:" + item.RuntimeID}, Actions: []authority.ActionMode{authority.ActionExecuteSandboxed}}, IssuedBy: AuthorityPrincipal, ExpiresAt: s.clock.UnixMilli() + int64((5*time.Minute)/time.Millisecond), UsageLimit: &usage, ActorPrincipalID: &actor})
	if err != nil {
		return fail(err)
	}
	inv, err := s.executor.Execute(ctx, projectroutine.ExecuteCommand{BindingID: item.BindingID, TaskID: t.ID, AttemptID: &attempt.ID, PrincipalID: WorkerPrincipal, CapabilityLeaseID: lease.ID, ActorPrincipalID: &worker})
	if err != nil {
		return fail(err)
	}
	res.InvocationID = inv.ID
	adapterID, adapterVersion := inv.AdapterID, inv.AdapterVersion
	obs, err := s.observations.Record(ctx, observation.RecordCommand{WorkspaceID: item.WorkspaceID, SubjectRef: "routine_occurrence:" + item.OccurrenceID, ObservationType: "project_routine_execution", ProbeToolID: inv.ToolID, ProbeToolVersion: inv.ToolVersion, SourcePrincipalID: &worker, AdapterID: &adapterID, AdapterVersion: &adapterVersion, Value: inv.Result, Label: policy.DataLabel{WorkspaceID: item.WorkspaceID, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUnverifiedDerived}, ActorPrincipalID: &worker})
	if err != nil {
		return fail(err)
	}
	res.ObservationID = obs.ID
	if err := s.observations.VerifyIntegrity(ctx, obs.ID); err != nil {
		return fail(err)
	}
	cur, err := s.tasks.Get(ctx, t.ID)
	if err != nil {
		return fail(err)
	}
	cur, err = s.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &worker, Reason: "routine sandbox command returned successfully"})
	if err != nil {
		return fail(err)
	}
	verifier := VerifierPrincipal
	cur, err = s.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &verifier, Reason: "verify routine execution evidence"})
	if err != nil {
		return fail(err)
	}
	spec, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "integrity_hash": obs.IntegrityHash, "tool_invocation_id": inv.ID, "predicate": "successful sandbox command result"})
	v, err := s.verification.Create(ctx, verification.CreateCommand{WorkspaceID: item.WorkspaceID, TaskID: &cur.ID, SubjectRef: "observation:" + obs.ID, RequiredLevel: policy.VerificationV1, Spec: spec, ActorPrincipalID: &verifier})
	if err != nil {
		return fail(err)
	}
	level := policy.VerificationV1
	vresult, _ := json.Marshal(map[string]any{"observation_id": obs.ID, "integrity_verified": true, "tool_status": inv.Status})
	v, err = s.verification.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &level, Result: vresult, VerifiedBy: verifier, ActorPrincipalID: &verifier})
	if err != nil {
		return fail(err)
	}
	res.VerificationID = v.ID
	cpState, _ := json.Marshal(map[string]any{"occurrence_id": item.OccurrenceID, "tool_invocation_id": inv.ID, "observation_id": obs.ID, "verification_id": v.ID})
	cp, err := s.verification.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: item.WorkspaceID, TaskID: cur.ID, VerificationID: v.ID, State: cpState, ActorPrincipalID: &verifier})
	if err != nil {
		return fail(err)
	}
	res.CheckpointID = cp.ID
	cur, err = s.tasks.Get(ctx, cur.ID)
	if err != nil {
		return fail(err)
	}
	final, _ := json.Marshal(map[string]any{"routine_occurrence_id": item.OccurrenceID, "invocation_id": inv.ID, "observation_id": obs.ID, "verification_id": v.ID, "checkpoint_id": cp.ID})
	if _, err = s.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, CheckpointID: cp.ID, Result: final, ActorPrincipalID: &verifier}); err != nil {
		return fail(err)
	}
	_, _ = s.routines.SyncOccurrenceState(ctx, item.OccurrenceID)
	res.Status = "complete"
	return res
}

func bounded(v string, n int) string {
	v = strings.TrimSpace(v)
	if len(v) > n {
		return v[:n]
	}
	return v
}

// Keep compile-time use of tool package explicit: Routine execution remains a
// ToolGateway invocation rather than direct container-engine execution.
var _ tool.Status = tool.StatusSucceeded
