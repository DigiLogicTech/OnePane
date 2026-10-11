package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// applyApprovedWorkspaceToolchain is a second, independent Task/Workspace
// admission boundary after enforceSandboxToolOwnership and before any lease
// lookup/invocation. If an operator has approved a Workspace toolchain,
// ordinary project.app.exec can no longer opt out of its required executable
// preflight or switch to a different OCI application.
//
// This does not attest versions, install software or confer tool authority.
// The sandbox adapter performs live presence checks under its existing lease
// and independently verified rootless OCI boundary before executing.
func applyApprovedWorkspaceToolchain(ctx context.Context,db *sql.DB,t task.Task,raw json.RawMessage)(json.RawMessage,error){
 if db==nil{return nil,fmt.Errorf("Workspace toolchain database unavailable")}
 if t.ProjectID==nil||t.ProjectWorkspaceID==nil{return raw,nil}
 var completion struct{
  ToolchainManifestSHA256 string `json:"toolchain_manifest_sha256"`
 }
 if len(t.Completion)>0 && json.Unmarshal(t.Completion,&completion)!=nil{
  return nil,fmt.Errorf("Task completion metadata cannot be verified")
 }
 manifest,err:=projectworkspace.NewService(db,nil,clock.Real{}).
  WorkspaceToolchainManifest(ctx,*t.ProjectID,*t.ProjectWorkspaceID)
 if errors.Is(err,sql.ErrNoRows){
  if completion.ToolchainManifestSHA256!=""{
   return nil,fmt.Errorf("Task-pinned Workspace toolchain approval missing; human reapproval required")
  }
  return raw,nil // Legacy/unconfigured Workspace Tasks remain supported.
 }
 if err!=nil{return nil,fmt.Errorf("Workspace toolchain approval unavailable: %w",err)}
 if manifest.Status!="approved_unverified"||!manifest.CurrentApplicationMatches{
  return nil,fmt.Errorf("Workspace toolchain approval stale; operator reapproval required")
 }
 if completion.ToolchainManifestSHA256!=""&&completion.ToolchainManifestSHA256!=manifest.ManifestSHA256{
  return nil,fmt.Errorf("Workspace toolchain approval changed since Task creation; operator review required")
 }
 var input map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&input);err!=nil||input==nil{
  return nil,fmt.Errorf("invalid Task Workspace exec input")
 }
 var applicationID string
 if err:=json.Unmarshal(input["application_id"],&applicationID);err!=nil||
  applicationID!=manifest.ApplicationID{
  return nil,fmt.Errorf("Workspace exec application differs from the human-approved toolchain")
 }
 names:=make([]string,0,len(manifest.Requirements))
 for _,r:=range manifest.Requirements{names=append(names,r.Executable)}
 approvedJSON,err:=json.Marshal(names)
 if err!=nil{return nil,err}
 if claimed,ok:=input["required_executables"];ok{
  var requested []string
  if json.Unmarshal(claimed,&requested)!=nil||len(requested)!=len(names){
   return nil,fmt.Errorf("Task attempted to override approved Workspace tool requirements")
  }
  for i,n:=range names{if requested[i]!=n{
   return nil,fmt.Errorf("Task attempted to override approved Workspace tool requirements")
  }}
 }
 input["required_executables"]=approvedJSON
 // Does not set an OCI image, environment, arbitrary path, shell arguments or
 // privileged resource. The sandbox adapter still validates all caller input.
 return json.Marshal(input)
}
