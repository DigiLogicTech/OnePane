package projectorchestrator

import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
 "strings"
)

// AutonomousPlanDraft is untrusted proposed work, not executable authority.
// Its Workspace and Project may ONLY be supplied by a canonical caller; a
// model is never permitted to choose a new Workspace or issue tool grants.
type AutonomousPlanDraft struct {
 ProjectID string
 ProjectWorkspaceID string
 UserObjective string
 Nodes []TaskGraphNodeSpec
}

// ParseAutonomousPlanDraft translates bounded local-model JSON to a
// non-executable DAG proposal. The separate Project grant and atomic Task
// graph service must authorise any future execution. Unknown JSON fields
// fail closed to prevent smuggled Workspace, shell, network and secrets grants.
func ParseAutonomousPlanDraft(raw json.RawMessage,projectID,workspaceID,userObjective string)(AutonomousPlanDraft,error){
 projectID,workspaceID,userObjective=
  strings.TrimSpace(projectID),strings.TrimSpace(workspaceID),strings.TrimSpace(userObjective)
 if projectID==""||workspaceID==""||userObjective==""||
  len(userObjective)>4096||len(raw)==0||len(raw)>16*1024{
  return AutonomousPlanDraft{},ErrInvalid
 }
 // Even direct parser callers must reject conflicting JSON fields. A
 // model must not win Task admission by repeating a key with new semantics.
 if err:=rejectAmbiguousPlannerJSON(raw);err!=nil{
  return AutonomousPlanDraft{},fmt.Errorf("%w: ambiguous local plan: %v",ErrInvalid,err)
 }
 var input struct{
  Version int `json:"version"`
  Steps []struct{
   Key string `json:"key"`
   Objective string `json:"objective"`
   DependsOn []string `json:"depends_on"`
  } `json:"steps"`
 }
 dec:=json.NewDecoder(bytes.NewReader(raw))
 dec.DisallowUnknownFields()
 if err:=dec.Decode(&input);err!=nil{return AutonomousPlanDraft{},fmt.Errorf("%w: invalid local plan: %v",ErrInvalid,err)}
 if err:=dec.Decode(new(any));err!=io.EOF{
  return AutonomousPlanDraft{},fmt.Errorf("%w: multiple plan payloads",ErrInvalid)
 }
 if input.Version!=1||len(input.Steps)<1||len(input.Steps)>8{
  return AutonomousPlanDraft{},fmt.Errorf("%w: plan needs 1–8 steps in schema version 1",ErrInvalid)
 }
 nodes:=make([]TaskGraphNodeSpec,0,len(input.Steps))
 for _,step:=range input.Steps{
  key,objective:=strings.TrimSpace(step.Key),strings.TrimSpace(step.Objective)
  if len(objective)==0||len(objective)>512||len(step.DependsOn)>8{
   return AutonomousPlanDraft{},fmt.Errorf("%w: oversized or missing plan step",ErrInvalid)
  }
  nodes=append(nodes,TaskGraphNodeSpec{
   Key:key,ProjectWorkspaceID:workspaceID,Objective:objective,
   DependsOn:append([]string{},step.DependsOn...),
   Priority:10,ComputePreference:"auto",
  })
 }
 // This validates keys, dependency existence, duplicate edges, cycles and
 // topological order without creating Tasks, model sessions or capabilities.
 if _,_,err:=graphCreationPlan(CreateTaskGraphCommand{
  ProjectID:projectID,ActorPrincipalID:"bounded-draft",
  Name:"Autonomous plan draft",IdempotencyKey:"draft-preview",Nodes:nodes,
 });err!=nil{return AutonomousPlanDraft{},err}
 return AutonomousPlanDraft{
  ProjectID:projectID,ProjectWorkspaceID:workspaceID,
  UserObjective:userObjective,Nodes:nodes,
 },nil
}
