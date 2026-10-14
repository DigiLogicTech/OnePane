package agentruntime

import(
 "context"
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
 if err:=s.requireTaskRuntimeEffects(ctx,cmd,current);err!=nil{return err}
 if _,_,err:=s.authorizeInvocation(current,cmd.InputLabel);err!=nil{
  return fmt.Errorf("%w: runtime policy revoked or transport unavailable: %v",ErrRuntimeInformationFlow,err)
 }
 return nil
}
