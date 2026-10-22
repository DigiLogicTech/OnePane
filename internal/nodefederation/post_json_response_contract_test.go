package nodefederation

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestPairedNodeResponsesRequireSingleUnambiguousJSONDocument(t *testing.T){
 payload:=""
 endpoint:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(payload))
 }))
 defer endpoint.Close()
 check:=func(name,body string,allowed bool){
  t.Helper()
  payload=body
  var out map[string]any
  err:=postJSON(context.Background(),endpoint.Client(),endpoint.URL+"/federation/v1/test",
   map[string]string{"scope":"approved"},&out)
  if allowed{
   if err!=nil||out["ok"]!=true{t.Fatalf("%s: valid peer denied: %+v %v",name,out,err)}
  }else if !errors.Is(err,ErrFederationResponseInvalid){
   t.Fatalf("%s: ambiguous or missing peer reply accepted: %+v err=%v",name,out,err)
  }
 }
 check("valid",`{"ok":true}`,true)
 check("empty","",false)
 check("null","null",false)
 check("duplicate",`{"ok":true,"ok":false}`,false)
 check("nested duplicate",`{"ok":true,"items":{"key":1,"key":2}}`,false)
 check("escaped duplicate",`{"ok":true,"o\u006b":false}`,false)
 check("multiple",`{"ok":true}{"ok":false}`,false)
 check("malformed",`{"ok":`,false)
 payload=""
 if err:=postJSON(context.Background(),endpoint.Client(),endpoint.URL,
  map[string]string{"scope":"approved"},nil);err!=nil{
  t.Fatalf("intentional no-return-body command rejected: %v",err)
 }
}
