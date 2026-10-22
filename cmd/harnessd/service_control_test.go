package main

import (
 "bytes"
 "encoding/hex"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/serviceproof"
)

const testToken="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
func requestWithNonce(method,path,remote string, nonce []byte) *http.Request {
 req:=httptest.NewRequest(method,path,nil)
 req.RemoteAddr=remote
 if nonce!=nil{req.Header.Set(serviceproof.ChallengeHeader,hex.EncodeToString(nonce))}
 return req
}
func TestReadinessReturnsNonceProofWithoutBearerHeader(t *testing.T){
 nonce:=bytes.Repeat([]byte{7},serviceproof.ChallengeBytes)
 handler:=serviceControlHandler(http.NotFoundHandler(),testToken,nil)
 for _,remote:=range []string{"127.0.0.1:50000","[::1]:50000"}{
  req:=requestWithNonce("GET","/__onepane/service/ready",remote,nonce)
  rec:=httptest.NewRecorder()
  handler.ServeHTTP(rec,req)
  if rec.Code!=204||rec.Header().Get("Cache-Control")!="no-store"||
   !serviceproof.Verify(testToken,nonce,rec.Header().Get(serviceproof.ProofHeader)){
   t.Fatalf("loopback readiness proof failed: %s %d",remote,rec.Code)
  }
 }
 for _,tc:=range []struct{method,remote string;nonce []byte}{
  {"GET","203.0.113.5:9999",nonce},
  {"POST","127.0.0.1:50000",nonce},
  {"GET","127.0.0.1:50000",nil},
  {"GET","127.0.0.1:50000",[]byte{1}},
 }{
  rec:=httptest.NewRecorder()
  handler.ServeHTTP(rec,requestWithNonce(tc.method,"/__onepane/service/ready",tc.remote,tc.nonce))
  if rec.Code!=404{t.Fatalf("invalid request accepted: %+v",tc)}
 }
}
func TestShutdownRejectsReadinessProofAndInvalidPurpose(t *testing.T){
 nonce:=bytes.Repeat([]byte{3},serviceproof.ChallengeBytes)
 closed:=make(chan struct{},1)
 handler:=serviceControlHandler(http.NotFoundHandler(),testToken,func(){closed<-struct{}{}})
 send:=func(method,remote,signature string)int{
  req:=requestWithNonce(method,"/__onepane/service/shutdown",remote,nonce)
  if signature!=""{req.Header.Set(serviceproof.ProofHeader,signature)}
  rec:=httptest.NewRecorder();handler.ServeHTTP(rec,req)
  return rec.Code
 }
 for _,signature:=range []string{"",serviceproof.Sign(testToken,nonce),
  serviceproof.SignShutdown("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",nonce)}{
  if got:=send("POST","127.0.0.1:50000",signature);got!=404{
   t.Fatalf("unapproved shutdown response: %d",got)
  }
 }
 valid:=serviceproof.SignShutdown(testToken,nonce)
 if send("GET","127.0.0.1:50000",valid)!=404||
  send("POST","203.0.113.1:50000",valid)!=404{t.Fatal("invalid method/remote shutdown accepted")}
 select{case <-closed:t.Fatal("unauthorized cancel");default:}
 if got:=send("POST","127.0.0.1:50000",valid);got!=202{t.Fatalf("signed shutdown returned %d",got)}
 select{case <-closed:case <-time.After(time.Second):t.Fatal("authenticated shutdown failed")}
}
func TestServiceControlDisabledWithoutLaunchToken(t *testing.T){
 base:=http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(http.StatusTeapot)})
 for _,token:=range []string{"","short"}{
  rec:=httptest.NewRecorder()
  serviceControlHandler(base,token,nil).ServeHTTP(rec,requestWithNonce("GET","/__onepane/service/ready","127.0.0.1:9",nil))
  if rec.Code!=http.StatusTeapot{t.Fatal("normal backend behavior changed")}
 }
}
