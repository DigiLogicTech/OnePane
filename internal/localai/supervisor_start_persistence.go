package localai

import (
 "context"
 "errors"
 "fmt"
 "syscall"
 "time"
)

// A model can spawn successfully but its "starting" SQLite row can fail to
// record the PID because the user cancels the initiating request or SQLite
// rejects the write. Never discard knowledge of a possibly resident process.
// This recovery runs synchronously, with a short independent deadline; it
// does not introduce a background worker or permission to signal unknown PIDs.
func (s *RuntimeSupervisor) reconcileUnrecordedLaunch(parent context.Context, inst RuntimeInstance, pid int, launchRecordErr error)error{
 if pid<=1{return errors.Join(launchRecordErr,errors.New("managed launch returned invalid process PID"))}
 cleanupCtx,cancel:=context.WithTimeout(context.WithoutCancel(parent),5*time.Second)
 defer cancel()
 verified:=false
 if s.processes.Alive(pid){
  inst.PID=pid
  // A PID alone is not authority to signal. Validate the original managed
  // executable/model/loopback-port identity before sending any signal.
  if err:=s.VerifyIdentity(cleanupCtx,inst);err==nil{
   verified=true
   _=s.processes.Signal(pid,syscall.SIGTERM)
   // A successful signal can be asynchronous on Linux. Wait briefly for
   // confirmation rather than marking reclaimed RAM/VRAM prematurely.
   deadline:=time.Now().Add(2*time.Second)
   for s.processes.Alive(pid)&&time.Now().Before(deadline)&&cleanupCtx.Err()==nil{
    time.Sleep(50*time.Millisecond)
   }
  }
 }
 live:=s.processes.Alive(pid)
 status:="failed"
 reason:="managed runtime launch could not be persisted; process exited"
 if live{
  status="orphaned"
  reason="managed runtime launch PID persistence failed; process ownership requires operator reconciliation"
  if !verified{
   reason="managed runtime launch PID unverified; signal refused; operator reconciliation required"
  }
 }
 // Re-bind the exact original launch row using a fresh, bounded context,
 // never silently overwrite an independently changed state or replacement.
 now:=s.clock.UnixMilli()
 result,err:=s.db.ExecContext(cleanupCtx,`UPDATE local_runtime_instances
 SET pid=?,status=?,failure_reason=?,updated_at=?,revision=revision+1
 WHERE id=? AND status='starting' AND pid IS NULL AND revision=? AND process_fingerprint=?`,pid,status,reason,now,inst.ID,inst.Revision,inst.ProcessFingerprint)
 if err!=nil{
  return errors.Join(launchRecordErr,fmt.Errorf("failed to retain unrecorded managed model process ownership: %w",err))
 }
 n,err:=result.RowsAffected()
 if err!=nil||n!=1{
  return errors.Join(launchRecordErr,fmt.Errorf("managed model launch state changed before recovery could bind its PID; operator reconciliation required"))
 }
 if live{
  return errors.Join(launchRecordErr,errors.New("managed model launch ownership is orphaned; retry and model swap withheld pending operator reconciliation"))
 }
 return launchRecordErr
}
