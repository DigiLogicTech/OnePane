package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type RecoveryDiscoveryResult struct{ Scanned, Captured, Deferred int }

// DiscoverRecoveryCases is bounded trusted maintenance for interrupted Attempts
// whose delivery jobs may already be gone. Ledger provenance never acknowledges
// a job, invents a job identifier or clears the execution admission fence.
func (s *Service) DiscoverRecoveryCases(ctx context.Context, limit int) (RecoveryDiscoveryResult, error) {
	var out RecoveryDiscoveryResult
	var failures error
	if limit <= 0 || limit > 16 {
		limit = 16
	}
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT a.id FROM task_attempts a
 WHERE a.status='interrupted' AND a.id>?
 ORDER BY a.id LIMIT ?`, s.recoveryDiscoveryCursor, limit)
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
		s.recoveryDiscoveryCursor = ""
	}
	for _, attemptID := range ids {
		captured := false
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var taskID, workspace string
			var revision int64
			e := tx.QueryRowContext(ctx, `SELECT t.id,t.workspace_id,t.revision FROM task_attempts a JOIN tasks t ON t.id=a.task_id
    WHERE a.id=? AND a.status='interrupted' AND NOT EXISTS(SELECT 1 FROM task_recovery_cases c WHERE c.attempt_id=a.id)`, attemptID).Scan(&taskID, &workspace, &revision)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			eventID, e := recoveryInterruptionEvent(ctx, tx, workspace, taskID, attemptID)
			if e != nil {
				return e
			}
			if eventID == "" {
				return nil
			}
			caseID, e := s.ids.New("recovery")
			if e != nil {
				return e
			}
			now := s.clock.UnixMilli()
			result, e := tx.ExecContext(ctx, `INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_kind,task_revision_at_capture,created_at)
    VALUES(?,?,?,?,?,'interruption_ledger',?,?) ON CONFLICT(attempt_id) DO NOTHING`, caseID, workspace, taskID, attemptID, eventID, revision, now)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if n == 0 {
				return nil
			}
			auditID, e := s.ids.New("evt")
			if e != nil {
				return e
			}
			body, _ := json.Marshal(map[string]any{"case_id": caseID, "task_id": taskID, "attempt_id": attemptID, "phase": "awaiting_inventory", "source_kind": "interruption_ledger"})
			if e = s.events.Append(ctx, tx, event.Event{ID: auditID, WorkspaceID: &workspace, Type: "task.recovery_case_captured", AggregateType: "task", AggregateID: taskID, Payload: body, OccurredAt: now}); e != nil {
				return e
			}
			captured = true
			return nil
		})
		// Failed writes stay durable and are retried after cursor wrap. They
		// cannot prevent later independent Attempts from reaching intake.
		s.recoveryDiscoveryCursor = attemptID
		out.Scanned++
		if err != nil {
			out.Deferred++
			failures = errors.Join(failures, err)
			if ctx.Err() != nil {
				return out, failures
			}
			continue
		}
		if captured {
			out.Captured++
		} else {
			out.Deferred++
		}
	}
	return out, failures
}

// Both delivery and historical discovery require a complete bounded ledger
// observation and exactly one scoped event for this original incarnation.
func recoveryInterruptionEvent(ctx context.Context, tx storage.Tx, workspace, taskID, attemptID string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,workspace_id,CASE WHEN length(CAST(payload_json AS BLOB))<=65536 THEN payload_json ELSE '' END
 FROM events WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.attempt_interrupted' ORDER BY sequence LIMIT 129`, taskID)
	if err != nil {
		return "", err
	}
	var eventID string
	count, matches := 0, 0
	for rows.Next() {
		var id, body string
		var ws sql.NullString
		if err = rows.Scan(&id, &ws, &body); err != nil {
			break
		}
		count++
		var p struct {
			TaskID    string `json:"task_id"`
			AttemptID string `json:"attempt_id"`
		}
		if json.Unmarshal([]byte(body), &p) == nil && ws.Valid && ws.String == workspace && p.TaskID == taskID && p.AttemptID == attemptID {
			eventID = id
			matches++
		}
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if count > 128 || matches != 1 {
		return "", nil
	}
	return eventID, nil
}
