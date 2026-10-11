package projectruntime

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestEndpointReconciliationManifestRejectsChangedOrReplacedRoutes(t *testing.T){
 a:="application-a"
 b:="application-b"
 path:="/preview"
 base:=[]projectworkspace.Endpoint{{
  ID:"web",ProjectRuntimeID:"runtime-a",ApplicationID:&a,
  Name:"web",Protocol:"http",InternalPort:8080,
  Exposure:projectworkspace.ExposurePreview,PathPrefix:&path,
  DesiredState:"enabled",Revision:11,Status:"declared",
 },{
  ID:"metrics",ProjectRuntimeID:"runtime-a",ApplicationID:&b,
  Name:"metrics",Protocol:"http",InternalPort:9090,
  Exposure:projectworkspace.ExposurePreview,
  DesiredState:"disabled",Revision:4,Status:"declared",
 }}
 other:=append([]projectworkspace.Endpoint(nil),base...)
 other[0],other[1]=other[1],other[0]
 if !sameEndpointReconciliationManifest(base,other){
  t.Fatal("logical endpoint order or observed Status changed the approved route plan")
 }
 other[1].Status="ready"
 other[1].UpdatedAt=123
 if !sameEndpointReconciliationManifest(base,other){
  t.Fatal("derived status/updated_at incorrectly considered a route intent revision")
 }
 cases:=[]struct{name string;mutate func([]projectworkspace.Endpoint) []projectworkspace.Endpoint}{
  {"new_endpoint",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{return append(v,projectworkspace.Endpoint{ID:"unexpected"})}},
  {"removed_endpoint",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{return v[:1]}},
  {"duplicate_endpoint",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{return append(v,v[0])}},
  {"revised",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].Revision++;return v}},
  {"port_changed",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].InternalPort=80;return v}},
  {"runtime_moved",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].ProjectRuntimeID="other";return v}},
  {"app_moved",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].ApplicationID=&b;return v}},
  {"disabled",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].DesiredState="disabled";return v}},
  {"path_changed",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{p:="/other";v[0].PathPrefix=&p;return v}},
  {"exposure_changed",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].Exposure="external";return v}},
  {"protocol_changed",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].Protocol="tcp";return v}},
  {"name_changed",func(v []projectworkspace.Endpoint)[]projectworkspace.Endpoint{v[0].Name="other";return v}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   changed:=append([]projectworkspace.Endpoint(nil),base...)
   changed=tc.mutate(changed)
   if sameEndpointReconciliationManifest(base,changed){
    t.Fatal("stale OCI port mapping accepted changed Endpoint intent")
   }
  })
 }
}

func TestEndpointPlanDigestPinsIdempotentOCIEnsureToCurrentRouteIntent(t *testing.T){
 app:="app"
 unrelated:="another-app"
 a:=projectworkspace.Endpoint{
  ID:"api",ProjectRuntimeID:"runtime",ApplicationID:&app,
  Name:"api",Protocol:"http",InternalPort:8080,
  Exposure:projectworkspace.ExposurePreview,Revision:1,DesiredState:"enabled",
 }
 other:=projectworkspace.Endpoint{
  ID:"other",ProjectRuntimeID:"runtime",ApplicationID:&unrelated,
  Protocol:"http",InternalPort:9000,Revision:1,DesiredState:"enabled",
 }
 original:=[]projectworkspace.Endpoint{a,other}
 first:=endpointPlanDigest(original,app)
 if first==""{t.Fatal("missing stable endpoint hash")}
 if first!=endpointPlanDigest([]projectworkspace.Endpoint{other,a},app){
  t.Fatal("different DB scan order altered OCI operation identity")
 }
 unrelatedChanged:=other
 unrelatedChanged.InternalPort=9999
 if first!=endpointPlanDigest([]projectworkspace.Endpoint{a,unrelatedChanged},app){
  t.Fatal("unrelated Workspace application forced a new OCI mutation")
 }
 revised:=a
 revised.Revision++
 if first==endpointPlanDigest([]projectworkspace.Endpoint{revised,other},app){
  t.Fatal("revised endpoint reused old OCI ensure operation")
 }
 changedPort:=a
 changedPort.InternalPort=8443
 if first==endpointPlanDigest([]projectworkspace.Endpoint{changedPort,other},app){
  t.Fatal("changed declared OCI port reused old operation")
 }
 changedDesired:=a
 changedDesired.DesiredState="disabled"
 if first==endpointPlanDigest([]projectworkspace.Endpoint{changedDesired,other},app){
  t.Fatal("disabled port retained old operation identity")
 }
 added:=append(append([]projectworkspace.Endpoint(nil),original...),
  projectworkspace.Endpoint{ID:"new",ProjectRuntimeID:"runtime",
   ApplicationID:&app,InternalPort:8181,Revision:1,DesiredState:"enabled"})
 if first==endpointPlanDigest(added,app){
  t.Fatal("newly declared port incorrectly reused old committed operation")
 }
 runtime:=projectworkspace.ProjectRuntime{ID:"runtime",Revision:7}
 application:=projectworkspace.Application{ID:app,Revision:3}
 plans:=func(es []projectworkspace.Endpoint)string{
  return appEnsurePlan(runtime,application,endpointSpecsForApp(es,app),
   endpointPlanDigest(es,app)).IdempotencyKey
 }
 if plans(original)==plans([]projectworkspace.Endpoint{changedPort,other}){
  t.Fatal("OCI ensure operation key did not change for edited port")
 }
 if plans(original)!=plans([]projectworkspace.Endpoint{other,a}){
  t.Fatal("equivalent route plans have different OCI ensure operation keys")
 }
}
