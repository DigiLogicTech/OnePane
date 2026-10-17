package nodefederation

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/inference"
)

func TestFederatedOutputRequiresBoundedValidJSON(t *testing.T){
 valid:=inference.DispatchResult{
  ResponseJSON:json.RawMessage(`{"completion":"ok"}`),
  UsageJSON:json.RawMessage(`{"tokens":2}`),
 }
 if got:=classifyFederatedOutput(valid);got!=""{t.Fatalf("valid output denied: %s",got)}
 valid.UsageJSON=nil
 if got:=classifyFederatedOutput(valid);got!=""{t.Fatalf("optional usage denied: %s",got)}
 cases:=[]struct{name string;r inference.DispatchResult;want string}{
  {"empty response",inference.DispatchResult{},"invalid_remote_response"},
  {"malformed response",inference.DispatchResult{ResponseJSON:json.RawMessage("not-json")},"invalid_remote_response"},
  {"oversized response",inference.DispatchResult{ResponseJSON:json.RawMessage(`{"text":"`+strings.Repeat("x",3<<20)+`"}`)},"invalid_remote_response"},
  {"malformed usage",inference.DispatchResult{ResponseJSON:valid.ResponseJSON,UsageJSON:json.RawMessage(`{"tokens":`)},"invalid_remote_usage"},
  {"oversized usage",inference.DispatchResult{ResponseJSON:valid.ResponseJSON,UsageJSON:json.RawMessage(`{"extra":"`+strings.Repeat("x",64<<10)+`"}`)},"invalid_remote_usage"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if got:=classifyFederatedOutput(tc.r);got!=tc.want{t.Fatalf("got %q want %q",got,tc.want)}
  })
 }
}
