package api

import (
 "context"
 "database/sql"
 "fmt"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A read-only, scoped observation of persisted execution rows. These rows
// are joined by Task identity, NOT by inferred request/trace causality. No
// model names, tool IDs, operation resources, prompts, results, or error text.
const qaExecutionSourceCap=64
const qaExecutionQueryTimeout=2*time.Second
const qaExecutionCountMax int64=65535

type qaExecutionSource struct {
 TaskRef string `json:"task_ref"`
 Source string `json:"source"`
 Total int64 `json:"total"`
 Succeeded int64 `json:"succeeded"`
 Failed int64 `json:"failed"`
 Uncertain int64 `json:"uncertain"`
 Pending int64 `json:"pending"`
 Unclassified int64 `json:"unclassified"`
 Capped bool `json:"capped"`
}

// Snapshot requirements are stronger than just knowing an ID:
// a Task must belong to the requested tenant, Project, and canonical
// Workspace in BOTH the provided page and the current persisted database.
// Every side-effect table row must independently match the tenant.
func loadQAExecutionSources(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,
 visible []task.Task)([]qaExecutionSource,bool,error){
 out:=make([]qaExecutionSource,0)
 if db==nil||tenant==""||projectID==""||workspaceID==""||len(visible)==0{return out,false,nil}
 ids:=make([]string,0,qaSnapshotTaskCap)
 seen:=map[string]bool{}
 for _,t:=range visible{
  if t.ID==""||t.WorkspaceID!=tenant||t.ProjectID==nil||*t.ProjectID!=projectID||
   t.ProjectWorkspaceID==nil||*t.ProjectWorkspaceID!=workspaceID||seen[t.ID]{continue}
  seen[t.ID]=true
  ids=append(ids,t.ID)
  if len(ids)>=qaSnapshotTaskCap{break}
 }
 if len(ids)==0{return out,false,nil}
 marks:=strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")
 // Static source/status vocabulary only. Bind all IDs and scope, and avoid
 // untrusted strings from any of the model/tool/operation/verification rows.
 q:=`WITH scoped AS(
 SELECT id FROM tasks WHERE workspace_id=? AND project_id=? AND project_workspace_id=? AND id IN (`+marks+`)
 )
 SELECT kind,task_id,total,successes,failures,uncertainties,pending FROM(
 SELECT 'model' AS kind,m.task_id,COUNT(*) total,
  SUM(CASE WHEN m.status='succeeded' THEN 1 ELSE 0 END) successes,
  SUM(CASE WHEN m.status IN('failed','rate_limited') THEN 1 ELSE 0 END) failures,
  SUM(CASE WHEN m.status='unknown' THEN 1 ELSE 0 END) uncertainties,
  SUM(CASE WHEN m.status IN('created','routed','budget_reserved','dispatched','executing') THEN 1 ELSE 0 END) pending
 FROM inference_requests m JOIN scoped t ON t.id=m.task_id WHERE m.workspace_id=?
 GROUP BY m.task_id
 UNION ALL
 SELECT 'tool',v.task_id,COUNT(*),
  SUM(CASE WHEN v.status='succeeded' THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status IN('failed','timed_out') THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status='interrupted' THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status IN('created','authorized','running') THEN 1 ELSE 0 END)
 FROM tool_invocations v JOIN scoped t ON t.id=v.task_id WHERE v.workspace_id=?
 GROUP BY v.task_id
 UNION ALL
 SELECT 'operation',o.task_id,COUNT(*),
  SUM(CASE WHEN o.state='committed' THEN 1 ELSE 0 END),
  SUM(CASE WHEN o.state IN('denied','failed','aborted','compensation_failed') THEN 1 ELSE 0 END),
  SUM(CASE WHEN o.state IN('unknown_outcome','blocked_unknown_outcome') THEN 1 ELSE 0 END),
  SUM(CASE WHEN o.state IN('proposed','authorized','prepared','executing','observing','verified','compensating','compensated') THEN 1 ELSE 0 END)
 FROM operations o JOIN scoped t ON t.id=o.task_id WHERE o.workspace_id=?
 GROUP BY o.task_id
 UNION ALL
 SELECT 'verification',v.task_id,COUNT(*),
  SUM(CASE WHEN v.status='pass' THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status='fail' THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status IN('inconclusive','stale') THEN 1 ELSE 0 END),
  SUM(CASE WHEN v.status='pending' THEN 1 ELSE 0 END)
 FROM verifications v JOIN scoped t ON t.id=v.task_id WHERE v.workspace_id=?
 GROUP BY v.task_id
 ) ORDER BY task_id,kind LIMIT ?`
 args:=make([]any,0,len(ids)+8)
 args=append(args,tenant,projectID,workspaceID)
 for _,id:=range ids{args=append(args,id)}
 args=append(args,tenant,tenant,tenant,tenant,qaExecutionSourceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,qaExecutionQueryTimeout)
 defer cancel()
 rows,err:=db.QueryContext(bounded,q,args...)
 if err!=nil{return nil,false,fmt.Errorf("load scoped execution evidence: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var kind,taskID string
  var n,success,failed,uncertain,pending int64
  if err:=rows.Scan(&kind,&taskID,&n,&success,&failed,&uncertain,&pending);err!=nil{return nil,false,err}
  if len(out)>=qaExecutionSourceCap{return out,true,nil}
  if n<0||success<0||failed<0||uncertain<0||pending<0||
   success+failed+uncertain+pending>n{return nil,false,fmt.Errorf("invalid execution evidence counters")}
  if kind!="model"&&kind!="tool"&&kind!="operation"&&kind!="verification"{
   return nil,false,fmt.Errorf("invalid execution evidence source")
  }
  capped:=n>qaExecutionCountMax
  clamp:=func(x int64)int64{if x>qaExecutionCountMax{return qaExecutionCountMax};return x}
  out=append(out,qaExecutionSource{
   TaskRef:qaOpaqueRef("task",taskID),Source:kind,Total:clamp(n),
   Succeeded:clamp(success),Failed:clamp(failed),
   Uncertain:clamp(uncertain),Pending:clamp(pending),
   Unclassified:clamp(n-success-failed-uncertain-pending),Capped:capped,
  })
 }
 if err:=rows.Err();err!=nil{return nil,false,err}
 return out,false,nil
}
