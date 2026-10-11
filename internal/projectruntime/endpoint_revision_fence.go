package projectruntime

import (
 "context"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Endpoint status, external preview URL and updated_at are observations,
// not operator intent. All declarative fields and the optimistic revision
// must remain unchanged across OCI ensure/inspection/route publication.
func sameEndpointReconciliationTarget(a,b projectworkspace.Endpoint)bool{
 return a.ID==b.ID&&a.ProjectRuntimeID==b.ProjectRuntimeID&&
  samePtr(a.ApplicationID,b.ApplicationID)&&
  a.Revision==b.Revision&&a.Name==b.Name&&
  a.Protocol==b.Protocol&&a.InternalPort==b.InternalPort&&
  a.Exposure==b.Exposure&&samePtr(a.PathPrefix,b.PathPrefix)&&
  a.DesiredState==b.DesiredState
}

func sameEndpointReconciliationManifest(a,b []projectworkspace.Endpoint)bool{
 if len(a)!=len(b){return false}
 byID:=make(map[string]projectworkspace.Endpoint,len(a))
 for _,endpoint:=range a{
  if endpoint.ID=="" {return false}
  if _,exists:=byID[endpoint.ID];exists{return false}
  byID[endpoint.ID]=endpoint
 }
 seen:=make(map[string]bool,len(b))
 for _,endpoint:=range b{
  if seen[endpoint.ID]{return false}
  seen[endpoint.ID]=true
  old,exists:=byID[endpoint.ID]
  if !exists||!sameEndpointReconciliationTarget(old,endpoint){return false}
 }
 return true
}

func(r *Reconciler) requireEndpointManifest(ctx context.Context,runtimeID string,expected []projectworkspace.Endpoint)error{
 live,err:=r.projects.ListEndpoints(ctx,runtimeID)
 if err!=nil{return err}
 if !sameEndpointReconciliationManifest(expected,live){
  return ErrReconciliationDrift
 }
 return nil
}
