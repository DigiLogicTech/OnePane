//go:build windows

package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

func TestServiceReadinessRejectsOldAndUnrelatedListeners(t *testing.T) {
 const token="a5e5fed0d30eb5504a6b43a94be9dd3ca5b44e7d3a9cc1109097da2afe767ab9"
 const older="0ea27f2d4e6063ce2e6acc2ff2127aadef070b4087f8ba2358b7379f08c4d33e"
 client:=&http.Client{Timeout:time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 for _,tc:=range []struct{name string; status int; requiresToken bool; want bool}{
  {"unrelated-public-health",http.StatusOK,false,false},
  {"previous-service-auth",http.StatusNoContent,true,false},
  {"correct-token",http.StatusNoContent,true,true},
  {"redirect-is-not-ready",http.StatusFound,false,false},
  {"backend-unavailable",http.StatusServiceUnavailable,false,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    if tc.requiresToken {
     expected:=older
     if tc.name=="correct-token"{expected=token}
     if r.Header.Get("X-OnePane-Service-Token")!=expected{http.NotFound(w,r);return}
    }
    if tc.status==http.StatusFound{w.Header().Set("Location","http://127.0.0.1:1")}
    w.WriteHeader(tc.status)
   }))
   defer server.Close()
   got:=probeLaunchedBackend(client,server.URL,token)
   if got!=tc.want{t.Fatalf("ready=%v; expected %v",got,tc.want)}
  })
 }
 if probeLaunchedBackend(client,"http://127.0.0.1:1",token){
  t.Fatal("unavailable child accepted as ready")
 }
 if probeLaunchedBackend(client,"http://127.0.0.1:1","short"){
  t.Fatal("short service token accepted")
 }
}
