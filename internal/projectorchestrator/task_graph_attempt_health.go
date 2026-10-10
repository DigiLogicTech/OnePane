package projectorchestrator

import (
 "context"
 "database/sql"
 "encoding/json"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// taskAttemptHealthTx is a read-only view within the graph's existing
// Project-scoped SQLite snapshot, not a claim that the external model or
// tool did (or did not) run. It never returns raw continuation or prompts.
func taskAttemptHealthTx(ctx context.Context,tx *sql.Tx,taskID,tenant string,state task.State)(string,error){
 rows,err:=tx.QueryContext(ctx,`SELECT id,status,
  COALESCE(worker_principal_id,''),metadata_json
  FROM task_attempts WHERE task_id=?
   AND status IN ('created','queued','running','waiting')
  ORDER BY attempt_number DESC LIMIT 2`,taskID)
 if err!=nil{return "",err}
 type attempt struct{id,status,owner,metadata string}
 matches:=[]attempt{}
 for rows.Next(){
  var a attempt
  if err=rows.Scan(&a.id,&a.status,&a.owner,&a.metadata);err!=nil{break}
  matches=append(matches,a)
 }
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil{return "",err}
 if closeErr!=nil{return "",closeErr}
 if len(matches)>1{return "ambiguous_active_attempts",nil}
 if len(matches)==0{
  if state==task.StateRunning{return "missing_active_attempt",nil}
  return "none",nil
 }
 a:=matches[0]
 var metadata struct{AgentWorker string `json:"agent_worker"`}
 if json.Unmarshal([]byte(a.metadata),&metadata)!=nil{return "invalid_attempt_metadata",nil}
 if a.owner!="system:agent-worker"||metadata.AgentWorker!="v1"{
  return "other_executor_unverified",nil
 }
 var runStatus string
 err=tx.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs
  WHERE task_id=? AND attempt_id=? AND workspace_id=?
   AND worker_principal_id='system:agent-worker'
   AND status IN ('running','waiting')
  ORDER BY updated_at DESC,id LIMIT 1`,taskID,a.id,tenant).Scan(&runStatus)
 if err==sql.ErrNoRows{return "orphaned_agent_attempt",nil}
 if err!=nil{return "",err}
 if state==task.StateRunning&&a.status=="running"&&runStatus=="running"{
  return "agent_worker_running",nil
 }
 if a.status=="waiting"&&runStatus=="waiting"&&
  (state==task.StateWaitingDependency||state==task.StateWaitingApproval||
   state==task.StatePaused||state==task.StateVerifying){
  return "agent_worker_waiting",nil
 }
 return "agent_worker_state_mismatch",nil
}

func needsAttemptRecoveryReview(status string)bool{
 switch status{
 case "orphaned_agent_attempt","missing_active_attempt",
  "ambiguous_active_attempts","invalid_attempt_metadata",
  "agent_worker_state_mismatch":
  return true
 }
 return false
}
