package localai

import (
 "context"
 "fmt"
)

// qualificationRuntimeLease is the minimum activity/stop boundary needed for
// Agent Check cleanup. The concrete RuntimeSupervisor uses residencyMu and
// activityMu to protect concurrent inference from eviction.
type qualificationRuntimeLease interface {
 Release(context.Context,string) error
 StopIfIdle(context.Context,string)(bool,error)
}

func cleanupQualificationResidency(ctx context.Context, runtime qualificationRuntimeLease, deploymentID string)(bool,error){
 if runtime==nil{return false,fmt.Errorf("qualification runtime lease unavailable")}
 if err:=runtime.Release(ctx,deploymentID);err!=nil{
  return false,fmt.Errorf("release Agent Check model activity lease: %w",err)
 }
 drained,err:=runtime.StopIfIdle(ctx,deploymentID)
 if err!=nil{return false,fmt.Errorf("drain idle Agent Check runtime: %w",err)}
 return drained,nil
}
