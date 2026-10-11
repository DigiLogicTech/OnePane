package api

import (
 "context"
 "database/sql"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// taskDependencyEvidence is a read-only count of hard prerequisites on the
// current authorised Task page, not permission to read another Workspace's
// Task body or to automatically resume failed/unknown external actions.
type taskDependencyEvidence struct {
 Total int64 `json:"total"`
 Completed int64 `json:"completed"`
 Failed int64 `json:"failed"`
 Blocked int64 `json:"blocked"`
 Restricted int64 `json:"restricted"`
 Remaining int64 `json:"remaining"`
}

// loadWorkspaceTaskDependencies only selects parent Tasks from the already
// authorised, canonical Project Workspace page. Every parent is rescoped in
// SQL; child states count only for the *same* Project and named Workspace.
// Cross-Workspace/project/tenant edges contribute to restricted counts, but
// child identity, state, objective, evidence and metadata remain invisible.
func loadWorkspaceTaskDependencies(ctx context.Context,db *sql.DB,tenantID,projectID,workspaceID string,
 visible []task.Task)(map[string]taskDependencyEvidence,error) {
 out:=make(map[string]taskDependencyEvidence)
 if db==nil||tenantID==""||projectID==""||workspaceID==""||len(visible)==0{return out,nil}
 ids:=make([]string,0,len(visible))
 args:=[]any{tenantID,projectID,workspaceID}
 seen:=map[string]bool{}
 for _,t:=range visible {
  if t.WorkspaceID!=tenantID||t.ProjectID==nil||*t.ProjectID!=projectID||
   t.ProjectWorkspaceID==nil||*t.ProjectWorkspaceID!=workspaceID||seen[t.ID]{continue}
  ids=append(ids,t.ID)
  args=append(args,t.ID)
  seen[t.ID]=true
 }
 if len(ids)==0{return out,nil}
 marks:=strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")
 query:=`SELECT t.id,
 COUNT(d.depends_on_task_id),
 COALESCE(SUM(CASE WHEN dep.workspace_id=t.workspace_id AND dep.project_id=t.project_id
   AND dep.project_workspace_id=t.project_workspace_id AND dep.state='complete' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN dep.workspace_id=t.workspace_id AND dep.project_id=t.project_id
   AND dep.project_workspace_id=t.project_workspace_id AND dep.state IN ('failed','cancelled') THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN dep.workspace_id=t.workspace_id AND dep.project_id=t.project_id
   AND dep.project_workspace_id=t.project_workspace_id AND dep.state='blocked' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN dep.id IS NULL OR dep.workspace_id<>t.workspace_id
   OR dep.project_id IS NULL OR dep.project_id<>t.project_id
   OR dep.project_workspace_id IS NULL OR dep.project_workspace_id<>t.project_workspace_id
   THEN 1 ELSE 0 END),0)
 FROM tasks t
 JOIN task_dependencies d ON d.task_id=t.id AND d.dependency_type='hard'
 LEFT JOIN tasks dep ON dep.id=d.depends_on_task_id
 WHERE t.workspace_id=? AND t.project_id=? AND t.project_workspace_id=?
 AND t.id IN (`+marks+`)
 GROUP BY t.id`
 rows,err:=db.QueryContext(ctx,query,args...)
 if err!=nil{return nil,fmt.Errorf("load Workspace Task prerequisite evidence: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var id string
  var d taskDependencyEvidence
  if err:=rows.Scan(&id,&d.Total,&d.Completed,&d.Failed,&d.Blocked,&d.Restricted);err!=nil{return nil,err}
  if d.Total<0||d.Completed<0||d.Failed<0||d.Blocked<0||d.Restricted<0||
   d.Completed+d.Failed+d.Blocked+d.Restricted>d.Total {continue}
  d.Remaining=d.Total-d.Completed
  out[id]=d
 }
 return out,rows.Err()
}
