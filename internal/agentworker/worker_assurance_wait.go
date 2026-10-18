package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func (s *Service) parkWorkerForIndependentAssurance(
	ctx context.Context, run Run, res TickResult,
	continuation json.RawMessage, verificationID, requiredLevel string,
) TickResult {
	if s==nil||s.tx==nil||s.db==nil||s.tasks==nil {
		return failedResult(res,ErrInvalidWorkerState)
	}
	err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		var runState,taskState,attemptState,principal string
		err:=tx.QueryRowContext(ctx,`SELECT r.status,t.state,a.status,a.worker_principal_id
			FROM agent_worker_runs r
			JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
			JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
			WHERE r.id=? AND r.workspace_id=? AND r.task_id=?
				AND r.attempt_id=? AND r.revision=?`,
			run.ID,run.WorkspaceID,run.TaskID,run.AttemptID,run.Revision,
		).Scan(&runState,&taskState,&attemptState,&principal)
		if err!=nil{return err}
		if runState!=string(RunRunning)||taskState!="verifying"||
			attemptState!="waiting"||principal!=WorkerPrincipal{
			return ErrInvalidWorkerState
		}
		var count int
		if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
			WHERE task_id=? AND status IN ('created','queued','running','waiting')`,
			run.TaskID).Scan(&count);err!=nil{return err}
		if count!=1{return ErrInvalidWorkerState}
		now:=s.clock.UnixMilli()
		changed,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
			SET status='blocked',continuation_json=?,
				last_error=?,revision=revision+1,updated_at=?
			WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=?
				AND revision=? AND status='running'`,
			string(continuation),ErrHigherVerification.Error(),now,
			run.ID,run.WorkspaceID,run.TaskID,run.AttemptID,run.Revision)
		if err!=nil{return err}
		n,err:=changed.RowsAffected()
		if err!=nil{return err}
		if n!=1{return ErrInvalidWorkerState}
		proposal:="complete"
		// The Step journal CHECK constraint does not accept "queued".
		// WAITING is an explicit supported status, with pending
		// independent verification rather than inferred success.
		return s.journalInTransaction(ctx,tx,run.ID,"verification","waiting",
			nil,nil,&proposal,&verificationID,map[string]any{
				"verification_id":verificationID,
				"required_level":requiredLevel,
				"state":"awaiting_independent_assurance",
			})
	})
	if err!=nil{
		s.dispatchReconciliationFailed=true
		if err==sql.ErrNoRows {err=ErrInvalidWorkerState}
		return failedResult(res,fmt.Errorf("independent assurance Worker park failed; scheduler halted pending recovery: %w",err))
	}
	res.Status="blocked_verification"
	res.Error=fmt.Sprintf("completion requires %s; independent assurance queued",requiredLevel)
	return res
}
