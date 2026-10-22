package localai

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
)

// A late readiness response is not authority to resurrect a stopped,
// orphaned or replaced model. Bind the transition to the exact starting
// instance generation, managed PID and approved model command fingerprint.
func (s *RuntimeSupervisor) confirmStartingHealthy(ctx context.Context, inst RuntimeInstance, health map[string]any) error {
 if inst.ID==""||inst.PID<=1||inst.Status!=RuntimeStarting||inst.ProcessFingerprint==""{
  return errors.New("managed model startup ownership evidence unavailable")
 }
 raw,err:=json.Marshal(health)
 if err!=nil{return fmt.Errorf("marshal managed runtime readiness: %w",err)}
 now:=s.clock.UnixMilli()
 result,err:=s.db.ExecContext(ctx,`UPDATE local_runtime_instances
  SET status='healthy',health_json=?,last_seen_at=?,updated_at=?,revision=revision+1
  WHERE id=? AND status='starting' AND revision=? AND pid=? AND process_fingerprint=?`,
  string(raw),now,now,inst.ID,inst.Revision,inst.PID,inst.ProcessFingerprint)
 if err!=nil{return err}
 n,err:=result.RowsAffected()
 if err!=nil{return err}
 if n!=1{return errors.New("managed runtime startup changed while probing health; stale readiness rejected")}
 return nil
}

// An unhealthy/dead child likewise cannot mark an unrelated replacement
// as failed after another operation has moved the instance forward.
func (s *RuntimeSupervisor) failStartingInstance(ctx context.Context, inst RuntimeInstance, reason string) error {
 if inst.ID==""||inst.PID<=1||inst.Status!=RuntimeStarting||inst.ProcessFingerprint==""{
  return errors.New("managed runtime startup ownership evidence unavailable for failure")
 }
 now:=s.clock.UnixMilli()
 result,err:=s.db.ExecContext(ctx,`UPDATE local_runtime_instances
  SET status='failed',failure_reason=?,updated_at=?,revision=revision+1
  WHERE id=? AND status='starting' AND revision=? AND pid=? AND process_fingerprint=?`,
  reason,now,inst.ID,inst.Revision,inst.PID,inst.ProcessFingerprint)
 if err!=nil{return err}
 n,err:=result.RowsAffected()
 if err!=nil{return err}
 if n!=1{return errors.New("managed runtime startup changed while probing health; stale failure rejected")}
 return nil
}
