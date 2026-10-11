package inference

import (
 "context"
 "fmt"
 "reflect"
)

// requireCurrentDispatchIdentity prevents model-queue/recovery latency from
// changing the approved destination. This applies to Task inferences too,
// not only the narrower, hard-local general Assistant policy. Compare the
// canonical full records, not a claimed class from scheduler metadata.
func(s *Service)requireCurrentDispatchIdentity(ctx context.Context,
 selectedDeployment ModelDeployment,selectedModel Model,
 selectedProvider *ProviderConnection)error{
 if s==nil||s.repo==nil{return fmt.Errorf("%w: inference catalog unavailable",ErrInformationFlow)}
 currentDeployment,err:=s.repo.Deployment(ctx,selectedDeployment.ID)
 if err!=nil||currentDeployment.Status!=DeploymentReady||
  !reflect.DeepEqual(currentDeployment,selectedDeployment){
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
 // A paired Node can be untrusted while a local model is cold-swapped.
 if selectedDeployment.NodeID!=nil&&*selectedDeployment.NodeID!=s.localNodeID{
  state,err:=s.repo.NodeTrust(ctx,*selectedDeployment.NodeID)
  if err!=nil||state!="paired"{
   return fmt.Errorf("%w: paired Node trust was revoked before dispatch",ErrInformationFlow)
  }
 }
 return nil
}
