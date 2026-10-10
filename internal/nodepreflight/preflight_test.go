package nodepreflight

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
)

type stubInspector struct{
 rootless bool
 probeErr error
 exists bool
 imageErr error
 probeCalls int
 imageCalls int
 refs []string
}
func(s *stubInspector) RootlessPodman(context.Context)(bool,error){
 s.probeCalls++
 return s.rootless,s.probeErr
}
func(s *stubInspector) ImagePresent(_ context.Context,ref string)(bool,error){
 s.imageCalls++
 s.refs=append(s.refs,ref)
 return s.exists,s.imageErr
}
func imageRef()string{return "docker.io/library/python@sha256:"+strings.Repeat("a",64)}
func env(values map[string]string)func(string)string{return func(name string)string{return values[name]}}

func TestMissingApprovalAndRootfulNodeAreNotPhysicalPasses(t *testing.T){
 ctx:=context.Background()
 p:=&stubInspector{rootless:true,exists:true}
 report,err:=Check(ctx,p,env(nil),"linux","")
 if err!=nil||report.ReadyForPhysicalTest||report.PhysicalAcceptanceVerified||
  report.Podman!="rootless"||len(report.ImageChecks)!=3||p.imageCalls!=0{
  t.Fatalf("unapproved physical image incorrectly accepted %+v %v",report,err)
 }
 for _,c:=range report.ImageChecks{if c.State!="approval_missing"{t.Fatalf("%+v",c)}}
 rootful:=&stubInspector{rootless:false,exists:true}
 r,err:=Check(ctx,rootful,env(map[string]string{PythonImageEnv:imageRef()}),"linux",PythonImageEnv)
 if err!=nil||r.Podman!="not_rootless"||r.ReadyForPhysicalTest||
  r.ImageChecks[0].State!="node_unavailable"||rootful.imageCalls!=0{
  t.Fatalf("rootful Podman was used: %+v %v",r,err)
 }
 other:=&stubInspector{rootless:true,exists:true}
 r,err=Check(ctx,other,env(map[string]string{PythonImageEnv:imageRef()}),"windows",PythonImageEnv)
 if err!=nil||r.Podman!="unsupported_platform"||other.probeCalls!=0||other.imageCalls!=0{
  t.Fatalf("Windows attempted Linux Node operation: %+v %v",r,err)
 }
}
func TestOnlyPinnedApprovedImagesAreReadFromTheLocalStore(t *testing.T){
 invalid:=[]string{"python:latest","docker.io/python@sha256:123"," docker.io/python@sha256:"+strings.Repeat("b",64),
  "http://unsafe/image@sha256:"+strings.Repeat("c",64),
  "image@sha256:"+strings.Repeat("a",64)+";echo-secret",
 }
 for _,reference:=range invalid{
  p:=&stubInspector{rootless:true,exists:true}
  report,err:=Check(context.Background(),p,
   env(map[string]string{PythonImageEnv:reference}),"linux",PythonImageEnv)
  if err!=nil||report.ImageChecks[0].State!="invalid_immutable_reference"||p.imageCalls!=0||
   report.ReadyForPhysicalTest{
   t.Fatalf("invalid image triggered process or passed check: %+v %v calls=%d",report,err,p.imageCalls)
  }
 }
 good:=imageRef()
 p:=&stubInspector{rootless:true,exists:true}
 report,err:=Check(context.Background(),p,env(map[string]string{PythonImageEnv:good}),"linux",PythonImageEnv)
 if err!=nil||!report.ReadyForPhysicalTest||report.PhysicalAcceptanceVerified||
  report.ImageChecks[0].State!="locally_present"||p.imageCalls!=1||
  len(p.refs)!=1||p.refs[0]!=good{
  t.Fatalf("pre-pulled immutable image not accepted: %+v %v",report,err)
 }
 encoded,e:=json.Marshal(report)
 if e!=nil||strings.Contains(string(encoded),good)||
  strings.Contains(string(encoded),"docker.io/library/python")||
  strings.Contains(string(encoded),"rootless_service_account"){
  t.Fatalf("image identity leaked through preflight report: %s",encoded)
 }
 // The report never claims a successful physical test, even when ready.
 if report.PhysicalAcceptanceVerified{t.Fatal("prerequisites forged acceptance")}
}
func TestCheckSeparatesMissingImageAndProbeFailure(t *testing.T){
 ref:=imageRef()
 tests:=[]struct{
  name string
  inspector *stubInspector
  wantPodman,wantImage string
 }{
  {"not_cached",&stubInspector{rootless:true,exists:false},"rootless","image_not_present"},
  {"image_engine_error",&stubInspector{rootless:true,imageErr:errors.New("secret path /var/lib")},"rootless","image_check_failed"},
  {"podman_failed",&stubInspector{probeErr:errors.New("secret machine identity")},"unavailable","node_unavailable"},
 }
 for _,tt:=range tests{
  t.Run(tt.name,func(t *testing.T){
   report,err:=Check(context.Background(),tt.inspector,env(map[string]string{PythonImageEnv:ref}),"linux",PythonImageEnv)
   if err!=nil||report.ReadyForPhysicalTest||report.Podman!=tt.wantPodman||
    report.ImageChecks[0].State!=tt.wantImage{
    t.Fatalf("wrong fail-closed status: %+v %v",report,err)
   }
   encoded,_:=json.Marshal(report)
   if strings.Contains(string(encoded),"secret"){t.Fatalf("sensitive diagnostic leaked: %s",encoded)}
  })
 }
 if _,err:=Check(context.Background(),&stubInspector{rootless:true},env(nil),"linux","UNAPPROVED_NAME");
  !errors.Is(err,ErrInvalidVariable){t.Fatalf("arbitrary environment key read: %v",err)}
}
