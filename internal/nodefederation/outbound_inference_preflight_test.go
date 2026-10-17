package nodefederation

import (
 "encoding/json"
 "errors"
 "testing"
)

func TestRemoteInferenceSenderRejectsAmbiguousRequestBeforeNetwork(t *testing.T){
 original:=RemoteInferenceRequest{Protocol:ProtocolVersion,
  RequestID:"task-1",DeploymentID:"model-deployment",
  RequestJSON:json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`)}
 check:=func(name string,p RemoteInferenceRequest,want bool){
  t.Helper()
  err:=validateOutboundFederatedInference(p)
  if want&&err!=nil{t.Fatalf("%s: valid payload denied: %v",name,err)}
  if !want&&!errors.Is(err,ErrInvalidOutboundFederatedInference){
   t.Fatalf("%s: malformed or ambiguous payload accepted: %v",name,err)
  }
 }
 check("valid",original,true)
 invalid:=original
 invalid.RequestID=""
 check("missing request ID",invalid,false)
 invalid=original
 invalid.RequestID=" task-1 "
 check("ambiguous padded request ID",invalid,false)
 invalid=original
 invalid.DeploymentID=""
 check("missing exact deployment",invalid,false)
 invalid=original
 invalid.RequestJSON=json.RawMessage(`{"messages":[],"messages":[1]}`)
 check("duplicate fields",invalid,false)
 invalid=original
 invalid.RequestJSON=json.RawMessage(`{"task_id":"a","task\u005fid":"b"}`)
 check("escaped duplicate fields",invalid,false)
 invalid=original
 invalid.RequestJSON=json.RawMessage(`{"messages":[`)
 check("malformed model request",invalid,false)
 invalid=original
 invalid.ContextManifest=json.RawMessage(`{"policy":1,"policy":2}`)
 check("ambiguous context manifest",invalid,false)
 invalid=original
 invalid.ClassificationJSON=json.RawMessage(`{"rating":"local","rating":"remote"}`)
 check("ambiguous classification",invalid,false)
 invalid=original
 invalid.Protocol="unsupported-protocol"
 check("unsupported federation protocol",invalid,false)
}
