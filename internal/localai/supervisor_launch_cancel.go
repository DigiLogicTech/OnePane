package localai

import (
 "context"
 "errors"
 "time"
)

// A cancelled caller cannot commit an unlaunched model's failure using its
// own dead context. Use a short detached write, scoped to the exact original
// starting revision and command fingerprint. No process was created yet.
func (s *RuntimeSupervisor) failUnlaunchedManagedRuntime(ctx context.Context, inst RuntimeInstance)error{
 if inst.ID==""||inst.Status!=RuntimeStarting||inst.PID>1||inst.ProcessFingerprint==""{
  return errors.New("unlaunched model ownership evidence unavailable")
 }
 persistCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),3*time.Second)
 defer cancel()
 res,err:=s.db.ExecContext(persistCtx,`UPDATE local_runtime_instances
  SET status='failed',
      failure_reason='managed runtime launch cancelled or could not create child',
      updated_at=?,revision=revision+1
  WHERE id=? AND status='starting' AND pid IS NULL
    AND revision=? AND process_fingerprint=?`,
    s.clock.UnixMilli(),inst.ID,inst.Revision,inst.ProcessFingerprint)
 if err!=nil{return err}
 affected,err:=res.RowsAffected()
 if err!=nil{return err}
 if affected!=1{
  return errors.New("unlaunched managed model generation changed; cleanup withheld pending reconciliation")
 }
 return nil
}
