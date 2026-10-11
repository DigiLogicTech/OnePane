package agentworker

import (
 "context"
 "database/sql"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/storage"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// orphanAgentAttempt describes only the autonomous Agent Worker protocol.
// Other Task executors have different recovery contracts and must not be
// interrupted by this reconciler.
type orphanAgentAttempt struct {
 TaskID string
 AttemptID string
 Revision int64
}

// recoverOrphanedAgentAttempts runs only during a known Worker startup.
// A waiting or operator/verification-BLOCKED Worker retains its original Task,
// model/resource wait and verification continuation. No unknown external
// operation is replayed. Each independent orphan is blocked and audited
// atomically using the ordinary Task recovery transaction.
func(s *Service) recoverOrphanedAgentAttempts(ctx context.Context)(int,error){
 if s==nil||s.db==nil||s.tx==nil||s.tasks==nil{
  return 0,ErrInvalidWorkerState
 }
 rows,err:=s.db.QueryContext(ctx,`SELECT t.id,a.id,t.revision
 FROM task_attempts a
 JOIN tasks t ON t.id=a.task_id
 WHERE a.worker_principal_id=?
  AND a.status IN ('created','queued','running','waiting')
  AND json_valid(a.metadata_json)=1
  AND json_extract(a.metadata_json,'$.agent_worker')='v1'
  AND t.state IN ('running','waiting_dependency','waiting_approval',
   'paused','completion_requested','verifying','blocked')
  AND NOT EXISTS(
   SELECT 1 FROM agent_worker_runs r
   WHERE r.attempt_id=a.id AND r.task_id=a.task_id
    AND r.workspace_id=t.workspace_id
    AND r.status IN ('running','waiting','blocked')
  )
 ORDER BY t.created_at,t.id,a.attempt_number LIMIT 256`,WorkerPrincipal)
 if err!=nil{return 0,err}
 candidates:=[]orphanAgentAttempt{}
 for rows.Next(){
  var orphan orphanAgentAttempt
  if err=rows.Scan(&orphan.TaskID,&orphan.AttemptID,&orphan.Revision);err!=nil{break}
  candidates=append(candidates,orphan)
 }
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil{return 0,err}
 if closeErr!=nil{return 0,closeErr}
 var problems []error
 recovered:=0
 for _,c:=range candidates{
  err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
   // Repeat *all* ownership and live-run exclusions under a write lock.
   // A recovered or newly attached Worker wins without losing its Attempt.
   var currentAttemptID string
   err:=tx.QueryRowContext(ctx,`SELECT a.id FROM task_attempts a
   JOIN tasks t ON t.id=a.task_id
   WHERE a.id=? AND t.id=? AND t.revision=?
    AND a.worker_principal_id=? AND a.status IN ('created','queued','running','waiting')
    AND json_valid(a.metadata_json)=1
    AND json_extract(a.metadata_json,'$.agent_worker')='v1'
    AND t.state IN ('running','waiting_dependency','waiting_approval',
     'paused','completion_requested','verifying','blocked')
    AND NOT EXISTS(
     SELECT 1 FROM agent_worker_runs r WHERE r.attempt_id=a.id
      AND r.task_id=t.id AND r.workspace_id=t.workspace_id
      AND r.status IN ('running','waiting','blocked')
    )`,c.AttemptID,c.TaskID,c.Revision,WorkerPrincipal).
    Scan(&currentAttemptID)
   if errors.Is(err,sql.ErrNoRows){return task.ErrRevisionConflict}
   if err!=nil{return err}
   if currentAttemptID!=c.AttemptID{return task.ErrInvalidAttempt}
   actor:=AuthorityPrincipal
   return s.tasks.InterruptForRecoveryInTransaction(ctx,tx,task.TransitionCommand{
    TaskID:c.TaskID,ExpectedRevision:c.Revision,
    ActorPrincipalID:&actor,
    Reason:"orphan_agent_attempt_no_live_worker_unknown_outcome",
   })
  })
  if err!=nil{
   // A competing successful recovery is not a second failure. All other
   // errors are returned after healthy independent orphans are inspected.
   if !errors.Is(err,task.ErrRevisionConflict){
    problems=append(problems,fmt.Errorf("orphan Task %s Attempt %s: %w",c.TaskID,c.AttemptID,err))
   }
   continue
  }
  recovered++
 }
 return recovered,errors.Join(problems...)
}
