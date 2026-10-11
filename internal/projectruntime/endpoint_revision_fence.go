package projectruntime

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "sort"

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

// endpointPlanDigest binds a prepared OCI ensure to the actual approved
// application endpoint configuration, independent of DB result ordering.
// An Endpoint-only edit need not increment the Application revision, but
// must allocate a *new* operation identity instead of returning an old
// committed OCI mutation created for a different published port plan.
func endpointPlanDigest(endpoints []projectworkspace.Endpoint,appID string)string{
 type routeIntent struct{
  ID string `json:"id"`
  Revision int64 `json:"revision"`
  Protocol string `json:"protocol"`
  InternalPort int `json:"internal_port"`
  Exposure projectworkspace.EndpointExposure `json:"exposure"`
  PathPrefix *string `json:"path_prefix,omitempty"`
  DesiredState string `json:"desired_state"`
 }
 selected:=make([]routeIntent,0)
 for _,e:=range endpoints{
  if e.ApplicationID==nil||*e.ApplicationID!=appID{continue}
  selected=append(selected,routeIntent{
   ID:e.ID,Revision:e.Revision,Protocol:e.Protocol,
   InternalPort:e.InternalPort,Exposure:e.Exposure,
   PathPrefix:e.PathPrefix,DesiredState:e.DesiredState,
  })
 }
 sort.Slice(selected,func(i,j int)bool{return selected[i].ID<selected[j].ID})
 canonical,_:=json.Marshal(selected)
 digest:=sha256.Sum256(canonical)
 return hex.EncodeToString(digest[:])
}
