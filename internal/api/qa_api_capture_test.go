package api

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestQACaptureOptInOwnerExpiryBoundsAndNoRawFields(t *testing.T){
 c:=&qaAPICapture{}
 now:=time.Date(2026,10,10,3,0,0,0,time.UTC)
 if _,ok:=c.view(now,"admin","credential");ok{t.Fatal("unstarted capture visible")}
 for _,bad:=range []struct{level string;seconds int}{
  {"",60},{"verbose",301},{"normal",29},{"debug",60},
 }{
  if _,err:=c.start(now,"admin","credential",bad.level,bad.seconds);!errors.Is(err,errQAInvalidCapture){t.Fatalf("bad options accepted: %+v",bad)}
 }
 session,err:=c.start(now,"admin","credential","verbose",30)
 if err!=nil{t.Fatal(err)}
 if len(session.CaptureID)!=48||session.Status!="recording"{t.Fatalf("incorrect session creation: %+v",session)}
 if _,err=c.start(now,"admin","credential","normal",60);!errors.Is(err,errQAAlreadyActive){t.Fatal("concurrent start accepted")}
 if _,ok:=c.view(now,"someoneelse","credential");ok{t.Fatal("cross-operator read allowed")}
 if _,ok:=c.view(now,"admin","other-credential");ok{t.Fatal("cross-session read allowed")}
 if _,ok:=c.stop(now,"admin","other-credential");ok{t.Fatal("cross-credential stop allowed")}
 c.observe(now,session.CaptureID,"admin","credential","/v1/projects/private?token=SECRET_CANARY","POST",502,120*time.Millisecond)
 c.observe(now,session.CaptureID,"admin","wrong","/v1/tasks/x","GET",200,10*time.Millisecond)
 c.observe(now,"forged","admin","credential","/v1/tasks/x","GET",200,10*time.Millisecond)
 v,ok:=c.view(now.Add(time.Second),"admin","credential")
 if !ok||v.ObservedRequests!=1||len(v.Observations)!=1||len(v.Counters)!=1{t.Fatalf("capture not scoped: %+v",v)}
 if v.Observations[0].Subsystem!="projects"||v.Observations[0].StatusCode!=502{t.Fatalf("not projected: %+v",v)}
 raw,_:=json.Marshal(v)
 for _,secret:=range []string{"SECRET_CANARY","/v1/projects/private","someoneelse","credentialA","raw_path","request_body"}{
  if strings.Contains(string(raw),secret){t.Fatalf("unallowlisted value %q leaked: %s",secret,raw)}
 }
 for i:=0;i<90;i++{
  c.observe(now.Add(time.Duration(i)*time.Millisecond),session.CaptureID,"admin","credential","/v1/tasks/"+strings.Repeat("x",100),"GET",200,2*time.Millisecond)
 }
 v,_=c.view(now.Add(3*time.Second),"admin","credential")
 if v.ObservedRequests!=91||len(v.Observations)!=64||v.DroppedObservations!=27{t.Fatalf("incorrect bounded ring: %+v",v)}
 if v.InstrumentationUpdateUS<0{t.Fatal("negative observer overhead")}
 ended,ok:=c.view(now.Add(31*time.Second),"admin","credential")
 if !ok||ended.Status!="expired"||len(ended.Observations)!=64{t.Fatalf("not automatically expired: %+v",ended)}
 c.observe(now.Add(32*time.Second),session.CaptureID,"admin","credential","/v1/tasks/x","POST",200,time.Second)
 ended,_=c.view(now.Add(33*time.Second),"admin","credential")
 if ended.ObservedRequests!=91{t.Fatal("observations appended after expiration")}
 if _,ok:=c.view(now.Add(30*time.Second).Add(qaAPIRetention),"admin","credential");ok{t.Fatal("expired trace retained too long")}
 resumed,err:=c.start(now.Add(3*time.Minute),"admin","credential","normal",60)
 if err!=nil{t.Fatal(err)}
 c.observe(now.Add(3*time.Minute+time.Second),resumed.CaptureID,"admin","credential","/v1/tasks/x","GET",200,10*time.Millisecond)
 normal,_:=c.stop(now.Add(3*time.Minute+2*time.Second),"admin","credential")
 if normal.Level!="normal"||len(normal.Observations)!=0||len(normal.Counters)!=1||normal.ObservedRequests!=1{t.Fatalf("normal mode must not expose per-request rows: %+v",normal)}
 if _,ok:=c.stop(now.Add(3*time.Minute+3*time.Second),"other","credential");ok{t.Fatal("stopped capture stolen")}
}

type qaCaptureTestAuth struct{}
func (qaCaptureTestAuth) Authenticate(r *http.Request)(Identity,error){
 if !strings.HasPrefix(r.Header.Get("Authorization"),"Bearer "){return Identity{},ErrUnauthenticated}
 auth:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")
 if auth=="adminA"{return Identity{PrincipalID:"admin",CredentialID:"credentialA"},nil}
 if auth=="adminB"{return Identity{PrincipalID:"admin",CredentialID:"credentialB"},nil}
 if auth=="outsider"{return Identity{PrincipalID:"outsider",CredentialID:"credentialC"},nil}
 return Identity{},ErrUnauthenticated
}
func (qaCaptureTestAuth) AuthorizeWorkspace(context.Context,Identity,string,string)error{return nil}
type qaCaptureTestFederation struct{nodeFederationService}
func (*qaCaptureTestFederation)CanOperate(_ context.Context,id string)error{
 if id=="admin"{return nil}
 return errors.New("not an operator")
}

func TestQACaptureHTTPRequiresAdminAndTagsOnlyOwnRequests(t *testing.T){
 s:=NewServer(nil,nil,qaCaptureTestAuth{})
 s.SetFederation(&qaCaptureTestFederation{})
 handler:=s.Handler()
 call:=func(method,path,bearer,body string,headers map[string]string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if bearer!=""{req.Header.Set("Authorization","Bearer "+bearer)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  for k,v:=range headers{req.Header.Set(k,v)}
  rw:=httptest.NewRecorder();handler.ServeHTTP(rw,req);return rw
 }
 if got:=call("POST","/v1/qa/api-capture/start","","{\"level\":\"verbose\",\"seconds\":60}",nil);got.Code!=401{t.Fatalf("unauthenticated start: %d",got.Code)}
 if got:=call("POST","/v1/qa/api-capture/start","outsider","{\"level\":\"verbose\",\"seconds\":60}",nil);got.Code!=403{t.Fatalf("nonadmin start: %d",got.Code)}
 started:=call("POST","/v1/qa/api-capture/start","adminA","{\"level\":\"verbose\",\"seconds\":60}",nil)
 if started.Code!=201{t.Fatalf("admin start: %d %s",started.Code,started.Body)}
 var view qaAPICaptureView
 if err:=json.Unmarshal(started.Body.Bytes(),&view);err!=nil{t.Fatal(err)}
 if len(view.CaptureID)!=48{t.Fatal("missing unpredictable token")}
 tag:=map[string]string{"X-OnePane-QA-Capture":view.CaptureID,"Accept":"application/json"}
 // Handler instrumentation is exercised independently of the normal mux.
 observer:=s.qaCaptureMiddleware(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.WriteHeader(418)
  _,_=w.Write([]byte("PRIVATE_PAYLOAD_SECRET_CANARY"))
 }))
 observe:=func(method,path,bearer string,h map[string]string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,nil)
  if bearer!=""{req.Header.Set("Authorization","Bearer "+bearer)}
  for k,v:=range h{req.Header.Set(k,v)}
  rw:=httptest.NewRecorder();observer.ServeHTTP(rw,req);return rw
 }
 for _,tc:=range []struct{path,auth string;heads map[string]string}{
  {"/v1/tasks/abc?token=SECRET","adminA",nil},
  {"/v1/tasks/abc?token=SECRET","adminB",tag},
  {"/v1/tasks/abc?token=SECRET","outsider",tag},
  {"/v1/qa/workspace-snapshot","adminA",tag},
  {"/v1/tasks/abc?token=SECRET","adminA",map[string]string{"X-OnePane-QA-Capture":view.CaptureID}},
  {"/v1/tasks/abc?token=SECRET","adminA",map[string]string{"X-OnePane-QA-Capture":"bad", "Accept":"application/json"}},
 }{
  observe("POST",tc.path,tc.auth,tc.heads)
 }
 observe("POST","/v1/tasks/abc?token=SECRET","adminA",tag)
 report:=call("GET","/v1/qa/api-capture","adminA","",nil)
 if report.Code!=200{t.Fatalf("owner status: %d",report.Code)}
 if json.Unmarshal(report.Body.Bytes(),&view)!=nil{t.Fatal("invalid report JSON")}
 if view.ObservedRequests!=1||len(view.Observations)!=1||
  view.Observations[0].StatusCode!=418||view.Observations[0].Subsystem!="tasks"{
  t.Fatalf("tag scoping failed: %+v",view)
 }
 if strings.Contains(report.Body.String(),"PRIVATE_PAYLOAD_SECRET_CANARY")||strings.Contains(report.Body.String(),"token=SECRET"){
  t.Fatal("request/response body or query leaked")
 }
 if got:=call("GET","/v1/qa/api-capture","adminB","",nil);got.Code!=404{t.Fatalf("other credential read allowed: %d",got.Code)}
 if got:=call("POST","/v1/qa/api-capture/stop","outsider","",nil);got.Code!=403{t.Fatalf("nonadmin stop allowed: %d",got.Code)}
 if got:=call("POST","/v1/qa/api-capture/stop","adminA","",nil);got.Code!=200{t.Fatalf("owner stop: %d",got.Code)}
 observe("POST","/v1/tasks/abc","adminA",tag)
 post:=call("GET","/v1/qa/api-capture","adminA","",nil)
 if json.Unmarshal(post.Body.Bytes(),&view)!=nil||view.ObservedRequests!=1{t.Fatal("stopped capture continued")}
}
