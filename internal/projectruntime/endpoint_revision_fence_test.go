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
