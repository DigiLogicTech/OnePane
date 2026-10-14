package agentruntime

import(
 "context"
 "fmt"
 "strings"
 "encoding/json"
 "github.com/DigiLogicTech/OnePane/internal/runtimeendpoint"
)

// requireTaskRuntimeEffects applies persisted canonical Task execution
// policy to every Agent Runtime egress, not the external bridge's claimed
// "proposal_only"/"local" capabilities. Legacy unconfigured Task policies
// remain subject to existing runtime/budget/data-flow gates.
func(s *Service)requireTaskRuntimeEffects(
 ctx context.Context,cmd InvokeCommand,c Connection)error{
 if cmd.TaskID==nil{
  // No Task means no canonical Project/Workspace/Agent grants. Preserve
  // explicit local-only proposal use for a human Assistant, but reject
  // unscoped external network, secret-backed or Tool Gateway effects.
  // Budget consent is independently required, even for localhost.
  var dp DataPolicy
  if s==nil||strings.TrimSpace(s.localNodeID)==""||
   c.NodeID==nil||*c.NodeID!=s.localNodeID||
   c.AdapterName!="builtin.agent_protocol_http"||c.AdapterVersion!="1"||
   c.OperatingMode!=ProposalOnly||
   !strings.EqualFold(strings.TrimSpace(c.AuthType),"none")||
   !runtimeendpoint.LiteralLoopbackHTTP(c.EndpointJSON)||
   json.Unmarshal(c.DataPolicyJSON,&dp)!=nil||
   dp.DestinationKind!="origin_node"{
   return fmt.Errorf("%w: Taskless Agent Runtime requires a local proposal-only uncredentialed bridge",ErrRuntimeInformationFlow)
  }
  return nil
 }
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
