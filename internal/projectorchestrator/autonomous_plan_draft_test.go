package projectorchestrator

import (
 "encoding/json"
 "errors"
 "strings"
 "testing"
)

func TestBoundedAutonomousPlanParserBindsCanonicalWorkspaceAndChecksDAG(t *testing.T){
 plan:=json.RawMessage(`{"version":1,"steps":[
  {"key":"research","objective":"Research the authorised requirement"},
  {"key":"build","objective":"Implement a local artifact","depends_on":["research"]},
  {"key":"test","objective":"Validate the artifact","depends_on":["build"]}
 ]}`)
 p,err:=ParseAutonomousPlanDraft(plan,"project","chosen-workspace","Build a verified deliverable")
 if err!=nil{t.Fatal(err)}
 if p.ProjectID!="project"||p.ProjectWorkspaceID!="chosen-workspace"||
  p.UserObjective!="Build a verified deliverable"||len(p.Nodes)!=3{
  t.Fatalf("draft lost canonical ownership or operator objective: %+v",p)
 }
 for _,node:=range p.Nodes{
  if node.ProjectWorkspaceID!="chosen-workspace"||
    node.ComputePreference!="auto"||node.Priority!=10{
   t.Fatalf("model gained placement, priority or compute authority: %+v",node)
  }
 }
 if len(p.Nodes[2].DependsOn)!=1||p.Nodes[2].DependsOn[0]!="build"{
  t.Fatalf("valid topology was lost: %+v",p.Nodes)
 }
}

func TestBoundedAutonomousPlanRejectsInjectedScopesToolsAndUnsafeTopologies(t *testing.T){
 cases:=[]struct{name,plan string}{
  {"foreign Workspace",`{"version":1,"steps":[{"key":"a","objective":"A","project_workspace_id":"foreign"}]}`},
  {"root tool request",`{"version":1,"network":true,"steps":[{"key":"a","objective":"A"}]}`},
  {"process capability",`{"version":1,"steps":[{"key":"a","objective":"A","tool":"shell"}]}`},
  {"secrets grant",`{"version":1,"steps":[{"key":"a","objective":"A","secrets":true}]}`},
  {"duplicate keys",`{"version":1,"steps":[{"key":"a","objective":"A"},{"key":"a","objective":"B"}]}`},
  {"unknown predecessor",`{"version":1,"steps":[{"key":"a","objective":"A","depends_on":["missing"]}]}`},
  {"duplicate edges",`{"version":1,"steps":[{"key":"a","objective":"A"},{"key":"b","objective":"B","depends_on":["a","a"]}]}`},
  {"cycle",`{"version":1,"steps":[{"key":"a","objective":"A","depends_on":["b"]},{"key":"b","objective":"B","depends_on":["a"]}]}`},
  {"self-edge",`{"version":1,"steps":[{"key":"a","objective":"A","depends_on":["a"]}]}`},
  {"empty",`{"version":1,"steps":[]}`},
  {"wrong version",`{"version":2,"steps":[{"key":"a","objective":"A"}]}`},
  {"malformed",`{"version":1,`},
  {"second payload",`{"version":1,"steps":[{"key":"a","objective":"A"}]}{"version":1}`},
  {"missing objective",`{"version":1,"steps":[{"key":"a"}]}`},
  {"invalid key",`{"version":1,"steps":[{"key":"@","objective":"A"}]}`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=ParseAutonomousPlanDraft(json.RawMessage(tc.plan),
    "project","work","User objective");err==nil{
    t.Fatalf("untrusted plan admitted: %s",tc.plan)
   }
  })
 }
 tooLong:=`{"version":1,"steps":[{"key":"a","objective":"`+strings.Repeat("a",513)+`"}]}`
 if _,err:=ParseAutonomousPlanDraft(json.RawMessage(tooLong),"project","work","User");!errors.Is(err,ErrInvalid){
  t.Fatalf("oversized Task objective admitted: %v",err)
 }
 if _,err:=ParseAutonomousPlanDraft(json.RawMessage(`{"version":1,"steps":[{"key":"a","objective":"A"}]}`),"project","","User");err==nil{
  t.Fatal("model plan was not bound to an operator-selected Workspace")
 }
}

func TestBoundedAutonomousPlanCapsCountAndPayloadBytes(t *testing.T){
 many:=[]map[string]any{}
 for i:=0;i<9;i++{many=append(many,map[string]any{"key":string(rune('a'+i)),"objective":"Safe work"})}
 raw,_:=json.Marshal(map[string]any{"version":1,"steps":many})
 if _,err:=ParseAutonomousPlanDraft(raw,"project","work","User");err==nil{
  t.Fatal("unbounded model Task fan-out admitted")
 }
 raw=json.RawMessage(strings.Repeat(" ",16*1024+1))
 if _,err:=ParseAutonomousPlanDraft(raw,"project","work","User");err==nil{
  t.Fatal("oversized JSON plan admitted")
 }
}
