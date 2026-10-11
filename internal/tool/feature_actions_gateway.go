package tool

import (
 "context"
 "fmt"
)

// authorizeToolFeatureActions applies the current canonical Task policy.
// Old Tasks without explicit policy remain subject to the existing lease,
// OperationCoordinator and sandbox restrictions. A configured policy
// cannot be bypassed through an unclassified generic tool adapter.
func (g *Gateway) authorizeToolFeatureActions(ctx context.Context,cmd InvokeCommand,def Definition)error{
 if cmd.TaskID==nil||g.scopes.DB==nil{
  return nil
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
