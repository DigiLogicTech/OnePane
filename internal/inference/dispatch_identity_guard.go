package inference

import (
 "context"
 "fmt"
 "reflect"
)

// requireCurrentDispatchIdentity prevents model-queue/recovery latency from
// changing the approved destination. This applies to Task inferences too.
// CPU/GPU residency and verified capacity may legitimately change as a model
// cold-loads. They are *observations*, not a new right to change runtime,
// model, provider, endpoint or destination, which still fail closed.
func(s *Service)requireCurrentDispatchIdentity(ctx context.Context,
 selectedDeployment ModelDeployment,selectedModel Model,
 selectedProvider *ProviderConnection)error{
 if s==nil||s.repo==nil{return fmt.Errorf("%w: inference catalog unavailable",ErrInformationFlow)}
 currentDeployment,err:=s.repo.Deployment(ctx,selectedDeployment.ID)
 if err!=nil||currentDeployment.Status!=DeploymentReady||
  !sameInferenceSecurityDeployment(selectedDeployment,currentDeployment){
  return fmt.Errorf("%w: deployment identity or runtime configuration changed while queued",ErrInformationFlow)
 }
 currentModel,err:=s.repo.Model(ctx,selectedModel.ID)
 if err!=nil||currentModel.ID!=selectedDeployment.ModelID||
  currentModel.TrustState==ModelQuarantined||currentModel.TrustState==ModelRevoked||
  !reflect.DeepEqual(currentModel,selectedModel){
  return fmt.Errorf("%w: model trust or identity changed while queued",ErrInformationFlow)
 }
 if selectedProvider!=nil {
  if selectedDeployment.ProviderConnectionID==nil||
   *selectedDeployment.ProviderConnectionID!=selectedProvider.ID{
   return fmt.Errorf("%w: provider association changed while queued",ErrInformationFlow)
  }
  currentProvider,err:=s.repo.Provider(ctx,selectedProvider.ID)
  if err!=nil||!reflect.DeepEqual(currentProvider,*selectedProvider){
   return fmt.Errorf("%w: provider status, routing or secret identity changed while queued",ErrInformationFlow)
  }
 }else if selectedDeployment.ProviderConnectionID!=nil{
  return fmt.Errorf("%w: unapproved provider association",ErrInformationFlow)
 }
 // Revocation applies to the origin host too. READY and resident
 // deployment metadata cannot preserve a zero-cost local authority if
 // its canonical Node trust was revoked during a model queue/cold load.
 // Provider-backed routes do not obtain local placement merely by
 // referencing the origin Node in historical deployment metadata.
 if selectedDeployment.NodeID!=nil{
  nodeID:=*selectedDeployment.NodeID
  if nodeID==s.localNodeID && selectedDeployment.ProviderConnectionID==nil{
   state,e:=s.repo.NodeTrust(ctx,nodeID)
   if e!=nil||state!="local"{
    return fmt.Errorf("%w: host Node trust was revoked before local inference dispatch",ErrInformationFlow)
   }
  }else if nodeID!=s.localNodeID{
   state,e:=s.repo.NodeTrust(ctx,nodeID)
   if e!=nil||state!="paired"{
    return fmt.Errorf("%w: paired Node trust was revoked before dispatch",ErrInformationFlow)
   }
  }
 }
 return nil
}

// sameInferenceSecurityDeployment permits monotone, observed residency and
// context-capacity changes during a legitimate cold load. A rollback of the
// revision or ANY destination/runtime/security field remains forbidden.
// This comparison is deliberately separate from whole-row equality: the
// local-first Worker may queue work until the model is loaded into memory.
func sameInferenceSecurityDeployment(before,after ModelDeployment)bool{
 if after.Revision<before.Revision{return false}
 before.ResidencyState=nil
 after.ResidencyState=nil
 before.ContextMaxReported=nil
 after.ContextMaxReported=nil
 before.ContextMaxVerified=nil
 after.ContextMaxVerified=nil
 before.Revision=0
 after.Revision=0
 before.UpdatedAt=0
 after.UpdatedAt=0
 return reflect.DeepEqual(before,after)
}
