package localai

import (
 "context"
 "errors"
 "fmt"
 "syscall"
 "time"
)

// A model may start successfully and then fail to persist its PID because
// its request is cancelled or SQLite rejects the ownership update. Bind the
// original launch generation to a durable orphaned row FIRST, before any
// potentially slow verification or termination. Never claim memory or VRAM
// reclaimed merely because a signal was sent.
func (s *RuntimeSupervisor) reconcileUnrecordedLaunch(parent context.Context, inst RuntimeInstance, pid int, launchRecordErr error)error{
 if pid<=1{return errors.Join(launchRecordErr,errors.New("managed launch returned invalid process PID"))}
 detached:=context.WithoutCancel(parent)
 persistCtx,persistCancel:=context.WithTimeout(detached,3*time.Second)
 now:=s.clock.UnixMilli()
 const orphanReason="managed model launched but PID ownership write failed; operator reconciliation required"
 result,err:=s.db.ExecContext(persistCtx,`UPDATE local_runtime_instances
 SET pid=?,status='orphaned',failure_reason=?,updated_at=?,revision=revision+1
 WHERE id=? AND status='starting' AND pid IS NULL AND revision=? AND process_fingerprint=?`,
 pid,orphanReason,now,inst.ID,inst.Revision,inst.ProcessFingerprint)
 if err!=nil{
  persistCancel()
  return errors.Join(launchRecordErr,fmt.Errorf("failed to retain unrecorded managed model process ownership: %w",err))
 }
 count,err:=result.RowsAffected()
 persistCancel()
 if err!=nil||count!=1{
  return errors.Join(launchRecordErr,errors.New("managed model launch state changed before recovery could bind its PID; operator reconciliation required"))
 }

 // Once durable ownership is preserved, a separate bounded window may
 // verify the newly launched process and request graceful termination.
 // An unverified or still-alive process remains orphaned in SQLite.
 cleanupCtx,cancel:=context.WithTimeout(detached,5*time.Second)
 defer cancel()
 if s.processes.Alive(pid){
  inst.PID=pid
  if verifyErr:=s.VerifyIdentity(cleanupCtx,inst);verifyErr==nil{
   _=s.processes.Signal(pid,syscall.SIGTERM)
   deadline:=time.Now().Add(2*time.Second)
   for s.processes.Alive(pid)&&time.Now().Before(deadline)&&cleanupCtx.Err()==nil{
    time.Sleep(50*time.Millisecond)
   }
  }
 }
 if s.processes.Alive(pid){
  return errors.Join(launchRecordErr,errors.New("managed model launch PID remains orphaned; operator reconciliation required before retry"))
 }

 // An independently confirmed dead process may transition orphaned->failed.
 // If this write fails the durable orphaned PID remains visible, not lost.
 finalizeCtx,finalizeCancel:=context.WithTimeout(detached,3*time.Second)
 defer finalizeCancel()
 res,err:=s.db.ExecContext(finalizeCtx,`UPDATE local_runtime_instances
  SET status='failed',failure_reason='managed runtime launch failed and child process exited',
  updated_at=?,revision=revision+1
  WHERE id=? AND pid=? AND status='orphaned' AND revision=? AND process_fingerprint=?`,
  s.clock.UnixMilli(),inst.ID,pid,inst.Revision+1,inst.ProcessFingerprint)
 if err!=nil{
  return errors.Join(launchRecordErr,fmt.Errorf("managed child exited but orphan-state finalization failed: %w",err))
 }
 affected,err:=res.RowsAffected()
 if err!=nil||affected!=1{
  return errors.Join(launchRecordErr,errors.New("managed child exited but launch owner changed during failure finalization"))
 }
 return launchRecordErr
}
