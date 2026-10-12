package tool

import (
 "context"
 "fmt"
 "strings"
)

// authorizeToolFeatureActions applies the current canonical Task policy.
// Old Tasks without explicit policy remain subject to the existing lease,
// OperationCoordinator and sandbox restrictions. A configured policy
// cannot be bypassed through an unclassified generic tool adapter.
func (g *Gateway) authorizeToolFeatureActions(ctx context.Context,cmd InvokeCommand,def Definition)error{
 if cmd.TaskID==nil{
  // Project-scoped sandbox/runtime tools must resolve a canonical Task
  // before being given an effect. A tenant Workspace lease alone does not
  // tell the gateway which Project/Workspace/Agent restrictions apply.
  // This also includes read/inspect tools, which can disclose Project data.
  id:=strings.ToLower(strings.TrimSpace(def.ID))
  if strings.HasPrefix(id,"project.app.")||
    strings.HasPrefix(id,"project.runtime.")||
    strings.HasPrefix(id,"project.image."){
   return fmt.Errorf("Project tool requires an attached, scoped Task")
  }
  return nil
 }
 if g.scopes.DB==nil{
  return fmt.Errorf("Project tool scope store is unavailable")
 }
 decision,err:=g.scopes.ForTask(ctx,cmd.WorkspaceID,*cmd.TaskID)
 if err!=nil{return fmt.Errorf("canonical Task scope unavailable: %w",err)}
 if !decision.Configured{return nil}
 actions,classified:=scopedToolActions(def.CapabilityID,def.ID,def.Mode)
 if !classified{
  return fmt.Errorf("tool effects have no permitted Workspace classification")
 }
 for _,action:=range actions{
  if !decision.Effective.ValidateExecution(action){
   return fmt.Errorf("Workspace policy denies %s",action)
  }
 }
 return nil
}
