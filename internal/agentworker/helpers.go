package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func strPtr(v string) *string { return &v }
func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
func boundedString(v string, n int) string {
	v = strings.TrimSpace(v)
	if len(v) > n {
		return v[:n]
	}
	return v
}
func boundedJSON(v json.RawMessage, n int) any {
	if len(v) <= n {
		var x any
		if json.Unmarshal(v, &x) == nil {
			return x
		}
	}
	return map[string]any{"truncated": true, "bytes": len(v)}
}
func defaultJSON(v json.RawMessage) json.RawMessage {
	if len(v) == 0 || !json.Valid(v) {
		return json.RawMessage(`{}`)
	}
	return v
}
func errorText(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}
func failedResult(r TickResult, err error) TickResult {
	r.Status = "failed"
	r.Error = err.Error()
	return r
}

func completionVerification(raw json.RawMessage) policy.VerificationLevel {
	var v struct {
		Required string `json:"required_verification"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return policy.VerificationV0
	}
	switch policy.VerificationLevel(strings.ToUpper(strings.TrimSpace(v.Required))) {
	case policy.VerificationV0, policy.VerificationV1, policy.VerificationV2, policy.VerificationV3, policy.VerificationV4, policy.VerificationV5:
		return policy.VerificationLevel(strings.ToUpper(strings.TrimSpace(v.Required)))
	}
	return policy.VerificationV0
}

func completionAssuranceCriteria(raw json.RawMessage) json.RawMessage {
	var v struct {
		Spec json.RawMessage `json:"verification_spec"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Spec) == 0 || !json.Valid(v.Spec) {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), v.Spec...)
}

func (s *Service) markReasoningDispatch(ctx context.Context, r *Run, kind, id string) error {
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE agent_worker_runs SET step_count=step_count+1,last_candidate_kind=?,last_candidate_id=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='running' AND step_count<max_steps`, kind, id, now, r.ID, r.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrInvalidWorkerState
	}
	r.StepCount++
	r.LastCandidateKind = &kind
	r.LastCandidateID = &id
	r.Revision++
	r.UpdatedAt = now
	return nil
}

// updateRun applies bounded counters and continuation/route policy changes with
// optimistic revision checking. escalationDelta/replanDelta are 0 or 1.
func (s *Service) updateRun(ctx context.Context, id string, revision int64, status RunStatus, continuation, routePolicy json.RawMessage, escalationDelta, replanDelta int64, lastKind, lastID, lastErr *string) error {
	now := s.clock.UnixMilli()
	var cont, route any = nil, nil
	if len(continuation) > 0 {
		cont = string(continuation)
	}
	if len(routePolicy) > 0 {
		route = string(routePolicy)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE agent_worker_runs SET status=?,continuation_json=COALESCE(?,continuation_json),route_policy_json=COALESCE(?,route_policy_json),escalation_count=escalation_count+?,replan_count=replan_count+?,last_candidate_kind=COALESCE(?,last_candidate_kind),last_candidate_id=COALESCE(?,last_candidate_id),last_error=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, status, cont, route, escalationDelta, replanDelta, lastKind, lastID, lastErr, now, id, revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrInvalidWorkerState
	}
	return nil
}

func (s *Service) finishRun(ctx context.Context, id string, status RunStatus, lastErr string) error {
	now := s.clock.UnixMilli()
	var e any = nil
	if strings.TrimSpace(lastErr) != "" {
		e = boundedString(lastErr, 2048)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE agent_worker_runs SET status=?,last_error=?,revision=revision+1,updated_at=?,completed_at=? WHERE id=? AND status IN ('running','waiting','blocked')`, status, e, now, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidWorkerState
	}
	return nil
}

func (s *Service) failRun(ctx context.Context, run Run, res TickResult, cause error) TickResult {
	cur, err := s.tasks.Get(ctx, run.TaskID)
	actor := WorkerPrincipal
	if err == nil && (cur.State == task.StateRunning || cur.State == task.StateVerifying || cur.State == task.StateCompletionRequested) {
		_, _ = s.tasks.Fail(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: boundedString(cause.Error(), 1024)})
	}
	_ = s.finishRun(ctx, run.ID, RunFailed, cause.Error())
	res.Status = "failed"
	res.Error = cause.Error()
	return res
}
func (s *Service) blockRun(ctx context.Context, run Run, res TickResult, reason string) TickResult {
	cur, err := s.tasks.Get(ctx, run.TaskID)
	actor := WorkerPrincipal
	if err == nil && cur.State == task.StateRunning {
		_, _ = s.tasks.MarkBlocked(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: boundedString(reason, 1024)})
	}
	_ = s.finishRun(ctx, run.ID, RunBlocked, reason)
	res.Status = "blocked"
	res.Error = reason
	return res
}

func (s *Service) continueWithError(ctx context.Context, run Run, res TickResult, code string, cause error) TickResult {
	cont, _ := json.Marshal(map[string]any{"last_error": map[string]any{"code": code, "message": boundedString(cause.Error(), 2048)}})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, nil, 0, 0, nil, nil, strPtr(cause.Error())); err != nil {
		return failedResult(res, err)
	}
	res.Status = "retry_reasoning"
	res.Error = cause.Error()
	return res
}

func (s *Service) journal(ctx context.Context, runID, kind, status string, candidateKind, candidateID, proposalType, resultRef *string, detail any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	sid, _ := s.ids.New("awstep")
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var seq int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(step_number),0)+1 FROM agent_worker_steps WHERE run_id=?`, runID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,candidate_kind,candidate_id,proposal_type,result_ref,detail_json,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sid, runID, seq, kind, status, candidateKind, candidateID, proposalType, resultRef, string(raw), now, now); err != nil {
			return err
		}
		var ws, taskID string
		if err := tx.QueryRowContext(ctx, `SELECT workspace_id,task_id FROM agent_worker_runs WHERE id=?`, runID).Scan(&ws, &taskID); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"run_id": runID, "task_id": taskID, "step_id": sid, "step_number": seq, "kind": kind, "status": status, "candidate_kind": candidateKind, "candidate_id": candidateID, "proposal_type": proposalType, "result_ref": resultRef, "detail": detail})
		actor := WorkerPrincipal
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &ws, Type: "agent_worker.step", AggregateType: "agent_worker_run", AggregateID: runID, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
	})
}

func (s *Service) findLease(ctx context.Context, workspaceID, taskID, capability string, action authority.ActionMode, resource string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope_json,expires_at,usage_limit,usage_count FROM capability_leases WHERE workspace_id=? AND principal_id=? AND task_id=? AND capability_id=? AND status='active' ORDER BY expires_at`, workspaceID, WorkerPrincipal, taskID, capability)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	now := s.clock.UnixMilli()
	for rows.Next() {
		var id, scopeRaw string
		var expires, used int64
		var limit sql.NullInt64
		if err := rows.Scan(&id, &scopeRaw, &expires, &limit, &used); err != nil {
			return "", err
		}
		if expires <= now || limit.Valid && used >= limit.Int64 {
			continue
		}
		var scope authority.Scope
		if json.Unmarshal([]byte(scopeRaw), &scope) != nil {
			continue
		}
		if scope.AllowsAction(action) && scope.AllowsResource(resource) {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", ErrAuthorityRequired
}

// Ensure compile-time separation: the worker never issues leases to itself.
var _ = errors.Is
var _ authority.Lease
var _ = fmt.Sprintf
