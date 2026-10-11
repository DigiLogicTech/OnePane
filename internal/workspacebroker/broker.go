// Package workspacebroker implements a bounded Node-local readiness probe for
// one human-approved cross-Workspace service grant. It is NOT a generic HTTP
// proxy, an Agent tool or a public API endpoint. Callers must first be bound
// by the trusted Task Gateway to the exact target Project Workspace.
package workspacebroker

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "strconv"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
)

var (
 ErrDenied=errors.New("Workspace service probe denied")
 ErrUnavailable=errors.New("verified Node-local Workspace service unavailable")
 ErrInvalidResponse=errors.New("invalid bounded service readiness response")
)

const maxReadinessBytes int64=64<<10

// PolicyStore does not provide a model-selected upstream. It validates exact
// target Workspace authority, link validity, route revision and OCI spec ID.
type PolicyStore interface {
 WorkspaceServiceLink(context.Context,string)(projectworkspace.WorkspaceServiceLink,error)
 ResolveWorkspaceServiceRoute(context.Context,string,string,string)(projectworkspace.IngressRoute,error)
 Runtime(context.Context,string)(projectworkspace.ProjectRuntime,error)
}

// SourceInspector must query the running OCI engine, not cached observations.
type SourceInspector interface {
 InspectContainer(context.Context,string,string)(sandboxrunner.ContainerState,error)
}

type Broker struct {
 policy PolicyStore
 source SourceInspector
 dataRoot string
 nodeID string
 slots chan struct{}
}

type Request struct {
 ProjectID string
 TargetWorkspaceID string
 LinkID string
}

// Receipt is the *entire* declassified result. It contains neither raw
// upstream bytes/headers nor host/port, credentials, container ID or secrets.
type Receipt struct {
 LinkID string `json:"link_id"`
 Healthy bool `json:"healthy"`
 Status string `json:"status"`
 StatusCode int `json:"status_code"`
}
func New(policy PolicyStore,source SourceInspector,dataRoot,nodeID string)*Broker{
 return &Broker{policy:policy,source:source,dataRoot:dataRoot,nodeID:nodeID,slots:make(chan struct{},2)}
}
func allowedReadinessPath(p string)bool{
 switch p{case "/health","/healthz","/ready","/readyz","/status":return true}
 return false
}
func validFreshRoute(route projectworkspace.IngressRoute,link projectworkspace.WorkspaceServiceLink)bool{
 return route.Endpoint.ID==link.EndpointID&&route.Endpoint.Protocol=="http"&&
  route.Endpoint.DesiredState=="enabled"&&route.Endpoint.Status=="ready"&&
  route.Route.Status=="verified"&&route.Route.EndpointID==link.EndpointID&&
  route.Route.ProjectRuntimeID==route.Endpoint.ProjectRuntimeID&&
  route.Route.ApplicationID==link.ApplicationID&&
  route.Route.ApplicationRevision==link.ApplicationRevision&&
  route.Route.EndpointRevision==link.EndpointRevision&&
  route.Route.ContainerSpecHash==link.SpecHash&&
  route.Route.VerificationID==link.VerificationID&&
  route.Route.HostIP=="127.0.0.1"&&route.Route.TransportProtocol=="tcp"&&
  route.Route.HostPort>0&&route.Route.HostPort<=65535&&
  route.Endpoint.InternalPort>0&&route.Endpoint.InternalPort<=65535
}
func exactLoopbackPort(state sandboxrunner.ContainerState,route projectworkspace.IngressRoute)bool{
 count:=0
 for _,p:=range state.Ports{
  if p.HostIP!="127.0.0.1"||p.HostPort<1||p.HostPort>65535{return false}
  if p.HostPort==route.Route.HostPort && p.InternalPort==route.Endpoint.InternalPort&&
   (p.Protocol=="tcp"||p.Protocol==""){count++}
 }
 return count==1
}
func sameRoute(x,y projectworkspace.IngressRoute)bool{
 return x.Endpoint.ID==y.Endpoint.ID&&
  x.Endpoint.Revision==y.Endpoint.Revision&&
  x.Route.ApplicationID==y.Route.ApplicationID&&
  x.Route.ApplicationRevision==y.Route.ApplicationRevision&&
  x.Route.EndpointRevision==y.Route.EndpointRevision&&
  x.Route.VerificationID==y.Route.VerificationID&&
  x.Route.ContainerSpecHash==y.Route.ContainerSpecHash&&
  x.Route.ObservationID==y.Route.ObservationID&&
  x.Route.HostIP==y.Route.HostIP&&x.Route.HostPort==y.Route.HostPort
}

// Probe performs a single unauthenticated *GET* for a small fixed readiness
// resource on an independently inspected, private rootless OCI service.
//
// Calling this method does not by itself establish caller/Task authority.
// Do not expose it through an unscoped HTTP handler, a model tool, or a browser
// until the Task Gateway has a trusted target-Workspace binding and per-call
// lease, and Node-local physical acceptance has passed.
func (b *Broker) Probe(ctx context.Context,req Request)(Receipt,error){
 if b==nil||b.policy==nil||b.source==nil||b.dataRoot==""||b.nodeID==""||
  req.ProjectID==""||req.TargetWorkspaceID==""||req.LinkID==""{
  return Receipt{},ErrDenied
 }
 ctx,cancel:=context.WithTimeout(ctx,6*time.Second)
 defer cancel()
 select {
 case b.slots<-struct{}{}:defer func(){<-b.slots}()
 case <-ctx.Done():return Receipt{},ctx.Err()
 }
 link,err:=b.policy.WorkspaceServiceLink(ctx,req.LinkID)
 if err!=nil||link.ProjectID!=req.ProjectID||link.TargetWorkspaceID!=req.TargetWorkspaceID||
  !link.Enabled||link.Expired||!allowedReadinessPath(link.ApprovedPath) {
  return Receipt{},ErrDenied
 }
 route,err:=b.policy.ResolveWorkspaceServiceRoute(ctx,req.ProjectID,req.TargetWorkspaceID,req.LinkID)
 if err!=nil||!validFreshRoute(route,link){return Receipt{},ErrDenied}
 runtime,err:=b.policy.Runtime(ctx,route.Route.ProjectRuntimeID)
 if err!=nil||runtime.ProjectID!=req.ProjectID||
  runtime.ProjectWorkspaceID==nil||*runtime.ProjectWorkspaceID!=link.SourceWorkspaceID||
  runtime.NodeID==nil||*runtime.NodeID!=b.nodeID||
  runtime.Status!=projectworkspace.RuntimeRunning||
  runtime.IsolationMode!=projectworkspace.IsolationSandboxedContainer{
  return Receipt{},ErrDenied
 }
 before,err:=b.source.InspectContainer(ctx,runtime.ID,link.ApplicationID)
 if err!=nil||!sandboxrunner.VerifyNodeLocalServiceSource(b.dataRoot,runtime.ID,link.ApplicationID,before)||
  before.SpecHash!=route.Route.ContainerSpecHash||!exactLoopbackPort(before,route){
  return Receipt{},ErrUnavailable
 }
 statusCode,status,healthy,err:=getReadiness(ctx,route.Route.HostPort,link.ApprovedPath)
 if err!=nil{return Receipt{},err}
 // Revocation, expiry, stale observations, rebuild/restart, port switch,
 // and source identity changes must fail before returning *any* result.
 fresh,err:=b.policy.WorkspaceServiceLink(ctx,req.LinkID)
 if err!=nil||!fresh.Enabled||fresh.Expired||fresh.Revision!=link.Revision||
  fresh.ExpiresAtMS!=link.ExpiresAtMS||fresh.ApprovedPath!=link.ApprovedPath{
  return Receipt{},ErrDenied
 }
 afterRoute,err:=b.policy.ResolveWorkspaceServiceRoute(ctx,req.ProjectID,req.TargetWorkspaceID,req.LinkID)
 if err!=nil||!sameRoute(afterRoute,route){return Receipt{},ErrDenied}
 after,err:=b.source.InspectContainer(ctx,runtime.ID,link.ApplicationID)
 if err!=nil||!sandboxrunner.VerifyNodeLocalServiceSource(b.dataRoot,runtime.ID,link.ApplicationID,after)||
  before.ID!=after.ID||before.SpecHash!=after.SpecHash||
  !exactLoopbackPort(after,route){
  return Receipt{},ErrUnavailable
 }
 return Receipt{LinkID:link.ID,Healthy:healthy,Status:status,StatusCode:statusCode},nil
}

// Return only a normalized health enum or boolean. No response headers,
// body fragments, text of errors, cookies, traces or server identities.
func normalizedReadiness(body []byte)(string,bool,error){
 var v map[string]json.RawMessage
 if err:=json.Unmarshal(body,&v);err!=nil||v==nil{
  return "",false,ErrInvalidResponse
 }
 if raw,ok:=v["status"];ok{
  var name string
  if json.Unmarshal(raw,&name)!=nil{return "",false,ErrInvalidResponse}
  switch strings.ToLower(name){
  case "ok","up","ready","healthy":return "healthy",true,nil
  case "degraded","unhealthy","down","not_ready":return "unhealthy",false,nil
  }
  return "",false,ErrInvalidResponse
 }
 for _,k:=range []string{"healthy","ready"} {
  if raw,ok:=v[k];ok{
   var yes bool
   if json.Unmarshal(raw,&yes)!=nil{return "",false,ErrInvalidResponse}
   if yes{return "healthy",true,nil}
   return "unhealthy",false,nil
  }
 }
 return "",false,ErrInvalidResponse
}

func getReadiness(ctx context.Context,port int,resource string)(int,string,bool,error){
 if port<1||port>65535||!allowedReadinessPath(resource){
  return 0,"",false,ErrDenied
 }
 address:=net.JoinHostPort("127.0.0.1",strconv.Itoa(port))
 // Proxy environment variables, name resolution and redirections are all
 // disabled. The dialer connects to exactly the vetted loopback port.
 transport:=&http.Transport{
  Proxy:nil,DisableCompression:true,DisableKeepAlives:true,
  DialContext:func(ctx context.Context,network,target string)(net.Conn,error){
   if target!=address||network!="tcp" {return nil,ErrDenied}
   return (&net.Dialer{Timeout:2*time.Second}).DialContext(ctx,"tcp",address)
  },
  ResponseHeaderTimeout:3*time.Second,
 }
 defer transport.CloseIdleConnections()
 client:=&http.Client{Transport:transport,
  CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
 }
 httpReq,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  "http://"+address+resource,nil)
 if err!=nil{return 0,"",false,ErrDenied}
 httpReq.Header.Set("Accept","application/json")
 httpReq.Header.Set("Accept-Encoding","identity")
 httpReq.Header.Set("Cache-Control","no-store")
 response,err:=client.Do(httpReq)
 if err!=nil{return 0,"",false,ErrUnavailable}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return 0,"",false,ErrInvalidResponse}
 if response.ContentLength>maxReadinessBytes||
  (response.Header.Get("Content-Encoding")!=""&&response.Header.Get("Content-Encoding")!="identity"){
  return 0,"",false,ErrInvalidResponse
 }
 contentType:=strings.ToLower(strings.TrimSpace(strings.SplitN(response.Header.Get("Content-Type"),";",2)[0]))
 if contentType!="application/json"{return 0,"",false,ErrInvalidResponse}
 body,err:=io.ReadAll(io.LimitReader(response.Body,maxReadinessBytes+1))
 if err!=nil||int64(len(body))>maxReadinessBytes{return 0,"",false,ErrInvalidResponse}
 status,healthy,err:=normalizedReadiness(body)
 if err!=nil{return 0,"",false,err}
 return response.StatusCode,status,healthy,nil
}

