package api

import (
 "crypto/rand"
 "context"
 "encoding/hex"
 "net/http"
 "strings"
 "sync"
 "time"
)

// RC11 opt-in local HTTP diagnostics. These describe *only* explicitly tagged
// requests made by the operator's own authenticated browser session. They
// are not a global traffic tap, raw logger, or cross-Workspace event stream.
const (
 qaAPIMaxEvents=64
 qaAPIMaxSeconds=300
 qaAPIMinSeconds=30
 qaAPIRetention=2*time.Minute
)
var qaAPIClasses=[]string{"projects","tasks","models","nodes","agents","providers","system","other"}
var qaAPIOutcomes=[]string{"2xx","3xx","4xx","5xx","other"}

type qaAPICounter struct{
 Subsystem string `json:"subsystem"`
 Outcome string `json:"outcome"`
 Count int `json:"count"`
}
type qaAPIObservation struct{
 AtUTC string `json:"at_utc"`
 Subsystem string `json:"subsystem"`
 Method string `json:"method"`
 StatusCode int `json:"status_code"`
 DurationMS int64 `json:"duration_ms"`
 TraceRef string `json:"trace_ref,omitempty"`
}
type qaAPICaptureView struct{
 SchemaVersion int `json:"schema_version"`
 Source string `json:"source"`
 Status string `json:"status"`
 Level string `json:"level"`
 CaptureID string `json:"capture_id"`
 StartedUTC string `json:"started_utc"`
 EndsUTC string `json:"ends_utc"`
 FinishedUTC string `json:"finished_utc,omitempty"`
 RetainedUntilUTC string `json:"retained_until_utc,omitempty"`
 ObservedRequests int `json:"observed_requests"`
 DroppedObservations int `json:"dropped_observations"`
 MaxEvents int `json:"max_events"`
 Counters []qaAPICounter `json:"counters"`
 Observations []qaAPIObservation `json:"observations"`
 InstrumentationUpdateUS int64 `json:"instrumentation_update_us"`
 Exclusions []string `json:"exclusions"`
}
type qaAPISession struct{
 id,principal,credential,level,status string
 started,ends,finished time.Time
 observed,dropped int
 counters [8][5]int
 observations []qaAPIObservation
 overheadUS int64
}
type qaAPICapture struct{
 mu sync.Mutex
 session *qaAPISession
}
func qaAPISubsystem(path string)int{
 // Bucket by a fixed path prefix, never record full path or query strings.
 switch{
 case strings.HasPrefix(path,"/v1/projects/"),path=="/v1/projects",strings.HasPrefix(path,"/v1/library/"):return 0
 case strings.HasPrefix(path,"/v1/tasks/"),path=="/v1/tasks",strings.HasPrefix(path,"/v1/routines/"):return 1
 case strings.HasPrefix(path,"/v1/local-ai/"),strings.HasPrefix(path,"/v1/model-"),
      strings.HasPrefix(path,"/v1/models/"),strings.HasPrefix(path,"/v1/model-routing/"):return 2
 case strings.HasPrefix(path,"/v1/nodes/"),path=="/v1/nodes":return 3
 case strings.HasPrefix(path,"/v1/agent-"),strings.HasPrefix(path,"/v1/teams/"),strings.HasPrefix(path,"/v1/assistant/"):return 4
 case strings.HasPrefix(path,"/v1/providers/"),strings.HasPrefix(path,"/v1/provider-"),strings.HasPrefix(path,"/v1/cloud-models/"):return 5
 case strings.HasPrefix(path,"/v1/system/"),path=="/v1/health",path=="/v1/about":return 6
 default:return 7
 }
}
func qaAPIOutcome(status int)int{
 if status>=200&&status<300{return 0}
 if status>=300&&status<400{return 1}
 if status>=400&&status<500{return 2}
 if status>=500&&status<600{return 3}
 return 4
}
func qaAPIMethod(method string)string{
 switch method{case "GET","POST","PUT","PATCH","DELETE","HEAD","OPTIONS":return method}
 return "OTHER"
}
func qaAPIRandomID()(string,error){
 buf:=make([]byte,24)
 if _,err:=rand.Read(buf);err!=nil{return "",err}
 return hex.EncodeToString(buf),nil
}
func qaAPIMs(d time.Duration)int64{
 ms:=d.Milliseconds()
 if ms<0{return 0}
 if ms>60000{return 60000}
 return ms
}
func (c *qaAPICapture) expireLocked(now time.Time){
 s:=c.session
 if s==nil{return}
 if s.status=="recording"&&!now.Before(s.ends){
  s.status="expired";s.finished=s.ends
 }
 if s.status!="recording"&&!now.Before(s.finished.Add(qaAPIRetention)){
  c.session=nil
 }
}
func(c *qaAPICapture) start(now time.Time,principal,credential,level string,seconds int)(qaAPICaptureView,error){
 if principal==""||credential==""||seconds<qaAPIMinSeconds||seconds>qaAPIMaxSeconds||
  (level!="normal"&&level!="verbose"){return qaAPICaptureView{},errQAInvalidCapture}
 id,err:=qaAPIRandomID();if err!=nil{return qaAPICaptureView{},err}
 c.mu.Lock();defer c.mu.Unlock()
 c.expireLocked(now)
 if c.session!=nil&&c.session.status=="recording"{return qaAPICaptureView{},errQAAlreadyActive}
 c.session=&qaAPISession{id:id,principal:principal,credential:credential,level:level,
  started:now,ends:now.Add(time.Duration(seconds)*time.Second),status:"recording",
  observations:make([]qaAPIObservation,0,qaAPIMaxEvents)}
 return qaAPIBuildView(c.session),nil
}
func(c *qaAPICapture) authorized(now time.Time,principal,credential string)(*qaAPISession,bool){
 c.expireLocked(now)
 s:=c.session
 return s,s!=nil&&principal!=""&&credential!=""&&s.principal==principal&&s.credential==credential
}
func(c *qaAPICapture) view(now time.Time,principal,credential string)(qaAPICaptureView,bool){
 c.mu.Lock();defer c.mu.Unlock()
 s,ok:=c.authorized(now,principal,credential)
 if !ok{return qaAPICaptureView{},false}
 return qaAPIBuildView(s),true
}
func(c *qaAPICapture) stop(now time.Time,principal,credential string)(qaAPICaptureView,bool){
 c.mu.Lock();defer c.mu.Unlock()
 s,ok:=c.authorized(now,principal,credential)
 if !ok{return qaAPICaptureView{},false}
 if s.status=="recording"{s.status="stopped";s.finished=now}
 return qaAPIBuildView(s),true
}
func(c *qaAPICapture) accepts(now time.Time,id,principal,credential string)bool{
 c.mu.Lock();defer c.mu.Unlock()
 s,ok:=c.authorized(now,principal,credential)
 return ok&&s.status=="recording"&&id!=""&&id==s.id
}
func(c *qaAPICapture) observe(now time.Time,id,principal,credential,path,method string,status int,elapsed time.Duration){
 c.observeWithTrace(now,id,principal,credential,path,method,status,elapsed,"")
}
func(c *qaAPICapture) observeWithTrace(now time.Time,id,principal,credential,path,method string,status int,elapsed time.Duration,traceRef string){
 started:=time.Now()
 c.mu.Lock();defer c.mu.Unlock()
 s,ok:=c.authorized(now,principal,credential)
 if !ok||s.status!="recording"||s.id!=id{return}
 cls,out:=qaAPISubsystem(path),qaAPIOutcome(status)
 if s.counters[cls][out]<65535{s.counters[cls][out]++}
 if s.observed<65535{s.observed++}
 if s.level=="verbose"{
  ev:=qaAPIObservation{AtUTC:now.UTC().Format(time.RFC3339Nano),
   Subsystem:qaAPIClasses[cls],Method:qaAPIMethod(method),
   StatusCode:status,DurationMS:qaAPIMs(elapsed)}
  if method=="POST"&&path=="/v1/tasks"&&status==http.StatusCreated&&
   qaSafeTraceRef(traceRef){ev.TraceRef=traceRef}
  if len(s.observations)>=qaAPIMaxEvents{
   copy(s.observations,s.observations[1:]);s.observations[len(s.observations)-1]=ev
   if s.dropped<65535{s.dropped++}
  }else{s.observations=append(s.observations,ev)}
 }
 // Only the collector's update time; authentication and writer-wrapper
 // overhead are explicitly NOT measured by this figure.
 s.overheadUS+=time.Since(started).Microseconds()
}
func qaAPIBuildView(s *qaAPISession)qaAPICaptureView{
 v:=qaAPICaptureView{SchemaVersion:1,Source:"operator_tagged_local_http",
  Status:s.status,Level:s.level,CaptureID:s.id,
  StartedUTC:s.started.UTC().Format(time.RFC3339Nano),
  EndsUTC:s.ends.UTC().Format(time.RFC3339Nano),
  MaxEvents:qaAPIMaxEvents,ObservedRequests:s.observed,
  DroppedObservations:s.dropped,InstrumentationUpdateUS:s.overheadUS,
  Counters:make([]qaAPICounter,0),Observations:make([]qaAPIObservation,0),
  Exclusions:[]string{
   "no paths, URLs, queries, request/response bodies, headers, credentials, IP addresses or principal identifiers",
   "only explicitly tagged JSON API requests from this administrator credential; no automatic global capture",
   "HTTP response codes do not prove Task/Worker, model, Tool Gateway, or artifact success",
   "collector update time excludes auth, middleware and application processing overhead",
   "off by default, limited to 30-300 seconds and 64 recent detailed observations",
  }}
 if !s.finished.IsZero(){
  v.FinishedUTC=s.finished.UTC().Format(time.RFC3339Nano)
  v.RetainedUntilUTC=s.finished.Add(qaAPIRetention).UTC().Format(time.RFC3339Nano)
 }
 for i,sub:=range qaAPIClasses{for j,out:=range qaAPIOutcomes{
  if n:=s.counters[i][j];n>0{v.Counters=append(v.Counters,qaAPICounter{Subsystem:sub,Outcome:out,Count:n})}
 }}
 v.Observations=append(v.Observations,s.observations...)
 return v
}

type qaAPIStatusWriter struct{http.ResponseWriter;status int}
func(w *qaAPIStatusWriter)WriteHeader(code int){
 if w.status==0{w.status=code;w.ResponseWriter.WriteHeader(code)}
}
func(w *qaAPIStatusWriter)Write(b []byte)(int,error){
 if w.status==0{w.status=200}
 return w.ResponseWriter.Write(b)
}
func(w *qaAPIStatusWriter)Unwrap()http.ResponseWriter{return w.ResponseWriter}
func(w *qaAPIStatusWriter)Flush(){
 if f,ok:=w.ResponseWriter.(http.Flusher);ok{f.Flush()}
}
// The context marker only originates in the already authenticated, operator-
 // owned capture middleware; untrusted X-Trace-ID request headers cannot make
 // themselves a trusted QA trace. IDs are random and never authorization tokens.
type qaTrustedTraceContextKey struct{}
func qaTrustedTaskTrace(r *http.Request)*string{
 if r==nil{return nil}
 v,ok:=r.Context().Value(qaTrustedTraceContextKey{}).(string)
 if !ok||len(v)!=48{return nil}
 return &v
}
func qaSafeTraceRef(s string)bool{
 if len(s)!=30||!strings.HasPrefix(s,"trace-"){return false}
 for _,c:=range s[6:]{if !((c>='0'&&c<='9')||(c>='a'&&c<='f')){return false}}
 return true
}
func(s *Server)qaCaptureMiddleware(next http.Handler)http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  // Uninstrumented requests have no further work, identity lookups or
  // capture-related data handling.
  token:=r.Header.Get("X-OnePane-QA-Capture")
  if token==""||len(token)!=48||!strings.HasPrefix(r.URL.Path,"/v1/")||
   strings.HasPrefix(r.URL.Path,"/v1/qa/")||
   !strings.Contains(strings.ToLower(r.Header.Get("Accept")),"application/json")||
   s.apiCapture==nil||s.auth==nil{
   next.ServeHTTP(w,r);return
  }
  i,err:=s.auth.Authenticate(r)
  if err!=nil||i.PrincipalID==""||i.CredentialID==""||
   s.federation==nil||s.federation.CanOperate(r.Context(),i.PrincipalID)!=nil||
   !s.apiCapture.accepts(time.Now(),token,i.PrincipalID,i.CredentialID){
   next.ServeHTTP(w,r);return
  }
  traceID,traceRef:="",""
  if r.Method=="POST"&&r.URL.Path=="/v1/tasks"{
   // This is the only trusted HTTP -> durable Task event trace insertion
   // in this increment. No timing-based links or untrusted caller trace IDs.
   if minted,e:=qaAPIRandomID();e==nil{
    traceID=minted
    traceRef=qaOpaqueRef("trace",traceID)
    r=r.WithContext(context.WithValue(r.Context(),qaTrustedTraceContextKey{},traceID))
   }
  }
  wrapped:=&qaAPIStatusWriter{ResponseWriter:w}
  started:=time.Now()
  next.ServeHTTP(wrapped,r)
  status:=wrapped.status
  if status==0{status=200}
  if status!=http.StatusCreated{traceRef=""}
  s.apiCapture.observeWithTrace(time.Now(),token,i.PrincipalID,i.CredentialID,
   r.URL.Path,r.Method,status,time.Since(started),traceRef)
 })
}
