package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// RecoveryCaptureResult counts durable intake only. Acknowledged jobs have a
// captured case; neither acknowledgement nor case creation reconciles effects.
type RecoveryCaptureResult struct{ Scanned, Captured, Acknowledged, Deferred int }

// CaptureRecoveryCases is trusted control-plane maintenance, never a model
// tool. Each bounded job is bound to its original interrupted Attempt and audit
// record in the acknowledgement transaction. Malformed/foreign histories remain
// pending and rotate out of the way of independent valid work.
func (s *Service) CaptureRecoveryCases(ctx context.Context, limit int) (RecoveryCaptureResult, error) {
	var out RecoveryCaptureResult
	if limit <= 0 || limit > 16 {
		limit = 16
	}
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	now := s.clock.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM outbox_jobs WHERE job_type='task.recovery.required' AND status IN ('pending','retry') AND available_at<=? AND id>? ORDER BY id LIMIT ?`, now, s.recoveryCursor, limit)
	if err != nil {
		return out, err
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
		return out, err
	}
	if closeErr != nil {
		return out, closeErr
	}
	if len(ids) == 0 {
		s.recoveryCursor = ""
	}
	for _, jobID := range ids {
		var captured, acknowledged bool
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var ws sql.NullString
			var raw string
			e := tx.QueryRowContext(ctx, `SELECT workspace_id,CASE WHEN length(payload_json)<=65536 THEN payload_json ELSE '' END FROM outbox_jobs WHERE id=? AND job_type='task.recovery.required' AND status IN ('pending','retry') AND available_at<=?`, jobID, now).Scan(&ws, &raw)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			var payload struct {
				TaskID    string `json:"task_id"`
				AttemptID string `json:"attempt_id"`
			}
			if !ws.Valid || json.Unmarshal([]byte(raw), &payload) != nil || payload.TaskID == "" || payload.AttemptID == "" {
				return nil
			}
			var revision int64
			e = tx.QueryRowContext(ctx, `SELECT t.revision FROM tasks t JOIN task_attempts a ON a.task_id=t.id WHERE t.id=? AND t.workspace_id=? AND a.id=? AND a.status='interrupted'`, payload.TaskID, ws.String, payload.AttemptID).Scan(&revision)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			// Bound complete interruption history; never infer identity from a prefix.
			history, e := tx.QueryContext(ctx, `SELECT id,workspace_id,CASE WHEN length(payload_json)<=65536 THEN payload_json ELSE '' END FROM events WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.attempt_interrupted' ORDER BY sequence LIMIT 129`, payload.TaskID)
			if e != nil {
				return e
			}
			var eventID string
			count, matches := 0, 0
			for history.Next() {
				var id, body string
				var eventWS sql.NullString
				if e = history.Scan(&id, &eventWS, &body); e != nil {
					break
				}
				count++
				var p struct {
					TaskID    string `json:"task_id"`
					AttemptID string `json:"attempt_id"`
				}
				if json.Unmarshal([]byte(body), &p) == nil && eventWS.Valid && eventWS.String == ws.String && p.TaskID == payload.TaskID && p.AttemptID == payload.AttemptID {
					eventID = id
					matches++
				}
			}
			if e == nil {
				e = history.Err()
			}
			closeErr := history.Close()
			if e != nil {
				return e
			}
			if closeErr != nil {
				return closeErr
			}
			if count > 128 || matches != 1 {
				return nil
			}
			caseID, e := s.ids.New("recovery")
			if e != nil {
				return e
			}
			result, e := tx.ExecContext(ctx, `INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,task_revision_at_capture,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(attempt_id) DO NOTHING`, caseID, ws.String, payload.TaskID, payload.AttemptID, eventID, jobID, revision, now)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			captured = n == 1
			var storedCase string
			if e = tx.QueryRowContext(ctx, `SELECT id FROM task_recovery_cases WHERE attempt_id=? AND task_id=? AND workspace_id=? AND interruption_event_id=?`, payload.AttemptID, payload.TaskID, ws.String, eventID).Scan(&storedCase); e != nil {
				return e
			}
			if captured {
				auditID, e := s.ids.New("evt")
				if e != nil {
					return e
				}
				body, _ := json.Marshal(map[string]any{"case_id": storedCase, "task_id": payload.TaskID, "attempt_id": payload.AttemptID, "phase": "awaiting_inventory"})
				if e = s.events.Append(ctx, tx, event.Event{ID: auditID, WorkspaceID: &ws.String, Type: "task.recovery_case_captured", AggregateType: "task", AggregateID: payload.TaskID, Payload: body, OccurredAt: now}); e != nil {
					return e
				}
			}
			result, e = tx.ExecContext(ctx, `UPDATE outbox_jobs SET status='succeeded',updated_at=?,claimed_by=NULL,claim_expires_at=NULL WHERE id=? AND status IN ('pending','retry')`, now, jobID)
			if e != nil {
				return e
			}
			n, e = result.RowsAffected()
			acknowledged = n == 1
			return e
		})
		if err != nil {
			return out, err
		}
		s.recoveryCursor = jobID
		out.Scanned++
		if captured {
			out.Captured++
		}
		if acknowledged {
			out.Acknowledged++
		} else {
			out.Deferred++
		}
	}
	return out, nil
}
