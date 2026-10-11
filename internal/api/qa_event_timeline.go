package api

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A bounded, read-only event chronology for the authorised Workspace Task
// selection. No audit payload, principal, user text, model/tool data or
// raw request IDs leave the API. The exported references are pseudonyms.
const qaTimelineEventCap = 96

type qaTimelineEvent struct {
 EventRef string `json:"event_ref"`
 TaskRef string `json:"task_ref"`
 RunRef string `json:"run_ref,omitempty"`
 EventType string `json:"event_type"`
 Severity string `json:"severity"`
 OccurredAt int64 `json:"occurred_at_ms"`
 TraceRef string `json:"trace_ref,omitempty"`
 RequestRef string `json:"request_ref,omitempty"`
}

// An opaque reference is useful for matching a chronology across two
// snapshots without copying potentially attacker-controlled ID text.
// Do not use this hash as an authorization credential or secret.
func qaOpaqueRef(prefix, original string) string {
 if original == "" {return ""}
 digest:=sha256.Sum256([]byte(original))
 return prefix+"-"+hex.EncodeToString(digest[:12])
}

func qaKnownEventType(s string) bool {
 switch s {
 case "task.created","task.ready","task.started","task.waiting_dependency",
 "task.waiting_approval","task.paused","task.resumed","task.completion_requested",
 "task.verification_started","task.verification_retry","task.completed",
 "task.blocked","task.failed","task.cancel_requested","task.attempt_interrupted",
 "task.archived","task.unarchived","agent_worker.started","agent_worker.step":
  return true
 default:return false
 }
}

func qaEventSeverity(s string) string {
 switch s {
 case "task.failed","task.blocked","task.attempt_interrupted":
  return "attention"
 case "task.waiting_approval","task.waiting_dependency","task.cancel_requested":
  return "waiting"
 default:return "information"
 }
}

// Every Task in the CTE is independently rechecked against the persisted
// canonical Project/Workspace and tenancy, even if caller passes forged
// "visible" Task objects. Worker events must belong to the exact same Task
// and tenancy. No dynamic values from events appear in the output except
// hashed opaque references, known event types and numeric timestamps.
func loadQATimeline(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,
 visible []task.Task)([]qaTimelineEvent,bool,error){
 out:=make([]qaTimelineEvent,0)
 if db==nil||tenant==""||projectID==""||workspaceID==""||len(visible)==0{return out,false,nil}
 ids:=make([]string,0,len(visible))
 seen:=map[string]bool{}
 for _,t:=range visible {
  if t.ID==""||t.WorkspaceID!=tenant||t.ProjectID==nil||*t.ProjectID!=projectID||
   t.ProjectWorkspaceID==nil||*t.ProjectWorkspaceID!=workspaceID||seen[t.ID]{continue}
  seen[t.ID]=true
  ids=append(ids,t.ID)
 }
 if len(ids)==0{return out,false,nil}
 if len(ids)>qaSnapshotTaskCap {ids=ids[:qaSnapshotTaskCap]}
 marks:=strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")
 args:=make([]any,0,len(ids)+8)
 args=append(args,tenant,projectID,workspaceID)
 for _,id:=range ids{args=append(args,id)}
 args=append(args,tenant,tenant,tenant,tenant,qaTimelineEventCap+1)
 query:=`WITH scoped AS (
 SELECT id FROM tasks WHERE workspace_id=? AND project_id=? AND project_workspace_id=?
 AND id IN (`+marks+`)
 )
 SELECT sequence,id,event_type,aggregate_type,aggregate_id,task_id,request_id,trace_id,occurred_at FROM (
  SELECT e.sequence,e.id,e.event_type,e.aggregate_type,e.aggregate_id,
   t.id AS task_id,e.request_id,e.trace_id,e.occurred_at
  FROM events e
  JOIN tasks t ON e.aggregate_type='task' AND e.aggregate_id=t.id
  JOIN scoped s ON s.id=t.id
  WHERE e.workspace_id=?
  UNION ALL
  SELECT e.sequence,e.id,e.event_type,e.aggregate_type,e.aggregate_id,
   r.task_id,e.request_id,e.trace_id,e.occurred_at
  FROM events e
  JOIN agent_worker_runs r ON e.aggregate_type='agent_worker_run' AND e.aggregate_id=r.id
  JOIN scoped s ON s.id=r.task_id
  JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
  WHERE e.workspace_id=? AND r.workspace_id=? AND t.workspace_id=?
 )
 ORDER BY occurred_at DESC,sequence DESC LIMIT ?`
 rows,err:=db.QueryContext(ctx,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("load scoped QA event timeline: %w",err)}
 defer rows.Close()
 truncated:=false
 for rows.Next(){
  var seq,occurred int64
  var eventID,eventType,kind,aggregateID,taskID string
  var requestID,traceID sql.NullString
  if err:=rows.Scan(&seq,&eventID,&eventType,&kind,&aggregateID,&taskID,&requestID,&traceID,&occurred);err!=nil{
   return nil,false,err
  }
  if len(out)>=qaTimelineEventCap{truncated=true;break}
  if !qaKnownEventType(eventType)||occurred<=0{continue}
  // Only the two known aggregate kinds are included by the SQL above.
  e:=qaTimelineEvent{
   EventRef:qaOpaqueRef("event",eventID),
   TaskRef:qaOpaqueRef("task",taskID),
   EventType:eventType,Severity:qaEventSeverity(eventType),OccurredAt:occurred,
  }
  if kind=="agent_worker_run"{e.RunRef=qaOpaqueRef("run",aggregateID)}
  if requestID.Valid{e.RequestRef=qaOpaqueRef("request",requestID.String)}
  if traceID.Valid{e.TraceRef=qaOpaqueRef("trace",traceID.String)}
  out=append(out,e)
 }
 if err:=rows.Err();err!=nil{return nil,false,err}
 return out,truncated,nil
}
