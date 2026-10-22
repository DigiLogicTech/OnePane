package localai

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
)

// An HTTP response is not proof that the original recovered model still
// owns its durable SQLite generation. Recover must call VerifyIdentity
// again after the health response and only then request this exact CAS.
// Other actors can still change the row before the write; never promote a
// replacement, stopped model or unverified process to healthy.
func (s *RuntimeSupervisor) confirmRecoveredHealthy(ctx context.Context, inst RuntimeInstance, health map[string]any) error {
 if inst.ID==""||inst.PID<=1||inst.Revision<=0||inst.ProcessFingerprint=="" {
  return errors.New("recovered managed runtime ownership evidence unavailable")
 }
 switch inst.Status {
 case RuntimeHealthy,RuntimeStarting:
 default:
  return errors.New("recovered managed runtime is not in an active state")
 }
 raw,err:=json.Marshal(health)
 if err!=nil{return fmt.Errorf("marshal recovered model readiness: %w",err)}
 now:=s.clock.UnixMilli()
 result,err:=s.db.ExecContext(ctx,`UPDATE local_runtime_instances
  SET status='healthy',health_json=?,last_seen_at=?,failure_reason=NULL,
      updated_at=?,revision=revision+1
  WHERE id=? AND revision=? AND pid=? AND process_fingerprint=? AND status=?`,
  string(raw),now,now,inst.ID,inst.Revision,inst.PID,inst.ProcessFingerprint,inst.Status)
 if err!=nil{return err}
 affected,err:=result.RowsAffected()
 if err!=nil{return err}
 if affected!=1{return errors.New("recovered model ownership changed before readiness commit")}
 return nil
}
