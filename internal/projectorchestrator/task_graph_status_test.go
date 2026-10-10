package projectorchestrator

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestTaskGraphReadinessPropagatesFailureButPreservesIndependentWork(t *testing.T){
 graph:=&TaskGraph{Nodes:[]TaskGraphNode{
  {Key:"world",State:task.StateFailed,DependsOn:[]string{}},
  {Key:"assets",State:task.StateCreated,DependsOn:[]string{}},
  {Key:"story",State:task.StateCreated,DependsOn:[]string{"world"},
   BlockedBy:[]string{"world"},FailedOrIntervenedOn:[]string{"world"}},
  {Key:"final",State:task.StateCreated,DependsOn:[]string{"story","assets"},
   BlockedBy:[]string{"story","assets"}},
 }}
 progress,status:=evaluateTaskGraph(graph)
 if status!="needs_attention"||progress.Total!=4||progress.NeedsAttention!=3||
  progress.Admissible!=1{
  t.Fatalf("failure propagated incorrectly status=%s progress=%+v",status,progress)
 }
 nodes:=map[string]TaskGraphNode{}
 for _,n:=range graph.Nodes{nodes[n.Key]=n}
 if nodes["assets"].Readiness!="admission_eligible"||
  nodes["story"].Readiness!="needs_attention"||
  nodes["final"].Readiness!="needs_attention"||
  !containsNodeKey(nodes["final"].FailedOrIntervenedOn,"story"){
  t.Fatalf("failed prerequisite blocked healthy independent branch: %+v",nodes)
 }
}
func TestTaskGraphDoesNotAutoretryUnknownOutcomeOrRegressedDependencies(t *testing.T){
 cases:=[]struct{
  name string
  node TaskGraphNode
  status string
 }{
  {"stale_after_success",TaskGraphNode{
   Key:"compiled",State:task.StateComplete,DependsOn:[]string{"source"},
   BlockedBy:[]string{"source"},
  },"needs_attention"},
  {"stale_after_start",TaskGraphNode{
   Key:"running",State:task.StateRunning,DependsOn:[]string{"source"},
   BlockedBy:[]string{"source"},
  },"needs_attention"},
  {"waiting_local_model",TaskGraphNode{
   Key:"model",State:task.StateWaitingDependency,
  },"waiting_resources"},
  {"paused_task",TaskGraphNode{
   Key:"paused",State:task.StatePaused,
  },"needs_attention"},
  {"untrusted_unscoped_edge",TaskGraphNode{
   Key:"unknown",State:task.StateCreated,
   BlockedBy:[]string{"unscoped_dependency"},
   FailedOrIntervenedOn:[]string{"unscoped_dependency"},
  },"needs_attention"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   graph:=&TaskGraph{Nodes:[]TaskGraphNode{tc.node}}
   _,_ = evaluateTaskGraph(graph)
   if graph.Nodes[0].Readiness!=tc.status||graph.Nodes[0].NextAction==""{
    t.Fatalf("unsafe recovery classification: %+v",graph.Nodes[0])
   }
  })
 }
}
func TestTaskGraphReportsCompletionOnlyForVerifiedCompleteState(t *testing.T){
 graph:=&TaskGraph{Nodes:[]TaskGraphNode{
  {Key:"a",State:task.StateComplete},
  {Key:"b",State:task.StateComplete},
 }}
 progress,status:=evaluateTaskGraph(graph)
 if status!="completed"||progress.Complete!=2{
  t.Fatalf("completed graph not visible: %+v %s",progress,status)
 }
 graph.Nodes[1].State=task.StateVerifying
 progress,status=evaluateTaskGraph(graph)
 if status!="work_available"||progress.Verifying!=1{
  t.Fatalf("verification mistaken for completed: %+v %s",progress,status)
 }
 graph.Nodes[1].State=task.StateCreated
 progress,status=evaluateTaskGraph(graph)
 if status!="work_available"||progress.Admissible!=1{
  t.Fatalf("no-model local queue mistaken for blocked: %+v %s",progress,status)
 }
}
