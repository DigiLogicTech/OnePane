package api

import (
 "context"
 "database/sql"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// taskExecutionProgress is an observation of the durable Agent Worker journal,
// NOT permission to resume or evidence that an in-flight action was committed.
// No prompts, raw tool results, continuation data or credentials are returned.
type taskExecutionProgress struct {
 RunID string `json:"run_id"`
 Status string `json:"status"`
 StepsUsed int64 `json:"steps_used"`
 MaxSteps int64 `json:"max_steps"`
 LastStepKind string `json:"last_step_kind,omitempty"`
 LastStepStatus string `json:"last_step_status,omitempty"`
 UpdatedAt int64 `json:"updated_at"`
 ReviewRequired bool `json:"review_required"`
}

// loadTaskExecutionProgress is fed ONLY the canonical already-authorised Task
// page and checks tenancy, Project and Workspace again in SQL. It never
// queries another Workspace's journal, even when handed a foreign Task ID.
func loadTaskExecutionProgress(ctx context.Context,db *sql.DB,tenantID,projectID,workspaceID string,
 visible []task.Task)(map[string]taskExecutionProgress,error){
 progress:=make(map[string]taskExecutionProgress)
 if db==nil||tenantID==""||projectID==""||workspaceID==""||len(visible)==0{return progress,nil}
 ids:=make([]string,0,len(visible))
 args:=[]any{tenantID,projectID,workspaceID,tenantID}
 seen:=make(map[string]bool,len(visible))
 for _,t:=range visible{
  if t.WorkspaceID!=tenantID||t.ProjectID==nil||*t.ProjectID!=projectID||
   t.ProjectWorkspaceID==nil||*t.ProjectWorkspaceID!=workspaceID||seen[t.ID]{continue}
  ids=append(ids,t.ID)
  args=append(args,t.ID)
  seen[t.ID]=true
 }
 if len(ids)==0{return progress,nil}
 // The Workspace queue is capped at 15, while the same projection can safely
 // support the backend's 500-Task maximum without exceeding SQLite variables.
 marks:=strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")
 query:=`SELECT r.task_id,r.id,r.status,r.step_count,r.max_steps,r.updated_at,
 COALESCE(st.step_kind,''),COALESCE(st.status,'')
 FROM agent_worker_runs r
 JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
 LEFT JOIN agent_worker_steps st ON st.id=(
  SELECT x.id FROM agent_worker_steps x WHERE x.run_id=r.id
  ORDER BY x.step_number DESC,x.id DESC LIMIT 1
 )
 WHERE t.workspace_id=? AND t.project_id=? AND t.project_workspace_id=?
 AND r.workspace_id=? AND r.task_id IN (`+marks+`)
 ORDER BY r.started_at DESC,r.id DESC`
 rows,err:=db.QueryContext(ctx,query,args...)
 if err!=nil{return nil,fmt.Errorf("load Workspace Task execution progress: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var id,runID,status,kind,stepStatus string
  var used,max,updated int64
  if err:=rows.Scan(&id,&runID,&status,&used,&max,&updated,&kind,&stepStatus);err!=nil{return nil,err}
  if _,exists:=progress[id];exists{continue} // newest run wins
  if max<1||used<0||used>max{continue} // fail closed on invalid counters
  progress[id]=taskExecutionProgress{
   RunID:runID,Status:status,StepsUsed:used,MaxSteps:max,
   LastStepKind:kind,LastStepStatus:stepStatus,UpdatedAt:updated,
   ReviewRequired:status=="interrupted"||stepStatus=="unknown"||
    (status=="interrupted"&&stepStatus=="started"),
  }
 }
 return progress,rows.Err()
}
