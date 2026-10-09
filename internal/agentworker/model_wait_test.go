package agentworker

import (
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestLocalFirstModelWaitOnlyForTransientEligibility(t *testing.T){
 workspace:="world"
 named:=task.Task{ProjectWorkspaceID:&workspace}
 legacy:=task.Task{}
 cases:=[]struct{
  name string
  current task.Task
  err error
  rejected []scheduler.Rejection
  want bool
 }{
  {"none_installed",named,scheduler.ErrNoEligibleCandidate,nil,true},
  {"hardware_busy",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"candidate is not schedulable"}},true},
  {"model_degraded",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"candidate is degraded"}},true},
  {"new_model_untested",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"candidate is untested"}},true},
  {"local_only_refuses_cloud",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"remote candidate disallowed by workspace policy"}},true},
  {"pinned_model_not_registered",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"not_selected_by_request"}},true},
  {"true_incompatibility",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"candidate is incompatible"}},false},
  {"insufficient_context",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"context exceeds candidate limit"}},false},
  {"policy_denied",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"information-flow policy denied candidate"}},false},
  {"paid_only",named,scheduler.ErrNoEligibleCandidate,[]scheduler.Rejection{{Reason:"candidate may incur monetary cost"}},false},
  {"legacy_task_unchanged",legacy,scheduler.ErrNoEligibleCandidate,nil,false},
  {"scheduler_error",named,errors.New("offline SQL"),nil,false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if got:=shouldWaitForLocalModel(tc.current,tc.err,tc.rejected);got!=tc.want{
    t.Fatalf("wait=%v want=%v",got,tc.want)
   }
  })
 }
}

func TestModelResourceWaitPersistsBoundedRetryAndPreviousAttempts(t *testing.T){
 var previous json.RawMessage
 for _,tc:=range []struct{attempt int;delay int64}{
  {1,15000},{2,30000},{3,60000},{4,120000},{5,240000},{6,300000},{100,300000},
 }{
  if got:=modelWaitDelayMS(tc.attempt);got!=tc.delay{
   t.Fatalf("retry %d delay=%d want=%d",tc.attempt,got,tc.delay)
  }
  b,err:=json.Marshal(modelWaitEnvelope{ModelWait:&modelWaitRecord{Attempt:tc.attempt,RetryAtMS:1700000000000+tc.delay,Reason:"waiting"}})
  if err!=nil{t.Fatal(err)}
  got:=decodeModelWait(b)
  if got==nil||got.Attempt!=tc.attempt||got.RetryAtMS!=1700000000000+tc.delay{
   t.Fatalf("durable wait malformed: %s",b)
  }
  previous=b
 }
 if decodeModelWait(json.RawMessage(`{"operation_id":"operation-1"}`))!=nil{
  t.Fatal("operation verification continuation incorrectly classified as model resource wait")
 }
 if len(previous)==0 {t.Fatal("missing final persisted wait record")}
 if decodeModelWait(json.RawMessage(`{"model_wait":{"attempt":0,"retry_at_ms":0}}`))!=nil{
  t.Fatal("invalid wait record accepted")
 }
}
