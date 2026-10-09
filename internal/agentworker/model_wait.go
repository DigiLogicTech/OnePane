package agentworker

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// modelWaitRecord is persisted in the existing agent_worker_runs continuation.
// It deliberately carries no model substitution or authority grant. A worker
// re-evaluates the *original* Task routing on wake after a resource pause.
type modelWaitRecord struct {
 Attempt int `json:"attempt"`
 RetryAtMS int64 `json:"retry_at_ms"`
 Reason string `json:"reason"`
}
type modelWaitEnvelope struct {
 ModelWait *modelWaitRecord `json:"model_wait,omitempty"`
}

func decodeModelWait(raw json.RawMessage) *modelWaitRecord {
 var state modelWaitEnvelope
 if len(raw)==0||json.Unmarshal(raw,&state)!=nil{return nil}
 if state.ModelWait==nil||state.ModelWait.Attempt<1||state.ModelWait.RetryAtMS<1{return nil}
 return state.ModelWait
}

func modelWaitDelayMS(attempt int) int64 {
 // 15s, 30s, 60s, 120s, 240s, then capped at 5 min.
 if attempt<1{attempt=1}
 if attempt>6{attempt=6}
 delay:=int64(15000) << uint(attempt-1)
 if delay>300000{return 300000}
 return delay
}

// Only resource availability and model-admission work can initiate a model
// wait. Permanently incompatible, over-budget, disallowed, or too-small
// candidates remain explicit blockers requiring an operator decision.
func shouldWaitForLocalModel(t task.Task, routeErr error, rejected []scheduler.Rejection) bool {
 if t.ProjectWorkspaceID==nil||!errors.Is(routeErr,scheduler.ErrNoEligibleCandidate){return false}
 if len(rejected)==0{return true} // No qualified model installed/registered yet.
 for _,c:=range rejected{
  switch strings.TrimSpace(c.Reason) {
  case "candidate is not schedulable","candidate is degraded","candidate is untested",
   "not_selected_by_request", // A pinned candidate may not be registered yet.
   "remote candidate disallowed by workspace policy":
   return true
  }
 }
 return false
}

func (s *Service) waitForLocalModel(ctx context.Context, run Run, res TickResult, reason string) TickResult {
 attempt:=1
 if previous:=decodeModelWait(run.Continuation);previous!=nil{
  attempt=previous.Attempt+1
 }
 if attempt>1000000{attempt=1000000}
 retryAt:=s.clock.UnixMilli()+modelWaitDelayMS(attempt)
 state,_:=json.Marshal(modelWaitEnvelope{ModelWait:&modelWaitRecord{
  Attempt:attempt,RetryAtMS:retryAt,Reason:boundedString(reason,512),
 }})
 if err:=s.updateRun(ctx,run.ID,run.Revision,RunWaiting,state,nil,0,0,nil,nil,strPtr(reason));err!=nil{
  return failedResult(res,err)
 }
 current,err:=s.tasks.Get(ctx,run.TaskID)
 if err!=nil{return failedResult(res,err)}
 actor:=WorkerPrincipal
 if current.State!=task.StateRunning {
  return failedResult(res,fmt.Errorf("cannot wait for model from Task state %s",current.State))
 }
 if _,err:=s.tasks.WaitDependency(ctx,task.TransitionCommand{
  TaskID:current.ID,ExpectedRevision:current.Revision,
  ActorPrincipalID:&actor,Reason:"awaiting an eligible local model; cloud substitution disabled",
 });err!=nil{
  return failedResult(res,err)
 }
 _=s.journal(ctx,run.ID,"route","waiting",nil,nil,nil,nil,
  map[string]any{"kind":"model_resources","retry_after_ms":retryAt,"wait_attempt":attempt})
 res.Status="waiting_model"
 res.Error=reason
 return res
}
