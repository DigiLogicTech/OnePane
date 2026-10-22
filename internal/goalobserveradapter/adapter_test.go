package goalobserveradapter

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "strings"
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


func TestObserverAdapterRequiresExactNonAmbiguousEffectScope(t *testing.T){
 valid:=`{"project_id":"project","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0}`
 got,err:=parseInput([]byte(valid))
 if err!=nil||got.ProjectID!="project"||got.ExpectedGoalRevision!=1{
  t.Fatalf("valid human-scoped effect refused: %+v %v",got,err)
 }
 for _,raw:=range []string{
  `{"project_id":"a","project_id":"b","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0}`,
  `{"project_id":"project","Project_ID":"b","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0}`,
  `{"project_id":"project","goal_id":"goal","expected_goal_revision":1,"expected_goal_revision":2,"expected_fence_revision":0}`,
  `{"project_id":"project","goal_id":"goal","expected_goal_revision":1}`,
  `{"project_id":"project","goal_id":"goal","expected_fence_revision":0}`,
  `{"project_id":"project","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0,"actor":"robot"}`,
  `{"project_id":"project","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0}{}`,
  `{"project_id":"project","goal_id":"goal","expected_goal_revision":1,"expected_fence_revision":0,"padding":"`+strings.Repeat("X",4096)+`"}`,
 }{
  if _,err:=parseInput([]byte(raw));err==nil{
   t.Fatalf("ambiguous or oversized observer effect accepted: %s",raw[:min(len(raw),120)])
  }
 }
}

func TestCanonicalObserverEffectHashMatchesToolGatewayBytes(t *testing.T){
 v:=input{ProjectID:"p",GoalID:"g",ExpectedGoalRevision:2,ExpectedFenceRevision:0}
 hash,err:=canonicalFenceInputHash(v)
 if err!=nil{t.Fatal(err)}
 canonical:=[]byte(`{"expected_fence_revision":0,"expected_goal_revision":2,"goal_id":"g","project_id":"p"}`)
 expected:=sha256.Sum256(canonical)
 if hash!="sha256:"+hex.EncodeToString(expected[:]){
  t.Fatalf("last-boundary hash diverges from gateway canonical JSON: %s",hash)
 }
 changed:=v
 changed.ExpectedFenceRevision=1
 different,err:=canonicalFenceInputHash(changed)
 if err!=nil||different==hash{t.Fatal("changed approved revision preserved original invocation hash")}
}
