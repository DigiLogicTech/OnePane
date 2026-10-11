package projectorchestrator

import (
 "context"
 "database/sql"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// taskExternalEffectHealthTx observes only Task-scoped Operation *state*.
// External tool inputs, secrets, resource refs and operation receipts stay
// inaccessible to the Project graph/QA surface.
func taskExternalEffectHealthTx(ctx context.Context,tx *sql.Tx,taskID,tenant string,state task.State)(string,error){
 var unknown,compensationFailed,compensating,unsettled int
 err:=tx.QueryRowContext(ctx,`SELECT
  COALESCE(SUM(CASE WHEN state IN ('unknown_outcome','blocked_unknown_outcome') THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN state='compensation_failed' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN state='compensating' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN state IN ('executing','observing','compensating') THEN 1 ELSE 0 END),0)
  FROM operations WHERE task_id=? AND workspace_id=?`,
  taskID,tenant).Scan(&unknown,&compensationFailed,&compensating,&unsettled)
 if err!=nil{return "",err}
 switch{
 case unknown>0:return "unresolved_unknown_outcome",nil
 case compensationFailed>0:return "failed_compensation",nil
 case unsettled>0&&state!=task.StateRunning:
  return "inflight_effect_outlived_task",nil
 case compensating>0:return "compensation_in_progress",nil
 case unsettled>0:return "external_effect_in_progress",nil
 default:return "none",nil
 }
}

func needsExternalEffectReview(status string)bool{
 switch status{
 case "unresolved_unknown_outcome","failed_compensation",
  "inflight_effect_outlived_task","compensation_in_progress":
  return true
 }
 return false
}
