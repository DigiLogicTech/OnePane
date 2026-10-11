package workspacebroker

import (
 "context"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "net/url"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
)

type fakePolicy struct{
 link projectworkspace.WorkspaceServiceLink
 route projectworkspace.IngressRoute
 runtime projectworkspace.ProjectRuntime
 reads int
 onRead func(int)
}
func(f *fakePolicy) WorkspaceServiceLink(context.Context,string)(projectworkspace.WorkspaceServiceLink,error){
 f.reads++
 if f.onRead!=nil{f.onRead(f.reads)}
 return f.link,nil
}
func(f *fakePolicy) ResolveWorkspaceServiceRoute(context.Context,string,string,string)(projectworkspace.IngressRoute,error){
 return f.route,nil
}
func(f *fakePolicy) Runtime(context.Context,string)(projectworkspace.ProjectRuntime,error){
 return f.runtime,nil
}
type fakeContainer struct{
 state sandboxrunner.ContainerState
 inspects int
 onInspect func(int)
}
func(f *fakeContainer) InspectContainer(context.Context,string,string)(sandboxrunner.ContainerState,error){
 f.inspects++
 if f.onInspect!=nil{f.onInspect(f.inspects)}
 return f.state,nil
}
func pstr(v string)*string{return &v}

func brokerFixture(t *testing.T,port int)(*Broker,*fakePolicy,*fakeContainer){
 t.Helper()
 root:=t.TempDir()
 workspace:=filepath.Join(root,"projects","runtime-world","workspace")
 if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
 route:=projectworkspace.IngressRoute{
  ProjectID:"project",WorkspaceID:"tenant",
  Endpoint:projectworkspace.Endpoint{
   ID:"endpoint",ProjectRuntimeID:"runtime-world",
   ApplicationID:pstr("app"),Protocol:"http",InternalPort:8080,
   DesiredState:"enabled",Status:"ready",Revision:7,
  },
  Route:projectworkspace.EndpointRoute{
   EndpointID:"endpoint",ProjectRuntimeID:"runtime-world",
   ApplicationID:"app",HostIP:"127.0.0.1",HostPort:port,
   TransportProtocol:"tcp",Status:"verified",
   ApplicationRevision:4,EndpointRevision:7,
   ContainerSpecHash:"sha256:approved",VerificationID:"verify1",
   ObservationID:"obs1",
  },
 }
 policy:=&fakePolicy{
  link:projectworkspace.WorkspaceServiceLink{
   ID:"grant",ProjectID:"project",SourceWorkspaceID:"world",
   TargetWorkspaceID:"story",EndpointID:"endpoint",ApprovedPath:"/health",
   Enabled:true,ExpiresAtMS:9999999999999,Revision:1,
   ApplicationID:"app",ApplicationRevision:4,EndpointRevision:7,
   SpecHash:"sha256:approved",VerificationID:"verify1",
  },
  route:route,
  runtime:projectworkspace.ProjectRuntime{
   ID:"runtime-world",ProjectID:"project",ProjectWorkspaceID:pstr("world"),
   NodeID:pstr("node-local"),Status:projectworkspace.RuntimeRunning,
   IsolationMode:projectworkspace.IsolationSandboxedContainer,
  },
 }
 engine:=&fakeContainer{state:sandboxrunner.ContainerState{
  ID:"container-1",RuntimeID:"runtime-world",ApplicationID:"app",
  SpecHash:"sha256:approved",Status:"running",ReadOnlyRootFS:true,
  IsolationVerified:true,NetworkInternal:true,
  Mounts:[]sandboxrunner.MountState{
   {Type:"bind",Source:workspace,Destination:"/workspace",RW:true},
   {Type:"tmpfs",Destination:"/tmp"},
  },
  Ports:[]sandboxrunner.PortState{
   {InternalPort:8080,HostIP:"127.0.0.1",HostPort:port,Protocol:"tcp"},
  },
 }}
 return New(policy,engine,root,"node-local"),policy,engine
}
func serverPort(t *testing.T,s *httptest.Server)int{
 t.Helper()
 u,err:=url.Parse(s.URL)
 if err!=nil{t.Fatal(err)}
 n,err:=strconv.Atoi(u.Port())
 if err!=nil{t.Fatal(err)}
 return n
}
func TestOnlyDeclassifiedNodeLocalReadinessAndFreshContainer(t *testing.T){
 hits:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits++
  if r.Method!="GET"||r.URL.Path!="/health"||r.URL.RawQuery!=""{
   t.Errorf("injected request: %s %q",r.Method,r.URL.String())
  }
  if r.Header.Get("Cookie")!=""||r.Header.Get("Authorization")!=""||
   r.Header.Get("X-Forwarded-Host")!=""||r.Header.Get("Proxy-Authorization")!=""{
   t.Error("secret/control-plane header reached Workspace service")
  }
  w.Header().Set("Content-Type","application/json")
  w.Header().Set("Set-Cookie","session=this-must-not-leak")
  w.Header().Set("Authorization","Bearer forbidden")
  _,_=w.Write([]byte(`{"status":"healthy","secret":"must-never-escape","model_key":"sk-private"}`))
 }))
 defer upstream.Close()
 broker,policy,engine:=brokerFixture(t,serverPort(t,upstream))
 result,err:=broker.Probe(context.Background(),Request{ProjectID:"project",TargetWorkspaceID:"story",LinkID:"grant"})
 if err!=nil{t.Fatal(err)}
 if !result.Healthy||result.Status!="healthy"||result.StatusCode!=200||result.LinkID!="grant"{
  t.Fatalf("unexpected sanitized receipt %+v",result)
 }
 if hits!=1||engine.inspects!=2||policy.reads!=2{
  t.Fatalf("missing independent admission or post-response checks: hits=%d inspections=%d authorizations=%d",hits,engine.inspects,policy.reads)
 }
 // The public payload must not contain any untrusted response body/headers,
 // local loopback mapping, OCI identity or service authentication hints.
 content:=fmt.Sprintf("%+v",result)
 for _,danger:=range []string{"sk-private","session","container-1","127.0.0.1","must-never-escape"}{
  if strings.Contains(content,danger){t.Fatalf("secret leaked into receipt: %q",content)}
 }
}
func TestReadinessBrokerDeniesWrongTargetAndChangedMountWithoutDial(t *testing.T){
 hits:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits++
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"ready":true}`))
 }))
 defer upstream.Close()
 request:=Request{ProjectID:"project",TargetWorkspaceID:"story",LinkID:"grant"}
 for _,tc:=range []struct{name string;change func(*Broker,*fakePolicy,*fakeContainer)}{
  {"wrong_target",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.link.TargetWorkspaceID="other"}},
  {"disabled",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.link.Enabled=false}},
  {"expired",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.link.Expired=true}},
  {"unapproved_path",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.link.ApprovedPath="/admin"}},
  {"wrong_local_node",func(b *Broker,_ *fakePolicy,_ *fakeContainer){b.nodeID="different"}},
  {"wrong_runtime_owner",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.runtime.ProjectWorkspaceID=pstr("other")}},
  {"runtime_not_running",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.runtime.Status=projectworkspace.RuntimeStopped}},
  {"stale_verification",func(_ *Broker,p *fakePolicy,_ *fakeContainer){p.route.Route.VerificationID="different"}},
  {"unverified_container",func(_ *Broker,_ *fakePolicy,e *fakeContainer){e.state.IsolationVerified=false}},
  {"extra_host_mount",func(_ *Broker,_ *fakePolicy,e *fakeContainer){
   e.state.Mounts=append(e.state.Mounts,sandboxrunner.MountState{Type:"bind",Source:"/etc",Destination:"/etc",RW:true})
  }},
  {"wrong_workspace_root",func(_ *Broker,_ *fakePolicy,e *fakeContainer){e.state.Mounts[0].Source="/tmp/other"}},
  {"public_port",func(_ *Broker,_ *fakePolicy,e *fakeContainer){e.state.Ports[0].HostIP="0.0.0.0"}},
  {"private_network_missing",func(_ *Broker,_ *fakePolicy,e *fakeContainer){e.state.NetworkInternal=false}},
  {"port_not_observed",func(_ *Broker,_ *fakePolicy,e *fakeContainer){e.state.Ports[0].HostPort++}},
 }{
  t.Run(tc.name,func(t *testing.T){
   b,p,e:=brokerFixture(t,serverPort(t,upstream))
   before:=hits
   tc.change(b,p,e)
   if _,err:=b.Probe(context.Background(),request);err==nil{
    t.Fatal("unsafe Workspace request was admitted")
   }
   if hits!=before{t.Fatal("policy rejection dialled source service")}
  })
 }
}
func TestReadinessBrokerDropsResultOnRevocationOrRestart(t *testing.T){
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"ready":true}`))
 }))
 defer upstream.Close()
 for _,tc:=range []struct{name string;change func(*fakePolicy,*fakeContainer)}{
  {"revoked_during_request",func(p *fakePolicy,_ *fakeContainer){
   p.onRead=func(n int){if n==2{p.link.Enabled=false}}
  }},
  {"renewed_during_request",func(p *fakePolicy,_ *fakeContainer){
   p.onRead=func(n int){if n==2{p.link.Revision++}}
  }},
  {"container_swapped_during_request",func(_ *fakePolicy,e *fakeContainer){
   e.onInspect=func(n int){if n==2{e.state.ID="replacement"}}
  }},
  {"new_unverified_mount",func(_ *fakePolicy,e *fakeContainer){
   e.onInspect=func(n int){if n==2{e.state.Mounts[0].Source="/tmp/other"}}
  }},
  {"route_reverified_during_request",func(p *fakePolicy,_ *fakeContainer){
   p.onRead=func(n int){if n==2{p.route.Route.ObservationID="new-observation"}}
  }},
 }{
  t.Run(tc.name,func(t *testing.T){
   b,p,e:=brokerFixture(t,serverPort(t,upstream))
   tc.change(p,e)
   if _,err:=b.Probe(context.Background(),Request{ProjectID:"project",TargetWorkspaceID:"story",LinkID:"grant"});err==nil{
    t.Fatal("sensitive result released despite permission or OCI identity change")
   }
  })
 }
}
func TestReadinessNetworkIsStrictlyBoundedAndNeverRedirects(t *testing.T){
 cases:=[]struct{name string;handler http.HandlerFunc;want error}{
  {"redirect",func(w http.ResponseWriter,r *http.Request){http.Redirect(w,r,"http://169.254.169.254/latest/meta-data/",302)},ErrInvalidResponse},
  {"huge",func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Content-Type","application/json")
   _,_=w.Write([]byte(`{"status":"ok","padding":"`+strings.Repeat("x",65<<10)+`"}`))
  },ErrInvalidResponse},
  {"unsupported_media",func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Content-Type","text/plain")
   _,_=w.Write([]byte("secret"))
  },ErrInvalidResponse},
  {"unrecognized_status",func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Content-Type","application/json")
   _,_=w.Write([]byte(`{"status":"token=secret"}`))
  },ErrInvalidResponse},
  {"not_ready",func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Content-Type","application/json")
   _,_=w.Write([]byte(`{"healthy":false}`))
  },nil},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   s:=httptest.NewServer(tc.handler)
   defer s.Close()
   b,_,_:=brokerFixture(t,serverPort(t,s))
   got,err:=b.Probe(context.Background(),Request{ProjectID:"project",TargetWorkspaceID:"story",LinkID:"grant"})
   if tc.want!=nil{
    if !errors.Is(err,tc.want){t.Fatalf("expected %v got receipt=%+v err=%v",tc.want,got,err)}
   }else if err!=nil||got.Healthy||got.Status!="unhealthy"{
    t.Fatalf("unhealthy status not normalized: %+v %v",got,err)
   }
  })
 }
}
