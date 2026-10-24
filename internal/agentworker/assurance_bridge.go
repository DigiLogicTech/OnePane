package agentworker

import (
	"context"
	"encoding/json"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// AssurancePassed closes the durable worker run after the independent
// assurance service has completed the Task through CompleteVerified.
func (s *Service) AssurancePassed(ctx context.Context, runID, verificationID, checkpointID string) error {
	r, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT r.status FROM agent_worker_runs r
 JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id AND t.state='complete'
 JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id AND a.status='succeeded' AND a.worker_principal_id=r.worker_principal_id
 JOIN verifications v ON v.id=? AND v.task_id=t.id AND v.workspace_id=t.workspace_id AND v.status='pass' AND v.completed_at IS NOT NULL
  AND v.verified_by='system:assurance-verifier' AND v.required_level IN ('V1','V2','V3','V4','V5')
  AND v.subject_ref='agent_worker_run:'||r.id AND json_extract(v.spec_json,'$.assurance_version')=1 AND json_extract(v.spec_json,'$.worker_run_id')=r.id
 JOIN checkpoints c ON c.id=? AND c.task_id=t.id AND c.workspace_id=t.workspace_id AND c.verification_id=v.id AND c.status='valid'
 WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.worker_principal_id=?
 AND NOT EXISTS(SELECT 1 FROM task_attempts other WHERE other.task_id=t.id AND other.status IN ('created','queued','running','waiting'))
 AND NOT EXISTS(SELECT 1 FROM task_attempts other WHERE other.task_id=t.id AND other.attempt_number>a.attempt_number)
 AND EXISTS(SELECT 1 FROM events e WHERE e.aggregate_type='task' AND e.aggregate_id=t.id AND e.workspace_id=t.workspace_id AND e.event_type='task.completed'
  AND json_extract(e.payload_json,'$.checkpoint_id')=c.id AND json_extract(e.payload_json,'$.revision')<=t.revision
  AND NOT EXISTS(SELECT 1 FROM events later WHERE later.aggregate_type='task' AND later.aggregate_id=t.id AND later.event_type='task.completed' AND later.sequence>e.sequence))`, verificationID, checkpointID, r.ID, r.WorkspaceID, r.TaskID, r.AttemptID, r.Revision, WorkerPrincipal).Scan(&state)
		if err != nil {
			return err
		}
		if state == string(RunSucceeded) {
			var delivered bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' AND status='succeeded' AND result_ref=? AND json_extract(detail_json,'$.verification_id')=? AND json_extract(detail_json,'$.checkpoint_id')=? AND json_extract(detail_json,'$.independent')=1)`, r.ID, checkpointID, verificationID, checkpointID).Scan(&delivered)
			if err != nil {
				return err
			}
			if delivered {
				return nil
			}
			return ErrInvalidWorkerState
		}
		if state != string(RunBlocked) && state != string(RunWaiting) {
			return ErrInvalidWorkerState
		}
		var parked, parkedStatus string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(result_ref,''),status FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' ORDER BY step_number DESC LIMIT 1`, r.ID).Scan(&parked, &parkedStatus); err != nil {
			return err
		}
		if parked != verificationID || parkedStatus != "waiting" {
			return ErrInvalidWorkerState
		}
		now := s.clock.UnixMilli()
		changed, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs SET status='succeeded',last_error=NULL,revision=revision+1,updated_at=?,completed_at=? WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=? AND revision=? AND status IN ('blocked','waiting')`, now, now, r.ID, r.WorkspaceID, r.TaskID, r.AttemptID, r.Revision)
		if err != nil {
			return err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrInvalidWorkerState
		}
		return s.journalInTransaction(ctx, tx, r.ID, "verification", "succeeded", nil, nil, strPtr("complete"), strPtr(checkpointID), map[string]any{"verification_id": verificationID, "checkpoint_id": checkpointID, "independent": true})
	})
}

// AssuranceRetry returns a completion attempt that failed or was inconclusive
// to the existing worker attempt. The Task transition and worker-run state are
// explicit; no new attempt is created and verified checkpoints remain intact.
func (s *Service) AssuranceRetry(ctx context.Context, runID, verificationID string, result json.RawMessage) error {
	r, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	// The callback may be redelivered after assurance restarts. Revalidate the
	// stored verification and exact original owners, never a caller's result.
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var workerState, taskState, attemptState, principal, storedResult string
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT r.status,t.state,t.revision,a.status,a.worker_principal_id,COALESCE(v.result_json,'{}')
 FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
 JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
 JOIN verifications v ON v.id=? AND v.workspace_id=t.workspace_id AND v.task_id=t.id
  AND v.subject_ref='agent_worker_run:'||r.id AND v.status IN ('fail','inconclusive')
  AND v.required_level IN ('V1','V2','V3','V4','V5') AND v.completed_at IS NOT NULL
  AND v.verified_by='system:assurance-verifier'
  AND json_extract(v.spec_json,'$.assurance_version')=1 AND json_extract(v.spec_json,'$.worker_run_id')=r.id
 WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=?
  AND r.worker_principal_id=?`, verificationID, r.ID, r.WorkspaceID, r.TaskID, r.AttemptID, r.Revision, WorkerPrincipal).Scan(&workerState, &taskState, &revision, &attemptState, &principal, &storedResult)
		if err != nil {
			return err
		}
		if principal != WorkerPrincipal {
			return ErrInvalidWorkerState
		}
		var lastRef, lastStatus string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(result_ref,''),status FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' ORDER BY step_number DESC LIMIT 1`, r.ID).Scan(&lastRef, &lastStatus); err != nil {
			return err
		}
		if lastRef != verificationID {
			return ErrInvalidWorkerState
		}
		if workerState == string(RunRunning) && taskState == string(task.StateRunning) && attemptState == string(task.AttemptRunning) && lastStatus == "succeeded" {
			var delivered bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_worker_steps WHERE run_id=? AND step_kind='verification' AND status='succeeded' AND result_ref=? AND json_extract(detail_json,'$.action')='retry')`, r.ID, verificationID).Scan(&delivered)
			if err != nil {
				return err
			}
			if delivered {
				return nil
			}
		}
		if workerState != string(RunBlocked) || taskState != string(task.StateVerifying) || attemptState != string(task.AttemptWaiting) || lastStatus != "waiting" {
			return ErrInvalidWorkerState
		}
		actor := VerifierPrincipal
		if err = s.tasks.ContinueAfterVerificationWorkerInTransaction(ctx, tx, task.TransitionCommand{TaskID: r.TaskID, ExpectedRevision: revision, ActorPrincipalID: &actor, Reason: "independent assurance did not establish completion"}, r.AttemptID, WorkerPrincipal); err != nil {
			return err
		}
		cont, _ := json.Marshal(map[string]any{"verification_feedback": map[string]any{"verification_id": verificationID, "result": json.RawMessage(storedResult)}})
		updated, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs SET status='running',continuation_json=?,last_error=?,revision=revision+1,updated_at=? WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=? AND revision=? AND status='blocked'`, string(cont), "completion verification did not pass", s.clock.UnixMilli(), r.ID, r.WorkspaceID, r.TaskID, r.AttemptID, r.Revision)
		if err != nil {
			return err
		}
		n, err := updated.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrInvalidWorkerState
		}
		// 'retry' is an action, not a permitted Step status. Success records
		// the committed continuation; the verification itself still failed.
		return s.journalInTransaction(ctx, tx, r.ID, "verification", "succeeded", nil, nil, strPtr("complete"), strPtr(verificationID), map[string]any{"verification_id": verificationID, "action": "retry", "verification_passed": false})
	})
}
