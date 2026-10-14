package agentruntime

import(
 "context"
 "encoding/json"
 "fmt"
 "reflect"

)

// requireCurrentRuntimeDispatch checks canonical runtime destination, trust,
// mode, endpoint, secret reference and policy AFTER a queued invocation is
// authorised and again immediately before sending any input to the adapter.
// Self-supplied endpoint JSON or a stale scheduled candidate never grants
// permission for a changed/revoked runtime.
func (s *Service)requireCurrentRuntimeDispatch(
 ctx context.Context, original Connection,cmd InvokeCommand)error{
 if s==nil||s.repo==nil{
  return fmt.Errorf("%w: canonical runtime unavailable",ErrRuntimeInformationFlow)
 }
 current,err:=s.repo.Get(ctx,original.ID)
 if err!=nil||!reflect.DeepEqual(current,original){
  return fmt.Errorf("%w: runtime identity, endpoint or grant changed while queued",ErrRuntimeInformationFlow)
 }
 // A queued origin-node bridge cannot borrow stale host trust after the
 // canonical Node is suspended or revoked. The runtime's own connection
 // status and self-reported endpoint policy are not Node authority.
 var dp DataPolicy
 if err:=json.Unmarshal(current.DataPolicyJSON,&dp);err!=nil{
  return fmt.Errorf("%w: runtime policy unreadable",ErrRuntimeInformationFlow)
 }
 if dp.DestinationKind=="origin_node"{
  if s.scopes.DB==nil||current.NodeID==nil||
   *current.NodeID!=s.localNodeID{
   return fmt.Errorf("%w: canonical local Node identity unavailable",ErrRuntimeInformationFlow)
  }
  var local int
  var trust string
  err:=s.scopes.DB.QueryRowContext(ctx,
   `SELECT local,trust_state FROM harness_nodes WHERE id=?`,
   *current.NodeID).Scan(&local,&trust)
  if err!=nil||local!=1||trust!="local"{
   return fmt.Errorf("%w: origin Node trust revoked before Agent Runtime dispatch",ErrRuntimeInformationFlow)
  }
 }
 if err:=s.requireTaskRuntimeEffects(ctx,cmd,current);err!=nil{return err}
 if _,_,err:=s.authorizeInvocation(current,cmd.InputLabel);err!=nil{
  return fmt.Errorf("%w: runtime policy revoked or transport unavailable: %v",ErrRuntimeInformationFlow,err)
 }
 return nil
}
