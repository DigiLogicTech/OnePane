package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// SettleUnstartedCancellations is a trusted maintenance boundary, not a model
// tool or general recovery API. Only pristine cancellation graphs qualify:
// there must be no historical Attempt, dispatch, reservation, effect or result.
// Previously executed work needs human reconciliation and is left untouched.
// Each scan is bounded and rotates past ineligible work; the process-local
// cursor is only an optimization. Durable state and audit own idempotency.
func (s *Service) SettleUnstartedCancellations(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 16 {
		return 0, ErrInvalidCommand
	}
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks
	 WHERE state='cancel_requested' AND id>? ORDER BY id LIMIT ?`, s.cancelCursor, limit)
	if err != nil {
		return 0, err
	}
	var candidates []string
	for rows.Next() {
		var taskID string
		if err = rows.Scan(&taskID); err != nil {
			break
		}
		candidates = append(candidates, taskID)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if len(candidates) == 0 {
		s.cancelCursor = ""
		return 0, nil
	}
	settled := 0
	remaining := 256 // Whole-pass graph budget, not 256 per candidate.
	for _, taskID := range candidates {
		if remaining == 0 {
			break
		}
		changed := 0
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var err error
			changed, err = s.settleUnstartedGraph(ctx, tx, taskID, &remaining)
			return err
		})
		// Even an audit/DB error must not permanently starve independent work.
		// Failed transactions retain the request and are retried next rotation.
		s.cancelCursor = taskID
		if err != nil {
			return settled, fmt.Errorf("settle unstarted cancellation %s: %w", taskID, err)
		}
		settled += changed
	}
	return settled, nil
}

func (s *Service) settleUnstartedGraph(ctx context.Context, tx storage.Tx, rootID string, remaining *int) (int, error) {
	root, err := s.repo.GetForUpdate(ctx, tx, rootID)
	if err != nil {
		return 0, err
	}
	if root.State != StateCancelRequested {
		return 0, nil
	}
	// UNION deduplicates cycles; LIMIT detects oversized/truncated graphs.
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE owned(id) AS (
	 SELECT ? UNION SELECT t.id FROM tasks t JOIN owned o ON t.parent_task_id=o.id LIMIT ?
	) SELECT t.id FROM tasks t JOIN owned o ON o.id=t.id ORDER BY t.id`, rootID, *remaining+1)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var taskID string
		if err = rows.Scan(&taskID); err != nil {
			break
		}
		ids = append(ids, taskID)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if len(ids) > *remaining {
		*remaining = 0
		return 0, nil
	}
	*remaining -= len(ids)
	var pending []Task
	for _, taskID := range ids {
		t, err := s.repo.GetForUpdate(ctx, tx, taskID)
		if err != nil {
			return 0, err
		}
		if t.WorkspaceID != root.WorkspaceID || !sameOptionalID(t.ProjectID, root.ProjectID) ||
			!sameOptionalID(t.ProjectWorkspaceID, root.ProjectWorkspaceID) ||
			!sameOptionalID(t.ArtifactSessionID, root.ArtifactSessionID) ||
			(t.State != StateCancelRequested && t.State != StateCancelled) || len(t.Result) != 0 {
			return 0, nil
		}
		pristine, err := cancellationPristine(ctx, tx, t.ID)
		if err != nil {
			return 0, err
		}
		if !pristine {
			return 0, nil
		}
		// A cancellation must originate from a known unstarted state. Missing
		// history or a request from running/recovery cannot be guessed safe.
		var intent bool
		revision := t.Revision
		if t.State == StateCancelled {
			revision--
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events
		 WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.cancel_requested'
		 AND json_extract(payload_json,'$.revision')=?
		 AND json_extract(payload_json,'$.from') IN ('created','ready'))`, t.ID, revision).Scan(&intent); err != nil {
			return 0, err
		}
		if !intent {
			return 0, nil
		}
		if t.State == StateCancelRequested {
			if t.CancelRequestedAt == nil {
				return 0, nil
			}
			pending = append(pending, t)
		}
	}
	for _, t := range pending {
		now := s.clock.UnixMilli()
		if err := s.repo.Transition(ctx, tx, transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision,
			From: StateCancelRequested, To: StateCancelled, UpdatedAt: now}); err != nil {
			return 0, err
		}
		eventID, err := s.ids.New("evt")
		if err != nil {
			return 0, err
		}
		payload, _ := json.Marshal(map[string]any{"task_id": t.ID, "from": StateCancelRequested,
			"to": StateCancelled, "revision": t.Revision + 1, "reason": "never_started_no_execution_evidence", "root_task_id": rootID})
		if err := s.events.Append(ctx, tx, event.Event{ID: eventID, WorkspaceID: &t.WorkspaceID,
			Type: "task.cancelled", AggregateType: "task", AggregateID: t.ID, Payload: payload, OccurredAt: now}); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
}

func sameOptionalID(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// No status filter is intentional: a terminal historical row can still carry
// effects. Never infer safety from the absence of a *current* Attempt. Even
// proposals, expired grants and failed requests are conservatively deferred.
func cancellationPristine(ctx context.Context, tx storage.Tx, taskID string) (bool, error) {
	for _, table := range []string{
		"task_attempts", "agent_worker_runs", "operations", "tool_invocations",
		"inference_sessions", "inference_requests", "agent_runtime_invocations",
		"capability_leases", "resource_leases", "budget_reservations",
		"checkpoints", "verifications", "recovery_snapshots", "continuation_capsules",
		"assurance_runs", "workspace_file_publications", "runtime_execution_queue",
		"node_power_holds", "node_wake_attempts", "edit_proposals", "project_change_proposals",
		// Team reasoning is owned through the session, not Task Attempts.
		// Its lifecycle must settle the root after propagating graph intent.
		"team_sessions",
	} {
		var exists bool
		// Identifiers are a fixed source-owned allowlist, never caller input.
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE task_id=?)`, taskID).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}
	var globalLease bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM global_resource_leases
	 WHERE json_extract(owner_json,'$.task_id')=?)`, taskID).Scan(&globalLease); err != nil {
		return false, err
	}
	if globalLease {
		return false, nil
	}
	var started bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE aggregate_type='task'
	 AND aggregate_id=? AND event_type IN ('task.started','task.attempt_interrupted','task.completed'))`, taskID).Scan(&started); err != nil {
		return false, err
	}
	return !started, nil
}
