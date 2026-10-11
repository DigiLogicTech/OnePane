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
		  AND v.subject_ref='agent_worker_run:' || ?
		  AND json_valid(v.spec_json)=1
		  AND json_extract(v.spec_json,'$.source')='agent_completion_proposal'
		  AND json_extract(v.spec_json,'$.worker_run_id')=?
		 JOIN events e ON e.aggregate_type='task' AND e.aggregate_id=t.id
		  AND e.workspace_id=t.workspace_id AND e.event_type='task.completed'
		  AND json_valid(e.payload_json)=1
		  AND json_extract(e.payload_json,'$.checkpoint_id')=c.id
		 WHERE t.id=? AND t.workspace_id=? AND t.state='complete'`,
		 attemptID,WorkerPrincipal,checkpointID,verificationID,
		 runID,runID,taskID,workspaceID).
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

// AssuranceRetry restores the SAME original Task Attempt only when a
// matching independently resolved Verification failed or was inconclusive.
// Task, Attempt, Worker status, feedback and immutable journal either all
// commit or all roll back; no new Attempt or external operation is replayed.
func (s *Service) AssuranceRetry(ctx context.Context,runID,verificationID string,result json.RawMessage) error {
	if s==nil||s.db==nil||s.tx==nil||s.tasks==nil||
		strings.TrimSpace(runID)==""||strings.TrimSpace(verificationID)==""{
		return ErrInvalidWorkerState
	}
	if len(result)==0||!json.Valid(result){result=json.RawMessage(`{}`)}
	continuation,err:=json.Marshal(map[string]any{
		"verification_feedback":map[string]any{
			"verification_id":verificationID,
			"result":json.RawMessage(result),
		},
	})
	if err!=nil{return err}
	return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		var taskID,workspaceID,attemptID string
		var status RunStatus
		var revision int64
		err:=tx.QueryRowContext(ctx,`SELECT task_id,workspace_id,attempt_id,status,revision
		 FROM agent_worker_runs WHERE id=? AND worker_principal_id=?`,
		 runID,WorkerPrincipal).Scan(&taskID,&workspaceID,&attemptID,&status,&revision)
		if err!=nil{return fmt.Errorf("%w: missing Worker run: %v",ErrInvalidWorkerState,err)}
		if status==RunRunning{
			var receipts int
			err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
			 WHERE run_id=? AND step_kind='verification' AND status='failed'
			  AND result_ref=? AND json_valid(detail_json)=1
			  AND json_extract(detail_json,'$.verification_id')=?
			  AND json_extract(detail_json,'$.action')='retry'`,
			  runID,verificationID,verificationID).Scan(&receipts)
			if err!=nil{return err}
			if receipts!=1{return ErrInvalidWorkerState}
			return nil
		}
		if status!=RunBlocked{return ErrInvalidWorkerState}
		var approved int
		err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM verifications v
		 WHERE v.id=? AND v.task_id=? AND v.workspace_id=?
		  AND v.status IN ('fail','inconclusive') AND v.verified_by IS NOT NULL
		  AND v.subject_ref='agent_worker_run:' || ?
		  AND json_valid(v.spec_json)=1
		  AND json_extract(v.spec_json,'$.source')='agent_completion_proposal'
		  AND json_extract(v.spec_json,'$.worker_run_id')=?`,
		  verificationID,taskID,workspaceID,runID,runID).Scan(&approved)
		if err!=nil{return err}
		if approved!=1{return ErrInvalidWorkerState}
		var taskRevision int64
		err=tx.QueryRowContext(ctx,`SELECT revision FROM tasks
		 WHERE id=? AND workspace_id=? AND state='verifying'`,taskID,workspaceID).
		 Scan(&taskRevision)
		if err!=nil{return fmt.Errorf("%w: Task is not VERIFYING: %v",ErrInvalidWorkerState,err)}
		actor:=VerifierPrincipal
		if err:=s.tasks.ContinueAfterVerificationInTransaction(ctx,tx,task.TransitionCommand{
			TaskID:taskID,ExpectedRevision:taskRevision,ActorPrincipalID:&actor,
			Reason:"independent assurance did not establish completion",
		},attemptID,WorkerPrincipal);err!=nil{return err}
		now:=s.clock.UnixMilli()
		r,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
		 SET status='running',continuation_json=?,last_error=?,
		  revision=revision+1,updated_at=?
		 WHERE id=? AND task_id=? AND workspace_id=? AND attempt_id=?
		  AND worker_principal_id=? AND revision=? AND status='blocked'`,
		  string(continuation),"completion verification did not pass",now,
		  runID,taskID,workspaceID,attemptID,WorkerPrincipal,revision)
		if err!=nil{return err}
		changed,err:=r.RowsAffected()
		if err!=nil{return err}
		if changed!=1{return ErrInvalidWorkerState}
		// Verification itself failed or was inconclusive. Journal that
		// result honestly; the action=retry marker records recovery of the
		// original Attempt without inventing an unsupported status value.
		return s.journalInTransaction(ctx,tx,runID,"verification","failed",
			nil,nil,strPtr("complete"),strPtr(verificationID),
			map[string]any{"verification_id":verificationID,"action":"retry"})
	})
}
