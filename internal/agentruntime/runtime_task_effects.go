package agentruntime

import(
 "context"
 "fmt"
 "strings"
)

// requireTaskRuntimeEffects applies persisted canonical Task execution
// policy to every Agent Runtime egress, not the external bridge's claimed
// "proposal_only"/"local" capabilities. Legacy unconfigured Task policies
// remain subject to existing runtime/budget/data-flow gates.
func(s *Service)requireTaskRuntimeEffects(
 ctx context.Context,cmd InvokeCommand,c Connection)error{
 if cmd.TaskID==nil{return nil}
 if s==nil||s.scopes.DB==nil{
  return fmt.Errorf("%w: canonical Task policy unavailable",ErrRuntimeInformationFlow)
 }
 effects:=[]string{"network.connect"}
 if !strings.EqualFold(strings.TrimSpace(c.AuthType),"none"){
  effects=append(effects,"secrets.use")
 }
 for _,action:=range effects{
  allowed,err:=s.scopes.AuthorizeTask(ctx,cmd.WorkspaceID,*cmd.TaskID,action)
  if err!=nil||!allowed{
   return fmt.Errorf("%w: canonical Workspace/Agent policy denied %s",ErrRuntimeInformationFlow,action)
  }
 }
 return nil
}
