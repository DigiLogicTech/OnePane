package projectorchestrator

import "github.com/DigiLogicTech/OnePane/internal/task"

// TaskGraphProgress is a derived, read-only observation, not authority to
// restart a Task, substitute a model or claim hardware has executed work.
type TaskGraphProgress struct{
 Total int `json:"total"`
 Admissible int `json:"admissible"`
 Running int `json:"running"`
 Waiting int `json:"waiting"`
 Verifying int `json:"verifying"`
 NeedsAttention int `json:"needs_attention"`
 Complete int `json:"complete"`
}

func isTaskDependencyNeedsAttention(state task.State)bool{
 switch state{
 case task.StateFailed,task.StateCancelled,task.StateBlocked,
  task.StatePaused,task.StateWaitingApproval,task.StateCancelRequested,
  task.StateCancelling,task.StateCompensating:
  return true
 default:return false
 }
}
func containsNodeKey(keys []string,key string)bool{
 for _,k:=range keys{if k==key{return true}}
 return false
}

// Nodes are in the approved graph's stable topological order. Propagate
// pending operator action through the graph without changing Task state.
// Independent branches must remain admissible even if one branch fails.
func evaluateTaskGraph(graph *TaskGraph)(TaskGraphProgress,string){
 progress:=TaskGraphProgress{}
 if graph==nil{return progress,"unavailable"}
 progress.Total=len(graph.Nodes)
 seen:=map[string]TaskGraphNode{}
 for i:=range graph.Nodes{
  n:=&graph.Nodes[i]
  for _,pred:=range n.DependsOn{
   prior,exists:=seen[pred]
   if exists && prior.Readiness=="needs_attention" &&
    !containsNodeKey(n.FailedOrIntervenedOn,pred){
    n.FailedOrIntervenedOn=append(n.FailedOrIntervenedOn,pred)
   }
  }
  switch{
  case n.Archived:
   n.Readiness="needs_attention"
   n.NextAction="Archived Task cannot satisfy the approved graph; operator must review restoration or replacement."
  case n.State==task.StateFailed||n.State==task.StateCancelled:
   n.Readiness="needs_attention"
   n.NextAction="Review failed/cancelled Task evidence before separately authorising a recovery or replacement."
  case n.State==task.StateBlocked:
   n.Readiness="needs_attention"
   n.NextAction="Review blocked Task, interrupted execution and any unknown side effects before resuming."
  case len(n.FailedOrIntervenedOn)>0:
   n.Readiness="needs_attention"
   n.NextAction="Prerequisite needs operator review; do not automatically retry or skip a hard dependency."
  case len(n.BlockedBy)>0:
   n.Readiness="waiting_prerequisites"
   n.NextAction="Wait for each hard predecessor to complete with independently verified evidence."
  case n.State==task.StateComplete:
   n.Readiness="complete"
   n.NextAction="No further Task admission required."
  case n.State==task.StateCreated||n.State==task.StateReady:
   n.Readiness="admission_eligible"
   n.NextAction="Existing Task Worker may attempt governed local-first admission; no model or hardware execution is implied."
  case n.State==task.StateRunning:
   n.Readiness="running"
   n.NextAction="Await Task execution and independent completion verification."
  case n.State==task.StateCompletionRequested||n.State==task.StateVerifying:
   n.Readiness="verifying"
   n.NextAction="Await independent evidence and checkpoint verification."
  case n.State==task.StateWaitingDependency:
   n.Readiness="waiting_resources"
   n.NextAction="Inspect persisted Worker continuation; model or toolchain may still be unavailable."
  case n.State==task.StatePaused||n.State==task.StateWaitingApproval||
   n.State==task.StateCancelRequested||n.State==task.StateCancelling||
   n.State==task.StateCompensating:
   n.Readiness="needs_attention"
   n.NextAction="Operator must review Task state, approvals or cancellation before changing execution."
  default:
   n.Readiness="needs_attention"
   n.NextAction="Task state is not eligible for automatic graph progression."
  }
  switch n.Readiness{
  case "admission_eligible":progress.Admissible++
  case "running":progress.Running++
  case "waiting_prerequisites","waiting_resources":progress.Waiting++
  case "verifying":progress.Verifying++
  case "needs_attention":progress.NeedsAttention++
  case "complete":progress.Complete++
  }
  seen[n.Key]=*n
 }
 switch{
 case progress.Total==0:return progress,"unavailable"
 case progress.Complete==progress.Total:return progress,"completed"
 case progress.NeedsAttention>0:return progress,"needs_attention"
 case progress.Admissible>0||progress.Running>0||progress.Verifying>0:
  return progress,"work_available"
 default:return progress,"waiting"
 }
}
