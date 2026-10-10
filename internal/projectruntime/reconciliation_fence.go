package projectruntime

import (
 "bytes"
 "context"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// ErrReconciliationDrift is a safety boundary. A verified observation about
// revision N must never be written to revision N+1 after the operator changes
// placement, desired state, application image or sandbox constraints.
var ErrReconciliationDrift=errors.New("Project runtime/application changed during reconciliation; re-evaluate new intent before any further Tool action")

func samePtr(a,b *string)bool{
 if a==nil||b==nil{return a==nil&&b==nil}
 return *a==*b
}
func sameRuntimeReconciliationTarget(a,b projectworkspace.ProjectRuntime)bool{
 return a.ID==b.ID&&a.Revision==b.Revision&&
  a.ProjectID==b.ProjectID&&
  samePtr(a.ProjectWorkspaceID,b.ProjectWorkspaceID)&&
  samePtr(a.NodeID,b.NodeID)&&
  a.Backend==b.Backend&&a.IsolationMode==b.IsolationMode&&
  a.DesiredState==b.DesiredState&&
  bytes.Equal(a.RuntimeSpecJSON,b.RuntimeSpecJSON)&&
  bytes.Equal(a.ResourceLimitsJSON,b.ResourceLimitsJSON)&&
  bytes.Equal(a.NetworkPolicyJSON,b.NetworkPolicyJSON)&&
  bytes.Equal(a.FilesystemPolicyJSON,b.FilesystemPolicyJSON)&&
  bytes.Equal(a.EnvironmentBindingsJSON,b.EnvironmentBindingsJSON)
}
func sameAppReconciliationTarget(a,b projectworkspace.Application)bool{
 return a.ID==b.ID&&a.Revision==b.Revision&&
  a.ProjectRuntimeID==b.ProjectRuntimeID&&
  a.SourceKind==b.SourceKind&&a.SourceRef==b.SourceRef&&
  samePtr(a.VersionRef,b.VersionRef)&&
  a.DesiredState==b.DesiredState&&
  bytes.Equal(a.InstallSpecJSON,b.InstallSpecJSON)&&
  bytes.Equal(a.RuntimeSpecJSON,b.RuntimeSpecJSON)&&
  bytes.Equal(a.EnvironmentBindingsJSON,b.EnvironmentBindingsJSON)
}
func(r *Reconciler) requireRuntimeRevision(ctx context.Context,expected projectworkspace.ProjectRuntime)error{
 live,err:=r.projects.Runtime(ctx,expected.ID)
 if err!=nil{return err}
 if !sameRuntimeReconciliationTarget(expected,live)||
  !localPlacementAllowed(r.localNodeID,live.NodeID){
  return ErrReconciliationDrift
 }
 return nil
}
func(r *Reconciler) requireApplicationRevision(ctx context.Context,expected projectworkspace.Application)error{
 live,err:=r.projects.Application(ctx,expected.ID)
 if err!=nil{return err}
 if !sameAppReconciliationTarget(expected,live){
  return ErrReconciliationDrift
 }
 return nil
}
