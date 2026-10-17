package nodefederation

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestFederationJSONTransportKeepsErrorBodiesPrivateAndBounded(t *testing.T){
 sent:=0
 endpoint:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  sent++
  switch r.URL.Path{
  case "/deny":
   w.WriteHeader(http.StatusForbidden)
   _,_=w.Write([]byte("PRIVATE_PROMPT=do-not-log-this"))
  case "/oversize":
   w.WriteHeader(http.StatusOK)
   _,_=w.Write([]byte(`{"payload":"`+strings.Repeat("x",4<<20)+`"}`))
  default:
   w.WriteHeader(http.StatusOK)
   _,_=w.Write([]byte(`{"ok":true}`))
  }
 }))
 defer endpoint.Close()
 var output struct{OK bool `json:"ok"`}
 if err:=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/valid",
  map[string]string{"approved":"safe"},&output);err!=nil||!output.OK{
  t.Fatalf("valid bounded peer reply rejected: %+v %v",output,err)
 }
 err:=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/deny",
  map[string]string{"prompt":"secret"},&output)
 if err==nil||strings.Contains(err.Error(),"PRIVATE_PROMPT"){
  t.Fatalf("peer error body reflected private content: %v",err)
 }
 if err=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/oversize",
  map[string]string{"approved":"safe"},&output);!errors.Is(err,ErrFederationResponseTooLarge){
  t.Fatalf("oversized successful peer payload admitted: %v",err)
 }
 before:=sent
 if err=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/valid",
  map[string]string{"payload":strings.Repeat("x",4<<20)},&output);
  !errors.Is(err,ErrFederationPayloadTooLarge)||sent!=before{
  t.Fatalf("oversized outbound JSON was transmitted: %v sent=%d",err,sent-before)
 }
 if err=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/valid",
  make(chan int),&output);err==nil||sent!=before{
  t.Fatalf("invalid JSON request reached paired Node: %v sent=%d",err,sent-before)
 }
}
