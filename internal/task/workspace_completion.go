package task

import (
 "encoding/json"
 "fmt"
 "strings"
)

// scopeWorkspaceCompletion imposes the Task's persisted Project Workspace on
// every Task creation path (HTTP, Routines, Project Orchestrator and delegation).
// Reasoning preferences remain caller-specified; filesystem/network/Vault
// authority is never inherited from untrusted completion JSON.
//
// A cloud model is allowed only by a deliberate remote_models:true on this
// Task. Scheduler qualification, provider policy and budget checks still apply.
func scopeWorkspaceCompletion(raw json.RawMessage, workspaceID string) (json.RawMessage, error) {
 workspaceID = strings.TrimSpace(workspaceID)
 if workspaceID=="" {return nil,fmt.Errorf("%w: Project Workspace identity required",ErrInvalidCommand)}
 var envelope map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&envelope);err!=nil||envelope==nil{
  return nil,fmt.Errorf("%w: Workspace Task completion must be a JSON object",ErrInvalidCommand)
 }
 routing:=map[string]json.RawMessage{}
 if existing,ok:=envelope["onepane_routing"];ok && string(existing)!="null" {
  if err:=json.Unmarshal(existing,&routing);err!=nil||routing==nil{
   return nil,fmt.Errorf("%w: onepane_routing must be an object",ErrInvalidCommand)
  }
 }
 if source,ok:=routing["project_workspace_id"];ok{
  var id string
  if err:=json.Unmarshal(source,&id);err!=nil||(id!=""&&id!=workspaceID){
   return nil,fmt.Errorf("%w: Workspace routing identity conflicts with Task ownership",ErrInvalidCommand)
  }
 }
 access:=map[string]json.RawMessage{}
 if existing,ok:=routing["workspace_access"];ok&&string(existing)!="null"{
  if err:=json.Unmarshal(existing,&access);err!=nil||access==nil{
   return nil,fmt.Errorf("%w: Workspace access must be an object",ErrInvalidCommand)
  }
 }
 for _,key:=range []string{"project_workspace_id","mode"} {
  if existing,ok:=access[key];ok{
   var v string
   if err:=json.Unmarshal(existing,&v);err!=nil{
    return nil,fmt.Errorf("%w: invalid Workspace access %s",ErrInvalidCommand,key)
   }
   if key=="project_workspace_id" && v!="" && v!=workspaceID {
    return nil,fmt.Errorf("%w: Workspace access identity conflicts with Task ownership",ErrInvalidCommand)
   }
   if key=="mode" && v!="" && v!="brokered" {
    return nil,fmt.Errorf("%w: direct Workspace access is prohibited",ErrInvalidCommand)
   }
  }
 }
 remote:=false
 if existing,ok:=access["remote_models"];ok{
  if err:=json.Unmarshal(existing,&remote);err!=nil{
   return nil,fmt.Errorf("%w: remote_models must be boolean",ErrInvalidCommand)
  }
 }
 filesystem:="workspace-only"
 if existing,ok:=access["filesystem"];ok{
  var v string
  if err:=json.Unmarshal(existing,&v);err!=nil{
   return nil,fmt.Errorf("%w: filesystem access must be a string",ErrInvalidCommand)
  }
  switch v {
  case "","workspace-only": 
  case "none":filesystem="none"
  default:return nil,fmt.Errorf("%w: host filesystem access is prohibited",ErrInvalidCommand)
  }
 }
 for _,key:=range []string{"internet","lan","browser","computer"}{
  if existing,ok:=access[key];ok{
   var requested bool
   if err:=json.Unmarshal(existing,&requested);err!=nil||requested{
    return nil,fmt.Errorf("%w: Workspace capability %s requires an independent grant",ErrInvalidCommand,key)
   }
  }
 }
 if existing,ok:=access["secrets"];ok {
  var scope string
  if err:=json.Unmarshal(existing,&scope);err!=nil||(scope!=""&&scope!="none"){
   return nil,fmt.Errorf("%w: Vault access requires an independent scoped grant",ErrInvalidCommand)
  }
 }
 safeAccess:=map[string]any{
  "mode":"brokered","project_workspace_id":workspaceID,"remote_models":remote,
  "filesystem":filesystem,"internet":false,"lan":false,
  "browser":false,"computer":false,"secrets":"none",
 }
 accessRaw,err:=json.Marshal(safeAccess)
 if err!=nil{return nil,err}
 idRaw,err:=json.Marshal(workspaceID)
 if err!=nil{return nil,err}
 routing["project_workspace_id"]=idRaw
 routing["workspace_access"]=accessRaw
 routingRaw,err:=json.Marshal(routing)
 if err!=nil{return nil,err}
 envelope["onepane_routing"]=routingRaw
 scoped,err:=json.Marshal(envelope)
 if err!=nil{return nil,err}
 return scoped,nil
}
