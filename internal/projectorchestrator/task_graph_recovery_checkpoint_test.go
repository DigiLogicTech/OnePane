package projectorchestrator

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestRecoveryCheckpointKeepsIndependentBranchesAndNoAutoSideEffectRetries(t *testing.T){
 graph:=TaskGraph{ID:"graph",ProjectID:"project",ManifestSHA256:"immutable",
  Nodes:[]TaskGraphNode{
   {Key:"world",TaskID:"world-task",ProjectWorkspaceID:"world",State:task.StateFailed},
   {Key:"assets",TaskID:"assets-task",ProjectWorkspaceID:"assets",State:task.StateCreated},
   {Key:"story",TaskID:"story-task",ProjectWorkspaceID:"story",
    State:task.StateCreated,DependsOn:[]string{"world"},
    BlockedBy:[]string{"world"},FailedOrIntervenedOn:[]string{"world"}},
   {Key:"build",TaskID:"build-task",ProjectWorkspaceID:"build",
    State:task.StateRunning,DependsOn:[]string{"story","assets"},
    BlockedBy:[]string{"story"}},
  }}
 graph.Progress,graph.Status=evaluateTaskGraph(&graph)
 proof:=recoveryCheckpointForGraph(graph)
 if proof.Schema!="onepane.task-graph-recovery-checkpoint/v1"||
  proof.AutomaticRetryAllowed||!proof.NeedsOperatorReview||
  proof.ManifestSHA256!="immutable"||len(proof.Nodes)!=4||
  len(proof.ObservationSHA256)!=64{
  t.Fatalf("graph recovery misclassified: %+v",proof)
 }
 if proof.Nodes[0].RecoveryClass!="operator_review"||
  proof.Nodes[1].RecoveryClass!="existing_worker_admission"||
  !proof.Nodes[1].CanExistingWorkerAdmit||
  proof.Nodes[2].RecoveryClass!="operator_review"||
  proof.Nodes[3].RecoveryClass!="operator_review"{
  t.Fatalf("unsafe side-effect replay or blocked independent work: %+v",proof.Nodes)
 }
 copy:=recoveryCheckpointForGraph(graph)
 if proof.ObservationSHA256!=copy.ObservationSHA256{
  t.Fatal("same persisted recovery snapshot gave inconsistent checkpoint hash")
 }
 graph.Nodes[1].State=task.StatePaused
 graph.Progress,graph.Status=evaluateTaskGraph(&graph)
 changed:=recoveryCheckpointForGraph(graph)
 if changed.ObservationSHA256==proof.ObservationSHA256||
  changed.Nodes[1].CanExistingWorkerAdmit{
  t.Fatal("stale checkpoint remained valid after Task state change")
 }
}

func TestRecoveryCheckpointRejectsDependencyScopeDriftAndUnverifiedCompletion(t *testing.T){
 graph:=TaskGraph{ID:"graph",ProjectID:"project",ManifestSHA256:"manifest",
  Nodes:[]TaskGraphNode{
   {Key:"unsafe",TaskID:"t1",State:task.StateReady,
    ScopeDrift:true,BlockedBy:[]string{"unscoped_dependency"},
    FailedOrIntervenedOn:[]string{"unscoped_dependency"}},
   {Key:"missing",TaskID:"t2",State:task.StateComplete,
    Archived:true},
   {Key:"pending",TaskID:"t3",State:task.StateVerifying},
   {Key:"waiting",TaskID:"t4",State:task.StateWaitingDependency},
  }}
 graph.Progress,graph.Status=evaluateTaskGraph(&graph)
 checkpoint:=recoveryCheckpointForGraph(graph)
 for _,n:=range checkpoint.Nodes[:2]{
  if n.RecoveryClass!="operator_review"||n.CanExistingWorkerAdmit{
   t.Fatalf("graph recovery trusted unsafe Task scope: %+v",n)
  }
 }
 if checkpoint.Nodes[2].RecoveryClass!="observe_existing_attempt"||
  checkpoint.Nodes[3].RecoveryClass!="observe_existing_attempt"||
  checkpoint.AutomaticRetryAllowed{
  t.Fatalf("uncertain Task side effects were automatically retried: %+v",checkpoint)
 }
}
