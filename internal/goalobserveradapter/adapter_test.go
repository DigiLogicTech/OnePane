package goalobserveradapter

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)
type noFenceEffect struct{calls int}
func(f *noFenceEffect)SetGoalTCPObservationFence(context.Context,projectworkspace.SetGoalTCPObservationFenceCommand)(projectworkspace.GoalTCPObservationFence,error){
 f.calls++
 return projectworkspace.GoalTCPObservationFence{},nil
}
func(f *noFenceEffect)GetGoalTCPObservationFence(context.Context,string,string)(projectworkspace.GoalTCPObservationFence,error){
 return projectworkspace.GoalTCPObservationFence{},nil
}
func TestAdapterRejectsSpoofedActorAndMalformedResourceBeforeAnyEffect(t *testing.T){
 fake:=&noFenceEffect{}
 a:=New(nil,fake)
 base:=tool.AdapterRequest{
  ToolID:PauseToolID,ToolVersion:ToolVersion,
  ResourceRef:ResourceRef("p","g"),
  Input:json.RawMessage(`{"project_id":"p","goal_id":"g","expected_goal_revision":1,"expected_fence_revision":0}`),
 }
 if _,err:=a.Invoke(context.Background(),base);err==nil||fake.calls!=0{
  t.Fatal("unconfigured adapter mutated local state")
 }
 a.db=nil // fail closed before operation identity lookup
 base.Input=json.RawMessage(`{"project_id":"p","goal_id":"g","actor":"owner","expected_goal_revision":1}`)
 if _,err:=parseInput(base.Input);err==nil{t.Fatal("caller-chosen human identity accepted")}
 for _,v:=range []string{
  `{"project_id":"p/other","goal_id":"g","expected_goal_revision":1}`,
  `{"project_id":"p","goal_id":"g","expected_goal_revision":0}`,
  `{"project_id":"p","goal_id":"g","expected_goal_revision":1}{"extra":1}`,
  `{"project_id":"p","goal_id":"g","expected_goal_revision":1,"expected_fence_revision":-1}`,
 }{
  if _,err:=parseInput([]byte(v));err==nil{t.Fatalf("malformed unbounded scope accepted: %s",v)}
 }
}
