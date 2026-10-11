package agentworker

import (
 "context"
 "encoding/json"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/storage"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// suspendForResource atomically persists the Worker wait, Task and active
// Attempt transition, and audit journal. No inference or Tool command is
// replayed by this method. Failed or stale updates roll back all writes.
func (s *Service) suspendForResource(ctx context.Context, run Run, state json.RawMessage, reason, kind string, detail map[string]any, lastError *string) error {
 if s==nil||s.tx==nil||s.tasks==nil||run.Revision<1||!json.Valid(state) {
  return ErrInvalidWorkerState
 }
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var revision int64
  err:=tx.QueryRowContext(ctx,`SELECT revision FROM tasks
   WHERE id=? AND workspace_id=? AND state='running'`,
   run.TaskID,run.WorkspaceID).Scan(&revision)
  if err!=nil{return err}
  now:=s.clock.UnixMilli()
  changed,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
   SET status='waiting',continuation_json=?,last_error=?,
   revision=revision+1,updated_at=?
   WHERE id=? AND workspace_id=? AND task_id=? AND attempt_id=?
   AND status='running' AND revision=?`,
   string(state),lastError,now,run.ID,run.WorkspaceID,run.TaskID,
   run.AttemptID,run.Revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrInvalidWorkerState}
  actor:=WorkerPrincipal
  if err:=s.tasks.WaitDependencyInTransaction(ctx,tx,task.TransitionCommand{
   TaskID:run.TaskID,ExpectedRevision:revision,ActorPrincipalID:&actor,
   Reason:reason,
  });err!=nil{return err}
  if err:=s.journalInTransaction(ctx,tx,run.ID,kind,"waiting",
   nil,nil,nil,nil,detail);err!=nil{return fmt.Errorf("journal resource suspension: %w",err)}
  return nil
 })
}
