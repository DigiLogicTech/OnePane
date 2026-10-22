//go:build windows

package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
 "encoding/hex"

 "github.com/DigiLogicTech/OnePane/internal/serviceproof"
)

func TestServiceReadinessRejectsOldAndUnrelatedListeners(t *testing.T) {
 const token="a5e5fed0d30eb5504a6b43a94be9dd3ca5b44e7d3a9cc1109097da2afe767ab9"
 const older="0ea27f2d4e6063ce2e6acc2ff2127aadef070b4087f8ba2358b7379f08c4d33e"
 client:=&http.Client{Timeout:time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 for _,tc:=range []struct{name string;status int;signer string;want bool}{
  {"unrelated-200",http.StatusOK,"",false},
  {"blind-unrelated-204",http.StatusNoContent,"",false},
  {"forged-proof",http.StatusNoContent,"forged",false},
  {"previous-service",http.StatusNoContent,older,false},
  {"correct-service",http.StatusNoContent,token,true},
  {"blind-reflected-challenge",http.StatusNoContent,"reflect",false},
  {"redirect",http.StatusFound,"",false},
  {"unavailable",http.StatusServiceUnavailable,"",false},
 }{
  t.Run(tc.name,func(t *testing.T){
   server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,req *http.Request){
    if tc.signer!="" {
     nonce,err:=hex.DecodeString(req.Header.Get(serviceproof.ChallengeHeader))
     if err!=nil{http.NotFound(w,req);return}
     if tc.signer=="forged"{
      w.Header().Set(serviceproof.ProofHeader,"0000000000000000000000000000000000000000000000000000000000000000")
     }else if tc.signer=="reflect"{
      w.Header().Set(serviceproof.ProofHeader,req.Header.Get(serviceproof.ChallengeHeader))
     }else{
      w.Header().Set(serviceproof.ProofHeader,serviceproof.Sign(tc.signer,nonce))
     }
    }
    if tc.status==http.StatusFound{w.Header().Set("Location","http://127.0.0.1:1")}
    w.WriteHeader(tc.status)
   }))
   defer server.Close()
   got:=probeLaunchedBackend(client,server.URL,token)
   if got!=tc.want{t.Fatalf("readiness=%v want %v",got,tc.want)}
  })
 }
 if probeLaunchedBackend(client,"http://127.0.0.1:1",token){
  t.Fatal("unavailable child accepted as ready")
 }
 if probeLaunchedBackend(client,"http://127.0.0.1:1","short"){
  t.Fatal("short service token accepted")
 }
}

func TestSCMReadinessDoesNotLeakLaunchSecretToPortHijacker(t *testing.T){
 const token="a5e5fed0d30eb5504a6b43a94be9dd3ca5b44e7d3a9cc1109097da2afe767ab9"
 seen:=make(chan http.Header,1)
 fake:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  seen<-r.Header.Clone()
  w.WriteHeader(http.StatusNoContent)
 }))
 defer fake.Close()
 client:=&http.Client{Timeout:time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 if probeLaunchedBackend(client,fake.URL,token){t.Fatal("blind 204 accepted")}
 headers:=<-seen
 if headers.Get("X-OnePane-Service-Token")!=""{t.Fatal("SCM disclosed bearer token")}
 for name,values:=range headers {
  for _,value:=range values{if value==token{t.Fatalf("leaked secret in %s header",name)}}
 }
 if headers.Get(serviceproof.ChallengeHeader)==""{t.Fatal("missing nonce")}
}
