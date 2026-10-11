package agentworker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// AssurancePassed closes the original Agent Worker only after the
// independently verified Task completion is durably correlated to the exact
// passed Verification and Checkpoint. An arbitrary blocked Worker is not
// evidence of success, even if a caller knows a checkpoint ID.
//
// Worker final state and success journal/audit must commit atomically. A
// failed journal write cannot leave a silent 'succeeded' Worker.
func (s *Service) AssurancePassed(ctx context.Context,runID,verificationID,checkpointID string) error {
	if s==nil||s.db==nil||s.tx==nil||
		strings.TrimSpace(runID)==""||strings.TrimSpace(verificationID)==""||
		strings.TrimSpace(checkpointID)==""{return ErrInvalidWorkerState}
	return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		var status RunStatus
		var taskID,workspaceID,attemptID string
		err:=tx.QueryRowContext(ctx,`SELECT status,task_id,workspace_id,attempt_id
		 FROM agent_worker_runs WHERE id=? AND worker_principal_id=?`,
		 runID,WorkerPrincipal).Scan(&status,&taskID,&workspaceID,&attemptID)
		if err!=nil{return fmt.Errorf("%w: Worker ownership unavailable: %v",ErrInvalidWorkerState,err)}
		if status==RunSucceeded{
			// Retried notifications must name the same durable evidence
			// already recorded. Never silently accept an unrelated receipt.
			var count int
			err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
			 WHERE run_id=? AND step_kind='verification' AND status='succeeded'
			  AND result_ref=? AND json_valid(detail_json)=1
			  AND json_extract(detail_json,'$.verification_id')=?`,
			  runID,checkpointID,verificationID).Scan(&count)
			if err!=nil{return err}
			if count!=1{return ErrInvalidWorkerState}
			return nil
		}
		if status!=RunBlocked&&status!=RunWaiting{return ErrInvalidWorkerState}
		var evidence int
		// CompleteVerified records this event *in the same transaction*
		// as Task completion and original Attempt success. Read its exact
		// checkpoint and Verification rather than trusting the callback.
		err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks t
		 JOIN task_attempts a ON a.id=? AND a.task_id=t.id
		  AND a.worker_principal_id=? AND a.status='succeeded'
		  AND json_valid(a.metadata_json)=1
		  AND json_extract(a.metadata_json,'$.agent_worker')='v1'
		 JOIN checkpoints c ON c.id=? AND c.task_id=t.id
		  AND c.workspace_id=t.workspace_id AND c.verification_id=? AND c.status='valid'
		 JOIN verifications v ON v.id=c.verification_id
		  AND v.task_id=t.id AND v.workspace_id=t.workspace_id
		  AND v.status='pass' AND v.achieved_level IS NOT NULL
		 JOIN events e ON e.aggregate_type='task' AND e.aggregate_id=t.id
		  AND e.workspace_id=t.workspace_id AND e.event_type='task.completed'
		  AND json_valid(e.payload_json)=1
		  AND json_extract(e.payload_json,'$.checkpoint_id')=c.id
		 WHERE t.id=? AND t.workspace_id=? AND t.state='complete'`,
		 attemptID,WorkerPrincipal,checkpointID,verificationID,taskID,workspaceID).
		 Scan(&evidence)
		if err!=nil{return err}
		if evidence!=1{return ErrInvalidWorkerState}
		now:=s.clock.UnixMilli()
		r,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
		 SET status='succeeded',last_error=NULL,revision=revision+1,
		  updated_at=?,completed_at=?
		 WHERE id=? AND task_id=? AND workspace_id=? AND attempt_id=?
		  AND worker_principal_id=? AND status IN ('blocked','waiting')`,
		 now,now,runID,taskID,workspaceID,attemptID,WorkerPrincipal)
		if err!=nil{return err}
		changed,err:=r.RowsAffected()
		if err!=nil{return err}
		if changed!=1{return ErrInvalidWorkerState}
		return s.journalInTransaction(ctx,tx,runID,"verification","succeeded",
			nil,nil,strPtr("complete"),strPtr(checkpointID),
			map[string]any{
				"verification_id":verificationID,
				"checkpoint_id":checkpointID,
				"independent":true,
			})
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
	if r.Status == RunRunning {
		return nil
	}
	if r.Status != RunBlocked {
		return ErrInvalidWorkerState
	}
	t, err := s.tasks.Get(ctx, r.TaskID)
	if err != nil {
		return err
	}
	if t.State != task.StateVerifying {
		return fmt.Errorf("%w: task is %s", ErrInvalidWorkerState, t.State)
	}
	actor := VerifierPrincipal
	if _, err := s.tasks.ContinueAfterVerification(ctx, task.TransitionCommand{
		TaskID:           t.ID,
		ExpectedRevision: t.Revision,
		ActorPrincipalID: &actor,
		Reason:           "independent assurance did not establish completion",
	}); err != nil {
		return err
	}
	if len(result) == 0 || !json.Valid(result) {
		result = json.RawMessage(`{}`)
	}
	cont, _ := json.Marshal(map[string]any{
		"verification_feedback": map[string]any{
			"verification_id": verificationID,
			"result":          json.RawMessage(result),
		},
	})
	if err := s.updateRun(ctx, r.ID, r.Revision, RunRunning, cont, nil, 0, 0, nil, nil, strPtr("completion verification did not pass")); err != nil {
		return err
	}
	return s.journal(ctx, r.ID, "verification", "retry", nil, nil, strPtr("complete"), strPtr(verificationID), map[string]any{"verification_id": verificationID})
}
