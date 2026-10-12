package tool

import (
 "context"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/authority"
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
  // The persisted feature-policy resolver needs a canonical Task to
  // identify which Project, Workspace and Agent permissions govern a
  // generic shell/MCP/OCI or external-send effect. A tenant lease alone
  // is not a Project permission grant. Keep legacy in-memory Gateway
  // tests compatible; production gateways always have a scope store.
  if g.scopes.DB!=nil{
   switch def.Mode{
   case authority.ActionExecuteSandboxed,authority.ActionMutate,
     authority.ActionExternalSend:
    return fmt.Errorf("side-effecting Tool requires an attached, scoped Task")
   }
  }
  return nil
 }
 if g.scopes.DB==nil{
  return fmt.Errorf("Project tool scope store is unavailable")
 }
 // Even an unmigrated Task cannot impersonate the *identity* of a
 // built-in Project operation with a different authority mode. The
 // legacy unconfigured-feature-policy exception only preserves already
 // registered, correctly classified operations; it is not a registry
 // spoofing exemption.
 id:=strings.ToLower(strings.TrimSpace(def.ID))
 projectTool:=strings.HasPrefix(id,"project.app.")||
   strings.HasPrefix(id,"project.runtime.")||
   strings.HasPrefix(id,"project.image.")
 actions,classified:=scopedToolActions(def.CapabilityID,def.ID,def.Mode)
 if projectTool&&!classified{
  return fmt.Errorf("unrecognised Project Tool ID or execution mode")
 }
 decision,err:=g.scopes.ForTask(ctx,cmd.WorkspaceID,*cmd.TaskID)
 if err!=nil{return fmt.Errorf("canonical Task scope unavailable: %w",err)}
 if !decision.Configured{return nil}
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
