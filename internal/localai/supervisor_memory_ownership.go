package localai

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
)

// Only this exact bounded pre-spawn failure transition proves that a
// managed child was never created. Other failed/NULL-PID records can have
// unknown external process effects after a daemon or SQLite crash.
const managedNoChildFailureReason = "managed runtime launch cancelled or could not create child"

// The same exact pre-spawn proof can persist after an explicit Stop
// transitions a never-launched failed model to stopped. A generic stopped
// record without PID is not proof its parent/child engine exited.
func confirmedNoChildWithoutPID(inst RuntimeInstance)bool{
 return (inst.Status==RuntimeFailed||inst.Status==RuntimeStopped) &&
  inst.PID==0 && !inst.pidRecorded && inst.StartedAt==nil &&
  inst.FailureReason!=nil && *inst.FailureReason==managedNoChildFailureReason
}
func confirmedUnlaunchedModelFailure(inst RuntimeInstance)bool{
 return inst.Status==RuntimeFailed&&confirmedNoChildWithoutPID(inst)
}

// A stopped/failed row does not establish freed memory. When any abnormal
// managed model from the same Node is live or has uncertain prelaunch
// ownership, refuse admission across backends (CPU, CUDA, Vulkan, Colibri).
// This never signals or changes the existing process; reconciliation remains
// an explicit operator action, with separate PID identity checks.
func (s *RuntimeSupervisor) refuseUnreconciledMemoryOwners(ctx context.Context,nodeID,target string)error{
 if s==nil||s.db==nil||s.processes==nil{
  return errors.New("local runtime memory ownership cannot be verified")
 }
 rows,err:=s.db.QueryContext(ctx,`SELECT deployment_id,status,pid,failure_reason,started_at
  FROM local_runtime_instances
  WHERE node_id=? AND deployment_id<>?
    AND (status IN ('starting','draining','orphaned','failed','stopped')
       OR (status IN ('healthy','busy') AND COALESCE(pid,0)<=1))
  ORDER BY deployment_id LIMIT 513`,nodeID,target)
 if err!=nil{return fmt.Errorf("inspect managed memory ownership: %w",err)}
 type candidate struct{id,status string;pid sql.NullInt64;reason sql.NullString;started sql.NullInt64}
 found:=make([]candidate,0)
 for rows.Next(){
  var c candidate
  if err=rows.Scan(&c.id,&c.status,&c.pid,&c.reason,&c.started);err!=nil{break}
  found=append(found,c)
 }
 if err==nil{err=rows.Err()}
 if closeErr:=rows.Close();err==nil{err=closeErr}
 if err!=nil{return fmt.Errorf("inspect managed memory ownership: %w",err)}
 if len(found)>512{return errors.New("managed model ownership registry exceeds bounded admission check")}
 for _,c:=range found{
  // A persisted non-NULL PID must have been returned by a real managed
  // child launch. Zero/negative/service PIDs are corrupt evidence, not
  // proof that a failed or stopped engine surrendered RAM/VRAM.
  if c.pid.Valid&&c.pid.Int64<=1{
   return fmt.Errorf("managed model %s has invalid persisted PID; operator reconciliation required",c.id)
  }
  if c.pid.Valid&&c.pid.Int64>1{
   if int64(int(c.pid.Int64))!=c.pid.Int64{
    return fmt.Errorf("managed model %s has invalid process identity; operator reconciliation required",c.id)
   }
   if s.processes.Alive(int(c.pid.Int64)){
    return fmt.Errorf("managed model %s remains %s with a live process; stop/reconcile before allocating more memory",c.id,c.status)
   }
   continue
  }
  // Healthy/busy with missing PID has no trustworthy process ownership,
  // even when a socket or database row once reported it ready.
  if c.status=="healthy"||c.status=="busy"{
   return fmt.Errorf("managed model %s has unverified %s PID ownership; operator reconciliation required",c.id,c.status)
  }
  // A NULL PID plus failed state without an exact pre-spawn proof could
  // have followed an interrupted OS launch before PID persistence.
  // A stopped row with NULL PID is as ambiguous as a failed one: the
  // daemon could have exited before recording a spawned model's PID.
  // Permit only the exact no-child SQL proof, with no start timestamp.
  if (c.status=="failed"||c.status=="stopped")&&(!c.reason.Valid||
    c.reason.String!=managedNoChildFailureReason||c.started.Valid){
   return fmt.Errorf("managed model %s has unverified %s launch ownership; operator reconciliation required",c.id,c.status)
  }
  if c.status=="starting"||c.status=="draining"||c.status=="orphaned"{
   return fmt.Errorf("managed model %s has uncertain %s process ownership; operator reconciliation required",c.id,c.status)
  }
 }
 return nil
}
