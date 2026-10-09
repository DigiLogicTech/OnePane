package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// taskModelWait is presentation metadata drawn from the durable Worker state.
// It cannot authorize routing, reschedule a Task or expose a model credential.
type taskModelWait struct {
 Kind string `json:"kind"`
 RetryAtMS int64 `json:"retry_at_ms"`
 Attempt int `json:"attempt"`
 Reason string `json:"reason,omitempty"`
}

func parseTaskModelWait(raw string)(taskModelWait,bool){
 var c struct{
  ModelWait *struct{
   Attempt int `json:"attempt"`
   RetryAtMS int64 `json:"retry_at_ms"`
   Reason string `json:"reason"`
  } `json:"model_wait"`
 }
 if json.Unmarshal([]byte(raw),&c)!=nil||c.ModelWait==nil||
  c.ModelWait.Attempt<1||c.ModelWait.RetryAtMS<1{return taskModelWait{},false}
 reason:=c.ModelWait.Reason
 if len(reason)>180{reason=reason[:180]}
 return taskModelWait{
  Kind:"model_resources",RetryAtMS:c.ModelWait.RetryAtMS,
  Attempt:c.ModelWait.Attempt,Reason:reason,
 },true
}

// Load visible Task IDs first through the already tenancy/project-scoped Task
// repository. Query Worker continuation only for those exact IDs, with an
// additional tenant join and a Task-state check. One bounded query avoids a
// per-Task database round-trip and never returns the raw continuation.
func loadTaskModelWaits(ctx context.Context,db *sql.DB,tenant string,rows []task.Task)(map[string]taskModelWait,error){
 result:=make(map[string]taskModelWait)
 if db==nil||tenant==""||len(rows)==0{return result,nil}
 ids:=make([]string,0,len(rows))
 args:=make([]any,0,len(rows)+1)
 args=append(args,tenant)
 for _,t:=range rows{
  if t.WorkspaceID!=tenant||t.State!=task.StateWaitingDependency{continue}
  ids=append(ids,t.ID)
  args=append(args,t.ID)
 }
 if len(ids)==0{return result,nil}
 marks:=strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")
 q:=`SELECT r.task_id,r.continuation_json
 FROM agent_worker_runs r
 JOIN tasks t ON t.id=r.task_id
 WHERE r.status='waiting' AND t.state='waiting_dependency'
 AND t.workspace_id=? AND r.task_id IN (`+marks+`)
 ORDER BY r.updated_at DESC,r.id DESC`
 qrows,err:=db.QueryContext(ctx,q,args...)
 if err!=nil{return nil,fmt.Errorf("load Task model wait status: %w",err)}
 defer qrows.Close()
 for qrows.Next(){
  var id,raw string
  if err:=qrows.Scan(&id,&raw);err!=nil{return nil,err}
  if _,seen:=result[id];seen{continue}
  if wait,ok:=parseTaskModelWait(raw);ok{result[id]=wait}
 }
 return result,qrows.Err()
}
