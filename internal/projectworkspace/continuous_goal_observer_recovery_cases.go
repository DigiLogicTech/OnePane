package projectworkspace

import (
 "context"
 "strings"
)

// GoalObserverRecoveryCase is a read-only pointer to a durable Operation
// that might have changed ONLY OnePane's local recurring TCP dispatch state.
// A failure/pause is NOT evidence of physical endpoint remediation.
type GoalObserverRecoveryCase struct {
 OperationID string `json:"operation_id"`
 ToolID string `json:"tool_id"`
 State string `json:"state"`
 UpdatedAt int64 `json:"updated_at"`
 HumanReconciliationRequired bool `json:"human_reconciliation_required"`
 IndependentDeviceVerification bool `json:"independent_device_verification"`
}

func(s *Service)ListGoalObserverRecoveryCases(ctx context.Context,
 projectID,goalID string)([]GoalObserverRecoveryCase,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var wsID,tenantID string
 err:=s.db.QueryRowContext(ctx,`SELECT g.project_workspace_id,p.workspace_id
  FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).
  Scan(&wsID,&tenantID)
 if err!=nil{return nil,err}
 resource:="goal-tcp-observer://"+projectID+"/"+goalID
 // No payload, receipt, Task objective, principal/credential, address or
 // operator-supplied SQL clause is returned. The inventory carries the
 // unbounded total; this list is capped to 25 rows for low-resource clients.
 rows,err:=s.db.QueryContext(ctx,`SELECT op.id,op.tool_id,op.state,op.updated_at
  FROM operations op
  JOIN tasks task ON task.id=op.task_id
   AND task.workspace_id=op.workspace_id
   AND task.project_id=? AND task.project_workspace_id=?
  WHERE op.workspace_id=? AND op.resource_ref=?
   AND op.adapter_id='onepane.goal.tcp_observer.control'
   AND op.tool_id IN ('project.goal.tcp_observer.pause',
     'project.goal.tcp_observer.resume')
   AND op.state IN ('executing','observing','verified',
     'unknown_outcome','blocked_unknown_outcome','compensating',
     'compensation_failed')
  ORDER BY op.updated_at DESC,op.id DESC LIMIT 25`,
  projectID,wsID,tenantID,resource)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalObserverRecoveryCase{}
 for rows.Next(){
  v:=GoalObserverRecoveryCase{HumanReconciliationRequired:true}
  if err:=rows.Scan(&v.OperationID,&v.ToolID,&v.State,&v.UpdatedAt);err!=nil{
   return nil,err
  }
  out=append(out,v)
 }
 return out,rows.Err()
}
