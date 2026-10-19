package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

func TestServiceReadinessRequiresEphemeralLaunchTokenAndLoopback(t *testing.T) {
 const token="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 const oldToken="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
 base:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.WriteHeader(http.StatusOK) // The public health handler is NOT proof.
 })
 cases:=[]struct{
  name,method,path,remote,header string
  want int
 }{
  {"ready","GET","/__onepane/service/ready","127.0.0.1:50000",token,204},
  {"ipv6-loopback","GET","/__onepane/service/ready","[::1]:50000",token,204},
  {"missing-token","GET","/__onepane/service/ready","127.0.0.1:50000","",404},
  {"old-service-token","GET","/__onepane/service/ready","127.0.0.1:50000",oldToken,404},
  {"remote-client","GET","/__onepane/service/ready","203.0.113.10:50000",token,404},
  {"bad-method","POST","/__onepane/service/ready","127.0.0.1:50000",token,404},
  {"public-health","GET","/v1/health","127.0.0.1:50000","",200},
  {"untrusted-shutdown","POST","/__onepane/service/shutdown","127.0.0.1:50000",oldToken,404},
 }
 handler:=serviceControlHandler(base,token,nil)
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   req:=httptest.NewRequest(tc.method,tc.path,nil)
   req.RemoteAddr=tc.remote
   if tc.header!=""{req.Header.Set("X-OnePane-Service-Token",tc.header)}
   recorder:=httptest.NewRecorder()
   handler.ServeHTTP(recorder,req)
   if recorder.Code!=tc.want{t.Fatalf("got %d, want %d",recorder.Code,tc.want)}
   if tc.want==204&&recorder.Header().Get("Cache-Control")!="no-store"{
    t.Fatal("service readiness is cacheable")
   }
  })
 }
}

func TestServiceControlDisabledOutsideServiceLaunch(t *testing.T) {
 base:=http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(http.StatusTeapot)})
 for _,token:=range []string{"","short"}{
  recorder:=httptest.NewRecorder()
  serviceControlHandler(base,token,nil).ServeHTTP(recorder,
   httptest.NewRequest("GET","/__onepane/service/ready",nil))
  if recorder.Code!=http.StatusTeapot{t.Fatalf("changed normal non-service behavior: %d",recorder.Code)}
 }
}

func TestServiceShutdownRequiresSameTokenAndOnlyPosts(t *testing.T) {
 const token="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
 canceled:=make(chan struct{},1)
 handler:=serviceControlHandler(http.NotFoundHandler(),token,func(){canceled<-struct{}{}})
 for _,method:=range []string{http.MethodGet,http.MethodPut}{
  req:=httptest.NewRequest(method,"/__onepane/service/shutdown",nil)
  req.RemoteAddr="127.0.0.1:50000"
  req.Header.Set("X-OnePane-Service-Token",token)
  rec:=httptest.NewRecorder()
  handler.ServeHTTP(rec,req)
  if rec.Code!=http.StatusNotFound{t.Fatalf("%s wrongly accepted",method)}
 }
 req:=httptest.NewRequest(http.MethodPost,"/__onepane/service/shutdown",nil)
 req.RemoteAddr="127.0.0.1:50000"
 req.Header.Set("X-OnePane-Service-Token",token)
 rec:=httptest.NewRecorder()
 handler.ServeHTTP(rec,req)
 if rec.Code!=http.StatusAccepted{t.Fatalf("shutdown returned %d",rec.Code)}
 select{case <-canceled:case <-time.After(time.Second):t.Fatal("authenticated shutdown did not cancel")}
}
