package inference

import (
 "context"
 "encoding/json"
 "fmt"
 "strings"
)

// tasklessPrivateChat marks the two narrow service-originated contexts that
// never consent to an off-host/paid model. This is a RESTRICTION on a
// service-controlled request, not new authority created by classification.
func tasklessPrivateChat(cmd ExecuteCommand)bool{
 var metadata struct {Source string `json:"source"`}
 if json.Unmarshal(cmd.ClassificationMetadata,&metadata)!=nil{return false}
 return metadata.Source=="onepane-assistant"||metadata.Source=="project-orchestrator"
}

// requireTasklessLocalDeployment prevents stale scheduler results and
// provider/free metadata from exporting Project or general Assistant prompts.
// Both the origin Node identity and provider absence must be verified from
// canonical deployment records. Node transfer requires a separate Task grant.
func requireTasklessLocalDeployment(cmd ExecuteCommand,d ModelDeployment,localNodeID string)error{
 if !tasklessPrivateChat(cmd){return nil}
 if cmd.TaskID!=nil||strings.TrimSpace(localNodeID)==""||
  d.ProviderConnectionID!=nil||d.NodeID==nil||*d.NodeID!=localNodeID||
  d.Status!=DeploymentReady{
  return fmt.Errorf("%w: taskless Assistant/Orchestrator inference must use verified host-local deployment",ErrInformationFlow)
 }
 return nil
}

// Before transport call, re-read the persisted deployment in case a remote
// provider or Node has been attached while waiting for runtime resources.
func(s *Service) requireTasklessLocalAtTransport(ctx context.Context,cmd ExecuteCommand,previous ModelDeployment)error{
 if !tasklessPrivateChat(cmd){return nil}
 current,err:=s.repo.Deployment(ctx,cmd.DeploymentID)
 if err!=nil{return fmt.Errorf("%w: private chat deployment unavailable",ErrInformationFlow)}
 if err=requireTasklessLocalDeployment(cmd,current,s.localNodeID);err!=nil{return err}
 if previous.ID!=current.ID||previous.ModelID!=current.ModelID||
  previous.Revision!=current.Revision||previous.DeploymentFingerprint!=current.DeploymentFingerprint{
  return fmt.Errorf("%w: private chat deployment changed before transport",ErrInformationFlow)
 }
 return nil
}
