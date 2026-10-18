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
 CreatedAt int64
 AttemptNumber int64
}

// recoverOrphanedAgentAttempts runs only during a known Worker startup.
// A live waiting Worker is intentionally retained for its original Task,
// model/resource wait, and toolchain continuation. No unknown external
// operation is replayed. Each independent orphan is blocked and audited
// atomically using the ordinary Task recovery transaction.
func(s *Service) recoverOrphanedAgentAttempts(ctx context.Context)(int,error){
 if s==nil||s.db==nil||s.tx==nil||s.tasks==nil{
  return 0,ErrInvalidWorkerState
 }
 // Bounded keyset traversal: the old single LIMIT 256 silently left
 // later orphaned Tasks unresolved after large crashes. Never OFFSET
 // over rows that recovery changes, and always move the cursor past a
 // malformed/individually unrecoverable Attempt so it cannot starve the
 // remaining independent work.
 const pageSize=256
 problems := boundedWorkerRecoveryErrors{category:"orphan Agent Attempt recovery failures"}
 recovered:=0
 var lastCreated,lastAttemptNumber int64
 var lastTask,lastAttempt string
 firstPage:=true
 for {
 rows,err:=s.db.QueryContext(ctx,`SELECT t.id,a.id,t.revision,t.created_at,a.attempt_number
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
    AND r.status IN ('running','waiting')
  )
  AND (?=1 OR (t.created_at,t.id,a.attempt_number,a.id)>(?,?,?,?))
 ORDER BY t.created_at,t.id,a.attempt_number,a.id LIMIT ?`,
 WorkerPrincipal,firstPage,lastCreated,lastTask,lastAttemptNumber,lastAttempt,pageSize)
 if err!=nil{return recovered,problems.result(err)}
 candidates:=make([]orphanAgentAttempt,0,pageSize)
 for rows.Next(){
  var orphan orphanAgentAttempt
  if err=rows.Scan(&orphan.TaskID,&orphan.AttemptID,&orphan.Revision,
   &orphan.CreatedAt,&orphan.AttemptNumber);err!=nil{break}
  candidates=append(candidates,orphan)
 }
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil||closeErr!=nil{return recovered,problems.result(err,closeErr)}
 if len(candidates)==0{break}
 last:=candidates[len(candidates)-1]
 lastCreated,lastTask,lastAttemptNumber,lastAttempt=
  last.CreatedAt,last.TaskID,last.AttemptNumber,last.AttemptID
 firstPage=false
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
      AND r.status IN ('running','waiting')
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
    problems.add(fmt.Errorf("orphan Task %s Attempt %s: %w",c.TaskID,c.AttemptID,err))
   }
   continue
  }
  recovered++
 }
 if len(candidates)<pageSize{break}
 }
 return recovered,problems.result()
}
