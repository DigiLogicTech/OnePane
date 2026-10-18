package agentworker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func (s *Service) completeVerifiedWorkerRun(
	ctx context.Context, run Run, res TickResult, expectedTaskRevision int64,
	checkpointID string, result json.RawMessage, verificationID string,
) TickResult {
	if s==nil||s.tasks==nil||s.tx==nil {
		return failedResult(res,ErrInvalidWorkerState)
	}
	err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		var status string
		err:=tx.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs
			WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=? AND revision=?`,
			run.ID,run.WorkspaceID,run.TaskID,run.AttemptID,run.Revision).Scan(&status)
		if err!=nil{return err}
		if status!=string(RunRunning){return ErrInvalidWorkerState}
		verifier:=VerifierPrincipal
		if err:=s.tasks.CompleteVerifiedWorkerInTransaction(ctx,tx,task.CompleteCommand{
			TaskID:run.TaskID,ExpectedRevision:expectedTaskRevision,
			CheckpointID:checkpointID,Result:result,
			ActorPrincipalID:&verifier,
		},run.AttemptID,WorkerPrincipal);err!=nil{return err}
		now:=s.clock.UnixMilli()
		updated,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs SET
			status='succeeded',last_error=NULL,revision=revision+1,
			updated_at=?,completed_at=?
			WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=?
			AND revision=? AND status='running'`,
			now,now,run.ID,run.WorkspaceID,run.TaskID,run.AttemptID,run.Revision)
		if err!=nil{return err}
		n,err:=updated.RowsAffected()
		if err!=nil{return err}
		if n!=1{return ErrInvalidWorkerState}
		proposal:="complete"
		return s.journalInTransaction(ctx,tx,run.ID,"verification","succeeded",
			nil,nil,&proposal,&checkpointID,map[string]any{
				"verification_id":verificationID,"checkpoint_id":checkpointID,"level":"V0",
			})
	})
	if err!=nil{
		s.dispatchReconciliationFailed=true
		return failedResult(res,fmt.Errorf("Worker completion commit failed; scheduler halted pending recovery: %w",err))
	}
	res.Status="complete"
	return res
}
