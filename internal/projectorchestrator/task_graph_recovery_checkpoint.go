package projectorchestrator

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// RecoveryCheckpoint is an observation of durable Task and dependency state,
// not a restart token, execution lease, or permission to replay side effects.
type RecoveryCheckpoint struct {
 Schema string `json:"schema"`
 GraphID string `json:"graph_id"`
 ProjectID string `json:"project_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 ObservationSHA256 string `json:"observation_sha256"`
 GraphStatus string `json:"graph_status"`
 NeedsOperatorReview bool `json:"needs_operator_review"`
 AutomaticRetryAllowed bool `json:"automatic_retry_allowed"`
 Nodes []RecoveryCheckpointNode `json:"nodes"`
}
type RecoveryCheckpointNode struct {
 Key string `json:"key"`
 TaskID string `json:"task_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 State task.State `json:"state"`
 Readiness string `json:"readiness"`
 AttemptHealth string `json:"attempt_health,omitempty"`
 ExternalEffectHealth string `json:"external_effect_health,omitempty"`
 RecoveryClass string `json:"recovery_class"`
 Dependencies []string `json:"dependencies"`
 Blockers []string `json:"blockers"`
 OperatorAction string `json:"operator_action"`
 CanExistingWorkerAdmit bool `json:"can_existing_worker_admit"`
}

// Recovery recommendations never initiate new Tasks, alter state, change
// approved model pins or assert that unknown side effects were rolled back.
func recoveryCheckpointForGraph(graph TaskGraph) RecoveryCheckpoint {
 out:=RecoveryCheckpoint{Schema:"onepane.task-graph-recovery-checkpoint/v1",
  GraphID:graph.ID,ProjectID:graph.ProjectID,ManifestSHA256:graph.ManifestSHA256,
  GraphStatus:graph.Status,AutomaticRetryAllowed:false,
  Nodes:make([]RecoveryCheckpointNode,0,len(graph.Nodes))}
 for _,n:=range graph.Nodes{
  current:=RecoveryCheckpointNode{
   Key:n.Key,TaskID:n.TaskID,ProjectWorkspaceID:n.ProjectWorkspaceID,
   State:n.State,Readiness:n.Readiness,AttemptHealth:n.AttemptHealth,
   ExternalEffectHealth:n.ExternalEffectHealth,
   Dependencies:append([]string{},n.DependsOn...),
   Blockers:append([]string{},n.BlockedBy...),
   OperatorAction:n.NextAction,
  }
  switch n.Readiness{
  case "complete":
   current.RecoveryClass="already_verified"
  case "admission_eligible":
   if len(n.BlockedBy)==0&&!n.ScopeDrift&&!n.Archived{
    current.RecoveryClass="existing_worker_admission"
    current.CanExistingWorkerAdmit=true
   }else{
    current.RecoveryClass="operator_review"
    out.NeedsOperatorReview=true
   }
  case "waiting_prerequisites":
   current.RecoveryClass="wait_for_dependencies"
  case "waiting_resources","running","verifying":
   current.RecoveryClass="observe_existing_attempt"
  default:
   current.RecoveryClass="operator_review"
   out.NeedsOperatorReview=true
  }
  if n.ScopeDrift||n.DependencyDrift||n.Archived||len(n.FailedOrIntervenedOn)>0{
   current.RecoveryClass="operator_review"
   current.CanExistingWorkerAdmit=false
   out.NeedsOperatorReview=true
  }
  if current.OperatorAction==""{
   current.OperatorAction="Inspect the existing Task record before taking action."
  }
  out.Nodes=append(out.Nodes,current)
 }
 // The checkpoint fingerprint changes when the underlying persisted
 // states/blockers change. It is NOT a proof of a completed tool execution.
 raw,_:=json.Marshal(struct{
  ProjectID string `json:"project_id"`
  GraphID string `json:"graph_id"`
  ManifestSHA256 string `json:"manifest_sha256"`
  GraphStatus string `json:"graph_status"`
  Nodes []RecoveryCheckpointNode `json:"nodes"`
 }{out.ProjectID,out.GraphID,out.ManifestSHA256,out.GraphStatus,out.Nodes})
 digest:=sha256.Sum256(raw)
 out.ObservationSHA256=hex.EncodeToString(digest[:])
 return out
}

func(s *Service) TaskGraphRecoveryCheckpoint(ctx context.Context,projectID,graphID string)(
 RecoveryCheckpoint,error){
 // TaskGraph reads the canonical persisted node/dependency state in a
 // consistent, project-scoped SQLite snapshot.
 graph,err:=s.TaskGraph(ctx,projectID,graphID)
 if err!=nil{return RecoveryCheckpoint{},err}
 return recoveryCheckpointForGraph(graph),nil
}
