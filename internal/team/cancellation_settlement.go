package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// SettleUnstartedCancellations is a trusted bounded owner, distinct from room
// projection. Executed/unknown graphs remain pending for human reconciliation.
func (s *Service) SettleUnstartedCancellations(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || s.tasks == nil || limit < 1 || limit > 16 {
		return 0, ErrInvalid
	}
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT t.id FROM tasks t JOIN team_sessions ss ON ss.task_id=t.id WHERE t.state='cancel_requested' AND t.id>? ORDER BY t.id LIMIT ?`, s.cancelCursor, limit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
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
	if len(ids) == 0 {
		s.cancelCursor = ""
		return 0, nil
	}
	settled, budget := 0, 256
	for _, id := range ids {
		if budget <= 0 {
			break
		}
		changed := false
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var e error
			changed, e = s.settleCancellation(ctx, tx, id, &budget)
			return e
		})
		s.cancelCursor = id
		if err != nil {
			return settled, fmt.Errorf("settle Team cancellation %s: %w", id, err)
		}
		if changed {
			settled++
		}
	}
	return settled, nil
}

func (s *Service) settleCancellation(ctx context.Context, tx storage.Tx, rootID string, budget *int) (bool, error) {
	var session, plan, ws, teamID, status, manifest, manifestSHA, completion, rootHash string
	var taskRevision, sessionRevision int64
	err := tx.QueryRowContext(ctx, `SELECT ss.id,ss.accepted_plan_id,ss.workspace_id,ss.team_id,ss.status,ss.revision,t.revision,m.snapshot_json,m.snapshot_sha256,t.completion_json,a.completion_sha256
 FROM tasks t JOIN team_sessions ss ON ss.task_id=t.id AND ss.workspace_id=t.workspace_id
 JOIN team_assignment_roots a ON a.session_id=ss.id AND a.plan_id=ss.accepted_plan_id AND a.task_id=t.id
 JOIN team_plans p ON p.id=a.plan_id AND p.session_id=ss.id AND p.workspace_id=ss.workspace_id AND p.status='accepted'
 JOIN team_session_manifests m ON m.session_id=ss.id AND m.workspace_id=ss.workspace_id AND m.team_id=ss.team_id AND m.execution_mode='team' AND m.research_mode=0
 JOIN task_execution_profiles ep ON ep.task_id=t.id AND ep.workspace_id=ss.workspace_id AND ep.team_id=ss.team_id AND ep.team_session_id=ss.id AND ep.execution_mode='team'
 WHERE t.id=? AND t.state='cancel_requested' AND t.archived_at IS NULL AND t.cancel_requested_at IS NOT NULL AND t.result_json IS NULL`, rootID).Scan(&session, &plan, &ws, &teamID, &status, &sessionRevision, &taskRevision, &manifest, &manifestSHA, &completion, &rootHash)
	if err != nil {
		return false, cancellationMissing(err)
	}
	if status != "plan_accepted" && status != "executing" && status != "team_review" {
		return false, nil
	}
	hash := sha256.Sum256([]byte(manifest))
	if hex.EncodeToString(hash[:]) != manifestSHA {
		return false, nil
	}
	hash = sha256.Sum256([]byte(completion))
	if hex.EncodeToString(hash[:]) != rootHash {
		return false, nil
	}
	var snapshot SessionSnapshot
	if json.Unmarshal([]byte(manifest), &snapshot) != nil || snapshot.SessionID != session || snapshot.TaskID != rootID || snapshot.ExecutionMode != "team" || snapshot.TeamID != teamID || snapshot.ResearchMode {
		return false, nil
	}
	var intent bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE aggregate_type='team_session' AND aggregate_id=? AND workspace_id=? AND event_type='team.execution_cancellation_requested' AND json_extract(payload_json,'$.task_id')=? AND json_extract(payload_json,'$.task_revision')=? AND json_extract(payload_json,'$.plan_id')=? AND json_extract(payload_json,'$.scope')='task_and_descendants')`, session, ws, rootID, taskRevision, plan).Scan(&intent)
	if err != nil || !intent {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE owned(id) AS (SELECT ? UNION SELECT t.id FROM tasks t JOIN owned o ON t.parent_task_id=o.id LIMIT ?) SELECT t.id,t.workspace_id IS root.workspace_id AND t.project_id IS root.project_id AND t.project_workspace_id IS root.project_workspace_id AND t.artifact_session_id IS root.artifact_session_id FROM tasks t JOIN owned o ON o.id=t.id JOIN tasks root ON root.id=?`, rootID, *budget+1, rootID)
	if err != nil {
		return false, err
	}
	var graph []string
	scopeOK := true
	for rows.Next() {
		var id string
		var same bool
		if err = rows.Scan(&id, &same); err != nil {
			break
		}
		graph = append(graph, id)
		scopeOK = scopeOK && same
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	if len(graph) > *budget {
		*budget = 0
		return false, nil
	}
	*budget -= len(graph)
	if !scopeOK {
		return false, nil
	}
	var badHold bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_execution_holds h
 LEFT JOIN team_execution_held_tasks held ON held.hold_id=h.id
 LEFT JOIN tasks t ON t.id=held.task_id
 WHERE h.session_id=? AND h.released_at IS NULL AND
 (h.plan_id IS NOT ? OR h.root_task_id IS NOT ? OR t.id IS NULL OR
 (t.id<>? AND t.parent_task_id IS NOT ?) OR t.state NOT IN ('cancel_requested','cancelled')))`, session, plan, rootID, rootID, rootID).Scan(&badHold)
	if err != nil || badHold {
		return false, err
	}
	// Every descendant must already carry the ordinary owner's full pristine
	// settlement evidence. Completed contributions are preserved and deferred.
	for _, id := range graph {
		if id == rootID {
			continue
		}
		ok, e := s.tasks.PristineCancelledInTransaction(ctx, tx, id)
		if e != nil || !ok {
			return false, e
		}
	}
	identityOK, err := cancellationGraphIdentity(ctx, tx, rootID, session, plan, ws, snapshot, graph)
	if err != nil || !identityOK {
		return false, err
	}
	clean, err := teamCancellationRootPristine(ctx, tx, rootID)
	if err != nil || !clean {
		return false, err
	}
	clean, err = s.cancellationReasoning(ctx, tx, rootID, session, ws, manifestSHA)
	if err != nil || !clean {
		return false, err
	}
	now := s.clock.UnixMilli()
	r, err := tx.ExecContext(ctx, `UPDATE team_turn_requests SET status='cancelled',completed_at=?,updated_at=? WHERE session_id=? AND workspace_id=? AND status='pending' AND selected_candidate_id IS NULL AND selected_candidate_kind IS NULL AND budget_reservation_id IS NULL AND response_message_id IS NULL AND completed_at IS NULL AND attempt_count=0`, now, now, session, ws)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		if err = s.emit(ctx, tx, ws, "team.pending_turns_cancelled", "team_session", session, nil, map[string]any{"task_id": rootID, "plan_id": plan, "task_revision": taskRevision, "count": n}); err != nil {
			return false, err
		}
	}
	if err = s.tasks.CompleteUnstartedTeamCancellationInTransaction(ctx, tx, task.TransitionCommand{TaskID: rootID, ExpectedRevision: taskRevision}, session, plan); err != nil {
		return false, err
	}
	// End scheduling bookkeeping only after every held Task is proven terminal
	// in this same graph transaction. This never resumes work or releases an
	// inference/resource lease.
	r, err = tx.ExecContext(ctx, `UPDATE team_execution_holds SET released_at=? WHERE session_id=? AND plan_id=? AND root_task_id=? AND released_at IS NULL`, now, session, plan, rootID)
	if err != nil {
		return false, err
	}
	closedHolds, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	if closedHolds > 0 {
		if err = s.emit(ctx, tx, ws, "team.execution_hold_closed", "team_session", session, nil, map[string]any{"task_id": rootID, "plan_id": plan, "reason": "terminal_cancellation", "count": closedHolds}); err != nil {
			return false, err
		}
	}
	r, err = tx.ExecContext(ctx, `UPDATE team_sessions SET status='cancelled',revision=revision+1,updated_at=? WHERE id=? AND status=? AND revision=? AND accepted_plan_id=?`, now, session, status, sessionRevision, plan)
	if err != nil {
		return false, err
	}
	n, err = r.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, ErrSessionState
	}
	if err = s.emit(ctx, tx, ws, "team.execution_cancelled", "team_session", session, nil, map[string]any{"task_id": rootID, "plan_id": plan, "task_revision": taskRevision + 1, "reason": "local_reasoning_settled_no_execution"}); err != nil {
		return false, err
	}
	return true, nil
}
