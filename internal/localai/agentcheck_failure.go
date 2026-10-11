package localai

import (
 "context"
 "database/sql"
 "errors"
 "io/fs"
 "time"
)

// Agent Check observations are machine-coded at the failure site, never
// inferred from arbitrary provider messages or client-supplied abort reasons.
// No exception string, prompt, response, request ID or filesystem path is saved.
func agentCheckCategory(stage string, cause error) string {
 if stage=="session_abort" {return "abort_requested"} // cause cannot establish why
 if errors.Is(cause,context.Canceled){return "cancelled"}
 if errors.Is(cause,context.DeadlineExceeded){return "deadline_exceeded"}
 if errors.Is(cause,sql.ErrNoRows)||errors.Is(cause,fs.ErrNotExist){return "not_found"}
 switch stage {
 case "response_validation":return "invalid_response"
 case "turn_store","completion_persist":return "storage_failure"
 case "inference_dispatch","runtime_acquire","runtime_release","runtime_unload":return "execution_failure"
 default:return "unknown"
 }
}
func knownAgentCheckStage(s string)bool{
 switch s{
 case "runtime_acquire","deployment_read","model_read","inference_dispatch",
  "response_validation","turn_store","completion_validation",
  "completion_persist","runtime_release","runtime_unload","session_abort":return true
 default:return false
 }
}

// Best-effort, bounded write: a diagnostics failure must never replace the
// real inference or cleanup error. The original request may be cancelled,
// so persist machine-coded metadata using a separately bounded context.
// No new event is claimed unless the transaction really commits.
func(s *Service)recordAgentCheckFailure(_ context.Context,sess TestbedSession,stage string,cause error){
 if cause==nil||!knownAgentCheckStage(stage)||sess.ID==""||sess.DeploymentID==""||s.db==nil{return}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second)
 defer cancel()
 _,_=s.db.ExecContext(ctx,`INSERT INTO model_agentcheck_failure_observations
 (session_id,deployment_id,stage,category,observed_at) VALUES(?,?,?,?,?)`,
 sess.ID,sess.DeploymentID,stage,agentCheckCategory(stage,cause),s.clock.UnixMilli())
}
